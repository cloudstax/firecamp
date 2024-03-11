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

	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/jazzl0ver/firecamp/pkg/containersvc"
	"github.com/jazzl0ver/firecamp/pkg/containersvc/awsecs"
	"github.com/jazzl0ver/firecamp/pkg/containersvc/k8s"
	"github.com/jazzl0ver/firecamp/pkg/containersvc/swarm"
	"github.com/jazzl0ver/firecamp/pkg/db"
	"github.com/jazzl0ver/firecamp/pkg/db/awsdynamodb"
	"github.com/jazzl0ver/firecamp/pkg/db/k8sconfigdb"
	"github.com/jazzl0ver/firecamp/pkg/dns"
	"github.com/jazzl0ver/firecamp/pkg/dns/awsroute53"
	"github.com/jazzl0ver/firecamp/pkg/log/awscloudwatch"
	"github.com/jazzl0ver/firecamp/pkg/managesvc"
	"github.com/jazzl0ver/firecamp/pkg/server/awsec2"
	"github.com/jazzl0ver/firecamp/pkg/utils"
)

var (
	platform      = flag.String("container-platform", common.ContainerPlatformECS, "The underline container platform: ecs or swarm, default: ecs")
	cluster       = flag.String("cluster", "", "The cluster name")
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

	if *cluster == "" || *zones == "" {
		fmt.Println("Invalid command, please specify the cluster name and availability zones for the system")
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

	var containersvcIns containersvc.ContainerSvc
	switch *platform {
	case common.ContainerPlatformECS:
		containersvcIns = awsecs.NewAWSEcs(sess)

	case common.ContainerPlatformSwarm:
		containersvcIns, err = swarmsvc.NewSwarmSvcOnManagerNode(azs)
		if err != nil {
			glog.Fatalln("NewSwarmSvcOnManagerNode error", err)
		}

	case common.ContainerPlatformK8s:
		containersvcIns, err = k8ssvc.NewK8sSvc(*cluster, common.CloudPlatformAWS, *dbtype, k8snamespace)
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
		dbIns = awsdynamodb.NewDynamoDB(sess, *cluster)

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

	err = managesvc.StartServer(*platform, *cluster, azs, *manageDNSName, *managePort, containersvcIns,
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
