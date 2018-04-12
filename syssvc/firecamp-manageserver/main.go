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
	"github.com/cloudstax/firecamp/containersvc/k8s"
	"github.com/cloudstax/firecamp/containersvc/swarm"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/db/awsdynamodb"
	"github.com/cloudstax/firecamp/db/k8sconfigdb"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/dns/awsroute53"
	"github.com/cloudstax/firecamp/log/awscloudwatch"
	"github.com/cloudstax/firecamp/manage/server"
	"github.com/cloudstax/firecamp/server/awsec2"
	"github.com/cloudstax/firecamp/utils"
)

var (
	platform      = flag.String("container-platform", common.ContainerPlatformECS, "The underline container platform: ecs or swarm, default: ecs")
	dbtype        = flag.String("dbtype", common.DBTypeCloudDB, "The db type")
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

	if *platform != common.ContainerPlatformECS &&
		*platform != common.ContainerPlatformSwarm &&
		*platform != common.ContainerPlatformK8s {
		fmt.Println("not supported container platform", *platform)
		os.Exit(-1)
	}

	if *zones == "" {
		fmt.Println("Invalid command, please specify the availability zones for the system")
		os.Exit(-1)
	}

	k8snamespace := ""
	if *platform == common.ContainerPlatformK8s {
		k8snamespace = os.Getenv(common.ENV_K8S_NAMESPACE)
		if len(k8snamespace) == 0 {
			glog.Infoln("k8s namespace is not set. set to default")
			k8snamespace = common.DefaultK8sNamespace
		}
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

	glog.Infoln("create manageserver, container platform", *platform, ", dbtype", *dbtype, ", availability zones", azs, ", k8snamespace", k8snamespace)

	dnsIns := awsroute53.NewAWSRoute53(sess)
	logIns := awscloudwatch.NewLog(sess, region, *platform, k8snamespace)

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
		cluster = os.Getenv(common.ENV_CLUSTER)
		if len(cluster) == 0 {
			glog.Fatalln("Swarm cluster name is not set")
		}

		info, err := swarmsvc.NewSwarmInfo(cluster)
		if err != nil {
			glog.Fatalln("NewSwarmInfo error", err)
		}

		cluster = info.GetContainerClusterID()
		containersvcIns, err = swarmsvc.NewSwarmSvcOnManagerNode(azs)
		if err != nil {
			glog.Fatalln("NewSwarmSvcOnManagerNode error", err)
		}

	case common.ContainerPlatformK8s:
		cluster = os.Getenv(common.ENV_CLUSTER)
		if len(cluster) == 0 {
			glog.Fatalln("K8s cluster name is not set")
		}

		fullhostname, err := awsec2.GetLocalEc2Hostname()
		if err != nil {
			glog.Fatalln("get local ec2 hostname error", err)
		}
		info := k8ssvc.NewK8sInfo(cluster, fullhostname)
		cluster = info.GetContainerClusterID()

		containersvcIns, err = k8ssvc.NewK8sSvc(cluster, common.CloudPlatformAWS, *dbtype, k8snamespace)
		if err != nil {
			glog.Fatalln("NewK8sSvc error", err)
		}

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

	case common.DBTypeK8sDB:
		dbIns, err = k8sconfigdb.NewK8sConfigDB(k8snamespace)
		if err != nil {
			glog.Fatalln("NewK8sConfigDB error", err)
		}

	default:
		glog.Fatalln("unknown db type", *dbtype)
	}

	err = manageserver.StartServer(*platform, cluster, azs, *manageDNSName, *managePort, containersvcIns,
		dbIns, dnsIns, logIns, serverInfo, serverIns, *tlsEnabled, *caFile, *certFile, *keyFile)
	if err != nil {
		glog.Fatalln("StartServer error", err)
	}
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
