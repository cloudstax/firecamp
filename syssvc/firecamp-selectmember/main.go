package main

import (
	"flag"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/jazzl0ver/firecamp/pkg/containersvc"
	"github.com/jazzl0ver/firecamp/pkg/containersvc/awsecs"
	"github.com/jazzl0ver/firecamp/pkg/containersvc/swarm"
	"github.com/jazzl0ver/firecamp/pkg/db/awsdynamodb"
	"github.com/jazzl0ver/firecamp/pkg/dns/awsroute53"
	"github.com/jazzl0ver/firecamp/pkg/plugins/network"
	"github.com/jazzl0ver/firecamp/pkg/server/awsec2"
	"github.com/jazzl0ver/firecamp/pkg/utils"
)

// This tool is used to select the member in the entrypoint.sh of the service's docker container.
// The stateless services that have multiple members

var (
	cluster      = flag.String("cluster", "", "The firecamp cluster")
	service      = flag.String("service-name", "", "The firecamp service name")
	memberName   = flag.String("member-name", "", "The target service member name")
	contPlatform = flag.String("container-platform", common.ContainerPlatformECS, "The underline container platform: ecs or swarm, default: ecs")
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

	dbIns := awsdynamodb.NewDynamoDB(sess, *cluster)

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
	memberHost, err := snet.UpdateServiceMemberDNS(ctx, *cluster, *service, *memberName, containersvcIns, info.GetLocalContainerInstanceID())
	if err != nil {
		glog.Fatalln("UpdateServiceMemberDNS error", err, "service", *service, "memberName", *memberName)
	}

	content := fmt.Sprintf("SERVICE_MEMBER=%s", memberHost)

	err = utils.CreateOrOverwriteFile(*outputfile, []byte(content), utils.DefaultFileMode)
	if err != nil {
		glog.Fatalln("create outputfile", *outputfile, "error", err, "service", *service, "member", memberHost)
	}

	glog.Infoln("updated member dns", memberHost, "for service", *service)

	glog.Flush()
}
