package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	mounttypes "github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/jazzl0ver/firecamp/pkg/containersvc"
	"github.com/jazzl0ver/firecamp/pkg/containersvc/swarm"
	"github.com/jazzl0ver/firecamp/pkg/db"
	"github.com/jazzl0ver/firecamp/pkg/db/awsdynamodb"
	"github.com/jazzl0ver/firecamp/pkg/log/awscloudwatch"
	"github.com/jazzl0ver/firecamp/pkg/server/awsec2"
)

var (
	cluster = flag.String("cluster", "", "The cluster name")
	role    = flag.String("role", "", "The swarm node role, worker or manager")
	azs     = flag.String("availability-zones", "", "The availability zones, example: us-east-1a,us-east-1b,us-east-1c")
)

const (
	defaultSwarmListenPort = "2377"
	defaultDockerSockPath  = "/var/run/docker.sock"
	manageServerConstraint = "node.role==manager"
	addrSep                = ","
)

func main() {
	flag.Parse()
	flag.Set("stderrthreshold", "INFO")

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
	azSep := ","
	zones := strings.Split(*azs, azSep)
	swarmSvc, err := swarmsvc.NewSwarmSvcOnManagerNode(zones)
	if err != nil {
		glog.Fatalln("NewSwarmSvcOnManagerNode error", err)
	}

	if *role == awsdynamodb.RoleManager {
		logIns := awscloudwatch.NewLog(sess, region, common.ContainerPlatformSwarm, "")
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

	// create the firecamp catalog service
	createCatalogService(ctx, swarmSvc, logIns, cluster)
}

func joinAsManager(ctx context.Context, swarmSvc *swarmsvc.SwarmSvc, dbIns *awsdynamodb.DynamoDB, cluster string, addr string, initManagerAddr string) {
	token := getJoinToken(ctx, dbIns, cluster, awsdynamodb.RoleManager)

	addrs, err := dbIns.GetManagerAddrs(ctx, cluster)
	if err != nil {
		glog.Fatalln("GetManagerAddrs error", err, "local addr", addr)
	}

	// join swarm cluster
	joinCluster(ctx, swarmSvc, dbIns, addrs, addr, token)

	// update manager addrs
	for i := 0; i < 3; i++ {
		if idx := strings.Index(addrs, addr); idx == -1 {
			// get all good and down managers and update manager addresses in db.
			goodManagers, downManagerNodes, downManagers, err := swarmSvc.ListSwarmManagerNodes(ctx)
			if err != nil {
				glog.Fatalln("ListSwarmManagerNodes error", err, "local addr", addr)
			}

			glog.Infoln("swarm managers", goodManagers, "down managers", downManagers, "local addr", addr)

			newAddrs := addrListToStrs(goodManagers)

			err = dbIns.AddManagerAddrs(ctx, cluster, addrs, newAddrs)
			if err == nil {
				removeDownManager(ctx, swarmSvc, downManagerNodes)
				break
			}

			if err != db.ErrDBConditionalCheckFailed {
				glog.Fatalln("AddManagerAddrs error", err, "oldAddrs", addrs, "newAddrs", newAddrs)
			}

			glog.Errorln("AddManagerAddrs conditional check failed, another manager may update the addr at the same time, oldAddrs", addrs, "newAddrs", newAddrs)
			addrs, err = dbIns.GetManagerAddrs(ctx, cluster)
			if err != nil {
				glog.Fatalln("GetManagerAddrs error", err, addr)
			}
		} else {
			glog.Errorln("manager addr already exists, the new node is assigned the old ip?", addr, "oldAddrs", addrs)
			break
		}
	}

	glog.Infoln("joined addr", addr, "to manager addrs", addrs, "cluster", cluster)
}

func removeDownManager(ctx context.Context, swarmSvc *swarmsvc.SwarmSvc, nodes []swarm.Node) {
	if len(nodes) == 0 {
		return
	}

	sort.SliceStable(nodes[:], func(i, j int) bool {
		return nodes[i].UpdatedAt.Unix() < nodes[j].UpdatedAt.Unix()
	})

	for _, n := range nodes {
		err := swarmSvc.RemoveDownManagerNode(ctx, n)
		if err != nil {
			glog.Errorln("RemoveDownManagerNode error", err, n)
		} else {
			// remove one down manager, as only a new manager is added.
			glog.Infoln("removed down manager node", n)
			break
		}
	}
}

func initWorker(ctx context.Context, swarmSvc *swarmsvc.SwarmSvc, dbIns *awsdynamodb.DynamoDB, cluster string, addr string) {
	token := getJoinToken(ctx, dbIns, cluster, awsdynamodb.RoleWorker)

	// get current swarm managers
	addrs, err := dbIns.GetManagerAddrs(ctx, cluster)
	if err != nil {
		glog.Fatalln("GetManagerAddrs error", err, cluster, addr)
	}

	// join swarm cluster
	joinCluster(ctx, swarmSvc, dbIns, addrs, addr, token)
}

func joinCluster(ctx context.Context, swarmSvc *swarmsvc.SwarmSvc,
	dbIns *awsdynamodb.DynamoDB, addrs string, localAddr string, token string) {
	// join swarm cluster
	addrList := strings.Split(addrs, addrSep)
	for _, a := range addrList {
		// join the swarm manager
		err := swarmSvc.SwarmJoin(ctx, localAddr, a, token)
		if err == nil {
			glog.Infoln("joined manager", a, "local addr", localAddr)
			return
		}

		// SwarmJoin error, try the next manager
		glog.Errorln("SwarmJoin error", err, localAddr, "manager addr", a)
	}

	glog.Fatalln("could not join any manager", addrs, "local addr", localAddr)
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

// example manage service creation:
// docker service create --name mgt --constraint node.role==manager --replicas 1 \
// --publish mode=host,target=27040,published=27040 \
// --mount type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock \
// -e CONTAINER_PLATFORM=swarm -e DB_TYPE=clouddb -e AVAILABILITY_ZONES=us-east-1a -e CLUSTER=c1 \
// jazzl0ver/firecamp-manageserver
func createManageService(ctx context.Context, swarmSvc *swarmsvc.SwarmSvc, logIns *awscloudwatch.Log, cluster string) {
	serviceUUID := common.SystemName

	// create the log group
	err := logIns.InitializeServiceLogConfig(ctx, cluster, common.ManageName, serviceUUID)
	if err != nil {
		glog.Fatalln("create the manage service log group error", err, common.ManageName)
	}

	logConfig := logIns.CreateStreamLogConfig(ctx, cluster, common.ManageName, serviceUUID, common.ManageName)

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
		Common:       commonOpts,
		PortMappings: []common.PortMapping{portMap},
		Replicas:     1,
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

	err = swarmSvc.CreateSwarmService(ctx, spec, createOpts)
	if err != nil {
		glog.Fatalln("create the manage service error", err, cluster)
	}

	glog.Infoln("created the manage service", cluster)
}

// example catalog service creation:
// docker service create --name mgt --replicas 1 \
// --publish mode=host,target=27041,published=27041 \
// -e CONTAINER_PLATFORM=swarm -e AVAILABILITY_ZONES=us-east-1a -e CLUSTER=c1 \
// jazzl0ver/firecamp-catalogservice
func createCatalogService(ctx context.Context, swarmSvc *swarmsvc.SwarmSvc, logIns *awscloudwatch.Log, cluster string) {
	serviceUUID := common.SystemName

	// create the log group
	err := logIns.InitializeServiceLogConfig(ctx, cluster, common.CatalogService, serviceUUID)
	if err != nil {
		glog.Fatalln("create the catalog service log group error", err, common.CatalogService)
	}

	logConfig := logIns.CreateStreamLogConfig(ctx, cluster, common.CatalogService, serviceUUID, common.CatalogService)

	commonOpts := &containersvc.CommonOptions{
		Cluster:        cluster,
		ServiceName:    common.CatalogServiceName,
		ServiceUUID:    serviceUUID,
		ContainerImage: common.CatalogContainerImage,
		Resource: &common.Resources{
			MaxCPUUnits:     common.DefaultMaxCPUUnits,
			ReserveCPUUnits: common.ManageReserveCPUUnits,
			MaxMemMB:        common.ManageMaxMemMB,
			ReserveMemMB:    common.ManageReserveMemMB,
		},
		LogConfig: logConfig,
	}

	portMap := common.PortMapping{
		ContainerPort: common.CatalogServerHTTPPort,
		HostPort:      common.CatalogServerHTTPPort,
	}

	createOpts := &containersvc.CreateServiceOptions{
		Common:       commonOpts,
		PortMappings: []common.PortMapping{portMap},
		Replicas:     1,
	}

	spec := swarmSvc.CreateServiceSpec(createOpts)

	// set the environment variables
	spec.TaskTemplate.ContainerSpec.Env = []string{
		common.ENV_CONTAINER_PLATFORM + "=" + common.ContainerPlatformSwarm,
		common.ENV_AVAILABILITY_ZONES + "=" + *azs,
		common.ENV_CLUSTER + "=" + cluster,
	}

	err = swarmSvc.CreateSwarmService(ctx, spec, createOpts)
	if err != nil {
		glog.Fatalln("create the catalog service error", err, cluster)
	}

	glog.Infoln("created the catalog service", cluster)
}

func addrListToStrs(addrList []string) string {
	addrs := addrList[0]
	for i := 1; i < len(addrList); i++ {
		addrs = addrs + addrSep + addrList[i]
	}
	return addrs
}
