package main

import (
	"flag"

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
	"github.com/cloudstax/firecamp/plugins/network"
	"github.com/cloudstax/firecamp/server/awsec2"
	"github.com/cloudstax/firecamp/utils"
)

// This tool is used to select the member in the entrypoint.sh of the service's docker container.
// The stateless services that have multiple members

var (
	cluster      = flag.String("cluster", "", "The firecamp cluster")
	service      = flag.String("service-name", "", "The firecamp service name")
	memberIndex  = flag.Int64("member-index", -1, "The target service member index")
	contPlatform = flag.String("container-platform", common.ContainerPlatformECS, "The underline container platform: ecs or swarm, default: ecs")
	dbtype       = flag.String("dbtype", common.DBTypeCloudDB, "The db type, such as the AWS DynamoDB or the embedded controldb")
	outputfile   = flag.String("outputfile", "/etc/firecamp-member", "The output file to store the assigned service member name")
)

func main() {
	flag.Parse()

	utils.SetLogDir()

	// not log error to std
	flag.Set("stderrthreshold", "INFO")

	if len(*cluster) == 0 || len(*service) == 0 {
		glog.Fatalln("please specify cluster", *cluster, "and service", *service)
	}

	region, err := awsec2.GetLocalEc2Region()
	if err != nil {
		glog.Fatalln("awsec2 GetLocalEc2Region error", err)
	}

	cfg := aws.NewConfig().WithRegion(region)
	sess, err := session.NewSession(cfg)
	if err != nil {
		glog.Fatalln("failed to create session, error", err)
	}

	ec2Info, err := awsec2.NewEc2Info(sess)
	if err != nil {
		glog.Fatalln("NewEc2Info error", err)
	}

	var dbIns db.DB
	switch *dbtype {
	case common.DBTypeCloudDB:
		dbIns = awsdynamodb.NewDynamoDB(sess, *cluster)
	case common.DBTypeControlDB:
		addr := dns.GetDefaultControlDBAddr(*cluster)
		dbIns = controldbcli.NewControlDBCli(addr)
	}

	dnsIns := awsroute53.NewAWSRoute53(sess)

	ec2Ins := awsec2.NewAWSEc2(sess)

	snet := dockernetwork.NewServiceNetwork(dbIns, dnsIns, ec2Ins, ec2Info)

	var info containersvc.Info
	var containersvcIns containersvc.ContainerSvc

	switch *contPlatform {
	case common.ContainerPlatformECS:
		info, err = awsecs.NewEcsInfo()
		if err != nil {
			glog.Fatalln("NewEcsInfo error", err)
		}

		containersvcIns = awsecs.NewAWSEcs(sess)

	case common.ContainerPlatformSwarm:
		sinfo, err := swarmsvc.NewSwarmInfo(*cluster)
		if err != nil {
			glog.Fatalln("NewSwarmInfo error", err)
		}

		info = sinfo

		containersvcIns, err = swarmsvc.NewSwarmSvcOnWorkerNode(region, *cluster)
		if err != nil {
			glog.Fatalln("NewSwarmSvcOnWorkerNode error", err)
		}

	default:
		glog.Fatalln("unsupport container platform", *contPlatform)
	}

	ctx := context.Background()
	memberName, domain, err := snet.UpdateServiceMemberDNS(ctx, *cluster, *service, *memberIndex, containersvcIns, info.GetLocalContainerInstanceID())
	if err != nil {
		glog.Fatalln("UpdateServiceMemberDNS error", err, "service", *service, "memberIndex", *memberIndex)
	}

	memberHost := dns.GenDNSName(memberName, domain)

	err = utils.CreateOrOverwriteFile(*outputfile, []byte(memberHost), utils.DefaultFileMode)
	if err != nil {
		glog.Fatalln("create outputfile", *outputfile, "error", err, "service", *service, "memberName", memberName, "index", *memberIndex)
	}

	glog.Infoln("assign member", memberName, "index", *memberIndex, "for service", *service)

	glog.Flush()
}
