package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/containersvc/awsecs"
	"github.com/cloudstax/firecamp/containersvc/swarm"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/db/awsdynamodb"
	"github.com/cloudstax/firecamp/db/controldb/client"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/dns/awsroute53"
	"github.com/cloudstax/firecamp/log"
	"github.com/cloudstax/firecamp/log/awscloudwatch"
	"github.com/cloudstax/firecamp/manage/server"
	"github.com/cloudstax/firecamp/server"
	"github.com/cloudstax/firecamp/server/awsec2"
	"github.com/cloudstax/firecamp/utils"
)

var (
	platform      = flag.String("container-platform", common.ContainerPlatformECS, "The underline container platform: ecs or swarm, default: ecs")
	dbtype        = flag.String("dbtype", common.DBTypeCloudDB, "The db type, such as the AWS DynamoDB or the embedded controldb")
	zones         = flag.String("availability-zones", "", "The availability zones for the system, example: us-east-1a,us-east-1b,us-east-1c")
	manageDNSName = flag.String("dnsname", "", "the dns name of the management service. Default: "+dns.GetDefaultManageServiceDNSName("cluster"))
	managePort    = flag.Int("port", common.ManageHTTPServerPort, "port that the manage http service listens on")
	tlsEnabled    = flag.Bool("tlsverify", false, "whether TLS is enabled")
	caFile        = flag.String("tlscacert", "", "The CA file")
	certFile      = flag.String("tlscert", "", "The TLS server certificate file")
	keyFile       = flag.String("tlskey", "", "The TLS server key file")
)

const azSep = ","

func main() {
	flag.Parse()

	// log to std, the manageserver container will send the logs to such as AWS CloudWatch.
	utils.SetLogToStd()
	//utils.SetLogDir()

	if *tlsEnabled && (*certFile == "" || *keyFile == "") {
		fmt.Println("invalid command, please pass cert file and key file for tls", *certFile, *keyFile)
		os.Exit(-1)
	}

	if *platform != common.ContainerPlatformECS && *platform != common.ContainerPlatformSwarm {
		fmt.Println("not supported container platform", *platform)
		os.Exit(-1)
	}

	if *zones == "" {
		fmt.Println("Invalid command, please specify the availability zones for the system")
		os.Exit(-1)
	}

	region, err := awsec2.GetLocalEc2Region()
	if err != nil {
		glog.Fatalln("awsec2 GetLocalEc2Region error", err)
	}

	config := aws.NewConfig().WithRegion(region)
	sess, err := session.NewSession(config)
	if err != nil {
		glog.Fatalln("failed to create session, error", err)
	}

	serverIns := awsec2.NewAWSEc2(sess)
	serverInfo, err := awsec2.NewEc2Info(sess)
	if err != nil {
		glog.Fatalln("NewEc2Info error", err)
	}

	regionAZs := serverInfo.GetLocalRegionAZs()
	azs := strings.Split(*zones, azSep)
	for _, az := range azs {
		find := false
		for _, raz := range regionAZs {
			if az == raz {
				find = true
				break
			}
		}
		if !find {
			glog.Fatalln("The specified availability zone is not in the region's availability zones", *zones, regionAZs)
		}
	}

	dnsIns := awsroute53.NewAWSRoute53(sess)
	logIns := awscloudwatch.NewLog(sess, region, *platform)

	cluster := ""
	var containersvcIns containersvc.ContainerSvc
	switch *platform {
	case common.ContainerPlatformECS:
		info, err := awsecs.NewEcsInfo()
		if err != nil {
			glog.Fatalln("NewEcsInfo error", err)
		}

		cluster = info.GetContainerClusterID()
		containersvcIns = awsecs.NewAWSEcs(sess)

	case common.ContainerPlatformSwarm:
		info, err := swarmsvc.NewSwarmInfo()
		if err != nil {
			glog.Fatalln("NewSwarmInfo error", err)
		}

		cluster = info.GetContainerClusterID()
		// use the same tls configs with the manageserver.
		// TODO separate tls configs for swarm.
		containersvcIns = swarmsvc.NewSwarmSvc(info.GetSwarmManagers(),
			*tlsEnabled, *caFile, *certFile, *keyFile)

	default:
		glog.Fatalln("unsupport container platform", *platform)
	}

	ctx := context.Background()
	var dbIns db.DB
	switch *dbtype {
	case common.DBTypeCloudDB:
		dbIns = awsdynamodb.NewDynamoDB(sess, cluster)

		tableStatus, ready, err := dbIns.SystemTablesReady(ctx)
		if err != nil && err != db.ErrDBResourceNotFound {
			glog.Fatalln("check dynamodb SystemTablesReady error", err, tableStatus)
		}
		if !ready {
			// create system tables
			err = dbIns.CreateSystemTables(ctx)
			if err != nil {
				glog.Fatalln("dynamodb CreateSystemTables error", err)
			}

			// wait the system tables ready
			waitSystemTablesReady(ctx, dbIns)
		}

	case common.DBTypeControlDB:
		addr := dns.GetDefaultControlDBAddr(cluster)
		dbIns = controldbcli.NewControlDBCli(addr)

		createControlDB(ctx, region, cluster, logIns, containersvcIns, serverIns, serverInfo)
		waitControlDBReady(ctx, cluster, containersvcIns)

	default:
		glog.Fatalln("unknown db type", dbtype)
	}

	err = manageserver.StartServer(cluster, azs, *manageDNSName, *managePort, containersvcIns,
		dbIns, dnsIns, logIns, serverInfo, serverIns, *tlsEnabled, *caFile, *certFile, *keyFile)

	glog.Fatalln("StartServer error", err)
}

func createControlDB(ctx context.Context, region string, cluster string, logIns cloudlog.CloudLog,
	containersvcIns containersvc.ContainerSvc, serverIns server.Server, serverInfo server.Info) {
	// check if the controldb service exists.
	exist, err := containersvcIns.IsServiceExist(ctx, cluster, common.ControlDBServiceName)
	if err != nil {
		glog.Fatalln("check the controlDB service exist error", err, common.ControlDBServiceName)
	}
	if exist {
		glog.Infoln("The controlDB service is already created")
		return
	}

	// create the controldb volume
	az := serverInfo.GetLocalAvailabilityZone()
	// TODO if some step fails, the volume may not be deleted.
	//      add tag to EBS volume, so the old volume could be deleted.
	volID, err := serverIns.CreateVolume(ctx, az, common.ControlDBVolumeSizeGB)
	if err != nil {
		glog.Fatalln("failed to create the controldb volume", err)
	}

	glog.Infoln("create the controldb volume", volID)

	err = serverIns.WaitVolumeCreated(ctx, volID)
	if err != nil {
		serverIns.DeleteVolume(ctx, volID)
		glog.Fatalln("volume is not at available status", err, volID)
	}

	// create the controldb service
	serviceUUID := utils.GenControlDBServiceUUID(volID)
	logConfig := logIns.CreateLogConfigForStream(ctx, cluster,
		common.ControlDBServiceName, serviceUUID, common.ControlDBServiceName)

	commonOpts := &containersvc.CommonOptions{
		Cluster:        cluster,
		ServiceName:    common.ControlDBServiceName,
		ServiceUUID:    serviceUUID,
		ContainerImage: common.ControlDBContainerImage,
		Resource: &common.Resources{
			MaxCPUUnits:     common.DefaultMaxCPUUnits,
			ReserveCPUUnits: common.ControlDBReserveCPUUnits,
			MaxMemMB:        common.ControlDBMaxMemMB,
			ReserveMemMB:    common.ControlDBReserveMemMB,
		},
		LogConfig: logConfig,
	}

	kv := &common.EnvKeyValuePair{
		Name:  common.ENV_CONTAINER_PLATFORM,
		Value: *platform,
	}

	p := common.PortMapping{
		ContainerPort: common.ControlDBServerPort,
		HostPort:      common.ControlDBServerPort,
	}

	createOpts := &containersvc.CreateServiceOptions{
		Common:        commonOpts,
		ContainerPath: common.ControlDBDefaultDir,
		PortMappings:  []common.PortMapping{p},
		Replicas:      int64(1),
		Envkvs:        []*common.EnvKeyValuePair{kv},
	}

	err = containersvcIns.CreateService(ctx, createOpts)
	if err != nil {
		glog.Fatalln("create controlDB service error", err, common.ControlDBServiceName, "serviceUUID", serviceUUID)
	}

	glog.Infoln("created the controlDB service")
}

func waitControlDBReady(ctx context.Context, cluster string, containersvcIns containersvc.ContainerSvc) {
	// wait the controldb service is running
	for sec := int64(0); sec < common.DefaultServiceWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		status, err := containersvcIns.GetServiceStatus(ctx, cluster, common.ControlDBServiceName)
		if err != nil {
			// The service is successfully created. It may be possible there are some
			// temporary error, such as network error. For example, ECS may return MISSING
			// for the GET right after the service creation.
			// Here just log the GetServiceStatus error and retry.
			glog.Errorln("GetServiceStatus error", err)
		} else {
			if status.RunningCount == status.DesiredCount {
				glog.Infoln("The controldb service is ready")
				return
			}
		}

		time.Sleep(time.Duration(common.DefaultRetryWaitSeconds) * time.Second)
	}

	glog.Fatalln("Wait the controldb service ready timeout")
}

func waitSystemTablesReady(ctx context.Context, dbIns db.DB) {
	for sec := int64(0); sec < common.DefaultServiceWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		tableStatus, ready, err := dbIns.SystemTablesReady(ctx)
		if err != nil {
			glog.Errorln("SystemTablesReady check failed", err)
		} else {
			if ready {
				glog.Infoln("system tables are ready")
				return
			}

			if tableStatus != db.TableStatusCreating {
				glog.Fatalln("unexpected table status", tableStatus)
			}
		}

		glog.Infoln("tables are not ready, sleep and check again")
		time.Sleep(time.Duration(common.DefaultRetryWaitSeconds) * time.Second)
	}

	glog.Fatalln("Wait the SystemTablesReady timeout")
}
