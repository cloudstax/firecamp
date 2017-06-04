package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/containersvc"
	"github.com/cloudstax/openmanage/containersvc/awsecs"
	"github.com/cloudstax/openmanage/containersvc/swarm"
	"github.com/cloudstax/openmanage/db"
	"github.com/cloudstax/openmanage/db/awsdynamodb"
	"github.com/cloudstax/openmanage/db/controldb/client"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/dns/awsroute53"
	"github.com/cloudstax/openmanage/manage/server"
	"github.com/cloudstax/openmanage/server/awsec2"
	"github.com/cloudstax/openmanage/utils"
)

var (
	platform      = flag.String("container-platform", common.ContainerPlatformECS, "The underline container platform: ecs or swarm, default: ecs")
	manageDNSName = flag.String("dnsname", "", "the dns name of the management service. Default: "+dns.GetDefaultManageServiceURL("cluster", false))
	managePort    = flag.Int("port", common.ManageHTTPServerPort, "port that the manage http service listens on")
	dbtype        = flag.String("dbtype", common.DBTypeCloudDB, "The db type, such as the AWS DynamoDB or the embedded controldb")
	tlsEnabled    = flag.Bool("tlsverify", false, "whether TLS is enabled")
	caFile        = flag.String("tlscacert", "", "The CA file")
	certFile      = flag.String("tlscert", "", "The TLS server certificate file")
	keyFile       = flag.String("tlskey", "", "The TLS server key file")
)

func main() {
	flag.Parse()

	utils.SetLogDir()

	if *tlsEnabled && (*certFile == "" || *keyFile == "") {
		fmt.Println("invalid command, please pass cert file and key file for tls", *certFile, *keyFile)
		os.Exit(-1)
	}

	if *platform != common.ContainerPlatformECS && *platform != common.ContainerPlatformSwarm {
		fmt.Println("not supported container platform", *platform)
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

	dnsIns := awsroute53.NewAWSRoute53(sess)

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

	var dbIns db.DB
	switch *dbtype {
	case common.DBTypeCloudDB:
		dbIns = awsdynamodb.NewDynamoDB(sess)
	case common.DBTypeControlDB:
		addr := dns.GetDefaultControlDBAddr(cluster)
		dbIns = controldbcli.NewControlDBCli(addr)
	}

	err = manageserver.StartServer(cluster, *manageDNSName, *managePort, containersvcIns,
		dbIns, dnsIns, serverInfo, serverIns, *tlsEnabled, *caFile, *certFile, *keyFile)

	glog.Fatalln("StartServer error", err)
}
