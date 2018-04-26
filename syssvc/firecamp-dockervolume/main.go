package main

import (
	"flag"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/docker/go-plugins-helpers/volume"
	"github.com/golang/glog"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/containersvc/awsecs"
	"github.com/cloudstax/firecamp/containersvc/swarm"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/db/awsdynamodb"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/dns/awsroute53"
	"github.com/cloudstax/firecamp/plugins/volume"
	"github.com/cloudstax/firecamp/server"
	"github.com/cloudstax/firecamp/server/awsec2"
	"github.com/cloudstax/firecamp/utils"
)

const socketAddress = "/run/docker/plugins/" + common.SystemName + "vol.sock"

var (
	platform  = flag.String("container-platform", common.ContainerPlatformECS, "The underline container platform: ecs or swarm, default: ecs")
	tlsVerify = flag.Bool("tlsverify", false, "Whether enable tls verify to talk with swarm manager")
	caFile    = flag.String("tlscacert", "", "The CA file")
	certFile  = flag.String("tlscert", "", "The TLS server certificate file")
	keyFile   = flag.String("tlskey", "", "The TLS server key file")
)

func main() {
	flag.Parse()

	utils.SetLogDir()

	// not log error to std
	flag.Set("stderrthreshold", "FATAL")

	// testmode is to test the driver locally
	testmode := os.Getenv("TESTMODE")
	if ok, _ := strconv.ParseBool(testmode); ok {
		glog.Infoln("new test volume driver")

		driver := dockervolume.NewVolumeDriver(db.NewMemDB(), dns.NewMockDNS(), server.NewMemServer(),
			server.NewMockServerInfo(), containersvc.NewMemContainerSvc(), containersvc.NewMockContainerSvcInfo())

		h := volume.NewHandler(driver)
		err := h.ServeUnix(socketAddress, 0)
		if err != nil {
			glog.Fatalln("ServeUnix failed, error", err)
		}
		return
	}

	targetPlatform := *platform
	// when the driver runs as the docker plugin, could only set the configs via the os environment variable.
	envplatform := os.Getenv("PLATFORM")
	if envplatform != targetPlatform {
		targetPlatform = envplatform
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

	var info containersvc.Info
	var containersvcIns containersvc.ContainerSvc

	switch targetPlatform {
	case common.ContainerPlatformECS:
		info, err = awsecs.NewEcsInfo()
		if err != nil {
			// The volume driver service will start after the ECS agent service starts.
			// But the ECS agent service start does NOT mean the ecs agent container is started.
			// Retry after sleep.
			glog.Errorln("NewEcsInfo error", err)
			sleepSeconds := time.Duration(5) * time.Second
			time.Sleep(sleepSeconds)
			info, err = awsecs.NewEcsInfo()
			if err != nil {
				glog.Fatalln("NewEcsInfo error", err)
			}
		}

		containersvcIns = awsecs.NewAWSEcs(sess)

	case common.ContainerPlatformSwarm:
		cluster := os.Getenv(common.ENV_CLUSTER)
		if len(cluster) == 0 {
			glog.Fatalln("Swarm cluster name is not set")
		}

		sinfo, err := swarmsvc.NewSwarmInfo(cluster)
		if err != nil {
			glog.Fatalln("NewSwarmInfo error", err)
		}

		info = sinfo

		containersvcIns, err = swarmsvc.NewSwarmSvcOnWorkerNode(region, cluster)
		if err != nil {
			glog.Fatalln("NewSwarmSvcOnWorkerNode error", err)
		}

	default:
		glog.Fatalln("unsupport container platform", targetPlatform)
	}

	ec2Info, err := awsec2.NewEc2Info(sess)
	if err != nil {
		glog.Fatalln("NewEc2Info error", err)
	}

	dbIns := awsdynamodb.NewDynamoDB(sess, info.GetContainerClusterID())

	ec2Ins := awsec2.NewAWSEc2(sess)
	dnsIns := awsroute53.NewAWSRoute53(sess)

	driver := dockervolume.NewVolumeDriver(dbIns, dnsIns, ec2Ins, ec2Info, containersvcIns, info)

	glog.Infoln("NewScDriver, platform", targetPlatform, "ec2InstanceID", ec2Info.GetLocalInstanceID(), "region", region,
		"ContainerInstanceID", info.GetLocalContainerInstanceID(),
		"tlsVerify", *tlsVerify, "ca file", *caFile, "cert file", *certFile, "key file", *keyFile)
	glog.Flush()

	h := volume.NewHandler(driver)

	glog.Infoln("start listening on", socketAddress)
	err = h.ServeUnix(socketAddress, 0)
	if err != nil {
		glog.Fatalln("ServeUnix failed, error", err)
	}
}
