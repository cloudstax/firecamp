package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	mounttypes "github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/containersvc/swarm"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/db/awsdynamodb"
	"github.com/cloudstax/firecamp/log/awscloudwatch"
	"github.com/cloudstax/firecamp/server/awsec2"
)

var (
	cluster = flag.String("cluster", "", "The cluster name")
	role    = flag.String("role", "", "The swarm node role, worker or manager")
	azs     = flag.String("availability-zones", "", "The availability zones")
)

const (
	defaultSwarmListenPort = "2377"
	defaultDockerSockPath  = "/var/run/docker.sock"
	manageServerConstraint = "node.role==manager"
)

func main() {
	flag.Parse()

	if *cluster == "" || *role == "" || *azs == "" {
		fmt.Println("ERROR: please speficy the cluster name, role and availability zones", *cluster, *role, *azs)
		os.Exit(-1)
	}
	if *role != awsdynamodb.RoleWorker && *role != awsdynamodb.RoleManager {
		fmt.Println("ERROR: invalid swarm node role", *role, "please input", awsdynamodb.RoleWorker, awsdynamodb.RoleManager)
		os.Exit(-1)
	}

	// get EC2 local IP
	ip, err := awsec2.GetLocalEc2IP()
	if err != nil {
		glog.Fatalln("GetLocalEc2Hostname error", err)
	}

	addr := ip + ":" + defaultSwarmListenPort

	// new dynamodb instance
	region, err := awsec2.GetLocalEc2Region()
	if err != nil {
		glog.Fatalln("awsec2 GetLocalEc2Region error", err)
	}

	config := aws.NewConfig().WithRegion(region)
	sess, err := session.NewSession(config)
	if err != nil {
		glog.Fatalln("failed to create session, error", err)
	}

	dbIns := awsdynamodb.NewDynamoDB(sess, *cluster)

	ctx := context.Background()
	err = dbIns.WaitSystemTablesReady(ctx, common.DefaultServiceWaitSeconds)
	if err != nil {
		glog.Fatalln("WaitSystemTablesReady error", err)
	}

	// init or join swarm
	swarmSvc, err := swarmsvc.NewSwarmSvc()
	if err != nil {
		glog.Fatalln("NewSwarmSvc error", err)
	}

	if *role == awsdynamodb.RoleManager {
		logIns := awscloudwatch.NewLog(sess, region, common.ContainerPlatformSwarm)
		initManager(ctx, swarmSvc, dbIns, logIns, *cluster, addr)
	} else {
		initWorker(ctx, swarmSvc, dbIns, *cluster, addr)
	}
}

func initManager(ctx context.Context, swarmSvc *swarmsvc.SwarmSvc,
	dbIns *awsdynamodb.DynamoDB, logIns *awscloudwatch.Log, cluster string, addr string) {
	// try to take the init manager
	err := dbIns.TakeInitManager(ctx, cluster, addr)
	if err != nil {
		if err != db.ErrDBConditionalCheckFailed {
			glog.Fatalln("TakeInitManager error", err, cluster, addr)
		}

		// the init manager record exists in db, check who is the current init manager.
		initManagerAddr, err := dbIns.GetInitManager(ctx, cluster)
		if err != nil {
			glog.Fatalln("GetInitManager error", err, cluster, addr)
		}

		glog.Infoln("init manager exist", initManagerAddr, cluster, addr)
		if initManagerAddr != addr {
			// the local node is not the init manager, join it
			joinAsManager(ctx, swarmSvc, dbIns, cluster, addr, initManagerAddr)
			return
		}

		// the local node is the init manager, may be a retry
	}

	// the local node is the init manager, init the swarm cluster
	glog.Infoln("successfully take the init manager", cluster, addr)
	init, err := swarmSvc.IsSwarmInitialized(ctx)
	if err != nil {
		glog.Fatalln("IsSwarmInitialized error", err, cluster, addr)
	}

	if !init {
		err = swarmSvc.SwarmInit(ctx, addr)
		if err != nil {
			glog.Fatalln("InitSwarm error", err, cluster, addr)
		}
		glog.Infoln("initialized swarm cluster", cluster, addr)
	} else {
		glog.Infoln("swarm cluster is already initialized", cluster, addr)
	}

	// put the join tokens to db
	mtoken, wtoken, err := swarmSvc.GetJoinToken(ctx)
	if err != nil {
		glog.Fatalln("GetJoinToken error", err, cluster, addr)
	}

	err = dbIns.CreateJoinToken(ctx, cluster, mtoken, awsdynamodb.RoleManager)
	if err != nil && err != db.ErrDBConditionalCheckFailed {
		glog.Fatalln("Create manager JoinToken error", err, cluster, addr)
	}

	err = dbIns.CreateJoinToken(ctx, cluster, wtoken, awsdynamodb.RoleWorker)
	if err != nil && err != db.ErrDBConditionalCheckFailed {
		glog.Fatalln("Create worker JoinToken error", err, cluster, addr)
	}

	glog.Infoln("put manager and worker join token into db", cluster, addr)

	// create the firecamp manage service
	createManageService(ctx, swarmSvc, logIns, cluster)
}

func joinAsManager(ctx context.Context, swarmSvc *swarmsvc.SwarmSvc, dbIns *awsdynamodb.DynamoDB, cluster string, addr string, initManagerAddr string) {
	token := getJoinToken(ctx, dbIns, cluster, awsdynamodb.RoleManager)

	// join the swarm manager
	err := swarmSvc.SwarmJoin(ctx, addr, initManagerAddr, token)
	if err != nil {
		glog.Fatalln("SwarmJoin error", err, addr, "init manager", initManagerAddr)
	}

	glog.Infoln("joined init manager", initManagerAddr, cluster, addr)
}

func initWorker(ctx context.Context, swarmSvc *swarmsvc.SwarmSvc, dbIns *awsdynamodb.DynamoDB, cluster string, addr string) {
	// the init manager record exists in db, check who is the current init manager.
	initManagerAddr, err := dbIns.GetInitManager(ctx, cluster)
	if err != nil {
		glog.Fatalln("GetInitManager error", err, cluster, addr)
	}

	token := getJoinToken(ctx, dbIns, cluster, awsdynamodb.RoleWorker)

	// join the swarm manager
	err = swarmSvc.SwarmJoin(ctx, addr, initManagerAddr, token)
	if err != nil {
		glog.Fatalln("SwarmJoin error", err, addr, "init manager", initManagerAddr)
	}

	glog.Infoln("joined init manager", initManagerAddr, cluster, addr)

}

func getJoinToken(ctx context.Context, dbIns *awsdynamodb.DynamoDB, cluster string, role string) string {
	// get the manager or worker join token
	var err error
	token := ""

	sleepSecs := time.Duration(common.DefaultRetryWaitSeconds) * time.Second

	for i := int64(0); i < common.DefaultTaskWaitSeconds; i += common.DefaultRetryWaitSeconds {
		token, err = dbIns.GetJoinToken(ctx, cluster, role)
		if err == nil {
			glog.Infoln("get the join token", cluster, "role", role)
			return token
		}

		if err == db.ErrDBRecordNotFound {
			glog.Infoln("the join token not exist in db, wait the init manager to put it", cluster)
		} else {
			glog.Errorln("GetJoinToken error", err, cluster, "role", role)
		}

		time.Sleep(sleepSecs)
	}

	glog.Fatalln("The join token not exist in db after", common.DefaultTaskWaitSeconds, "seconds", cluster, "role", role)
	return ""
}

func createManageService(ctx context.Context, swarmSvc *swarmsvc.SwarmSvc, logIns *awscloudwatch.Log, cluster string) {
	serviceUUID := common.SystemName

	logConfig := logIns.CreateServiceLogConfig(ctx, cluster, common.ManageName, serviceUUID)

	commonOpts := &containersvc.CommonOptions{
		Cluster:        cluster,
		ServiceName:    common.ManageServiceName,
		ServiceUUID:    serviceUUID,
		ContainerImage: common.ManageContainerImage,
		Resource: &common.Resources{
			MaxCPUUnits:     common.DefaultMaxCPUUnits,
			ReserveCPUUnits: common.ManageReserveCPUUnits,
			MaxMemMB:        common.ManageMaxMemMB,
			ReserveMemMB:    common.ManageReserveMemMB,
		},
		LogConfig: logConfig,
	}

	portMap := common.PortMapping{
		ContainerPort: common.ManageHTTPServerPort,
		HostPort:      common.ManageHTTPServerPort,
	}

	createOpts := &containersvc.CreateServiceOptions{
		Common:        commonOpts,
		ContainerPath: "",
		PortMappings:  []common.PortMapping{portMap},
		Replicas:      1,
	}

	spec := swarmSvc.CreateServiceSpec(createOpts)

	// mount docker.sock into container as manageserver needs to talk with
	// the local docker daemon to get the swarm managers addresses.
	mount := mounttypes.Mount{
		Type:   mounttypes.TypeBind,
		Source: defaultDockerSockPath,
		Target: defaultDockerSockPath,
	}
	spec.TaskTemplate.ContainerSpec.Mounts = []mounttypes.Mount{mount}

	// set the environment variables
	spec.TaskTemplate.ContainerSpec.Env = []string{
		common.ENV_CONTAINER_PLATFORM + "=" + common.ContainerPlatformSwarm,
		common.ENV_DB_TYPE + "=" + common.DBTypeCloudDB,
		common.ENV_AVAILABILITY_ZONES + "=" + *azs,
		common.ENV_CLUSTER + "=" + cluster,
	}

	// limit the manage service to run on the swarm manager node only.
	spec.TaskTemplate.Placement = &swarm.Placement{
		Constraints: []string{manageServerConstraint},
	}

	err := swarmSvc.CreateSwarmService(ctx, spec, createOpts)
	if err != nil {
		glog.Fatalln("create the manage service error", err, cluster)
	}

	glog.Infoln("created the manage service", cluster)
}
