package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/docker/go-plugins-helpers/volume"
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
	"github.com/cloudstax/openmanage/dockervolume"
	"github.com/cloudstax/openmanage/server/awsec2"
	"github.com/cloudstax/openmanage/utils"
)

var (
	platform  = flag.String("container-platform", common.ContainerPlatformECS, "The underline container platform: ecs or swarm, default: ecs")
	dbtype    = flag.String("dbtype", common.DBTypeCloudDB, "The db type, such as the AWS DynamoDB or the embedded controldb")
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
	switch *platform {
	case common.ContainerPlatformECS:
		info, err = awsecs.NewEcsInfo()
		if err != nil {
			glog.Fatalln("NewEcsInfo error", err)
		}

		containersvcIns = awsecs.NewAWSEcs(sess)

	case common.ContainerPlatformSwarm:
		if *tlsVerify && (*caFile == "" || *certFile == "" || *keyFile == "") {
			fmt.Println("invalid command, please pass cert file and key file for tls", *certFile, *keyFile)
			os.Exit(-1)
		}

		sinfo, err := swarmsvc.NewSwarmInfo()
		if err != nil {
			glog.Fatalln("NewSwarmInfo error", err)
		}

		info = sinfo

		containersvcIns = swarmsvc.NewSwarmSvc(sinfo.GetSwarmManagers(), *tlsVerify, *caFile, *certFile, *keyFile)

	default:
		glog.Fatalln("unsupport container platform", *platform)
	}

	ec2Info, err := awsec2.NewEc2Info(sess)
	if err != nil {
		glog.Fatalln("NewEc2Info error", err)
	}

	var dbIns db.DB
	switch *dbtype {
	case common.DBTypeCloudDB:
		dbIns = awsdynamodb.NewDynamoDB(sess)
	case common.DBTypeControlDB:
		addr := dns.GetDefaultControlDBAddr(info.GetContainerClusterID())
		dbIns = controldbcli.NewControlDBCli(addr)
	}

	ec2Ins := awsec2.NewAWSEc2(sess)
	dnsIns := awsroute53.NewAWSRoute53(sess)

	driver := openmanagedockervolume.NewVolumeDriver(dbIns, dnsIns, ec2Ins, ec2Info, containersvcIns, info)

	glog.Infoln("NewScDriver, ec2InstanceID", ec2Info.GetLocalInstanceID(), "region", region,
		"ContainerInstanceID", info.GetLocalContainerInstanceID(),
		"tlsVerify", *tlsVerify, "ca file", *caFile, "cert file", *certFile, "key file", *keyFile)
	glog.Flush()

	h := volume.NewHandler(driver)
	err = h.ServeUnix("docker", common.VolumeDriverName)
	if err != nil {
		glog.Fatalln("ServeUnix failed, error", err)
	}
}
