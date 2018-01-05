package main

import (
	"flag"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/db/awsdynamodb"
	"github.com/cloudstax/firecamp/dns/awsroute53"
	"github.com/cloudstax/firecamp/docker/network"
	"github.com/cloudstax/firecamp/docker/volume"
	"github.com/cloudstax/firecamp/server/awsec2"
	"github.com/cloudstax/firecamp/utils"
)

// For now, the init container is only used by K8s.
// The init container will update the DNS record or static ip if necessary,
// and create/update the config files if necessary.
// In the future, will integrate with the external dns project.

const (
	prestopStaticIPFile = "prestop-staticip"
)

var (
	op          = flag.String("op", containersvc.InitContainerOpInit, "pod init or stop")
	cluster     = flag.String("cluster", "", "The cluster name")
	serviceName = flag.String("service-name", "", "The service name")
	memberIndex = flag.Int64("member-index", -1, "The service member index")
)

func main() {
	flag.Parse()

	if *op == containersvc.InitContainerOpTest {
		glog.Infoln("test op, cluster", *cluster, "service", *serviceName, "member index", *memberIndex)
		return
	}

	if *op == containersvc.InitContainerOpStop {
		// this will be called in the pod pre-stop hook
		prestopfilepath := filepath.Join(common.DefaultConfigPath, prestopStaticIPFile)
		exist, err := utils.IsFileExist(prestopfilepath)
		if err != nil {
			glog.Fatalln("check prestop file exist error", err, prestopfilepath)
		}

		if exist {
			// prestopfile exists. Must be the service that requires static ip. delete the ip from network.
			b, err := ioutil.ReadFile(prestopfilepath)
			if err != nil {
				glog.Fatalln("read prestop file error", err, prestopfilepath)
			}

			ip := string(b)
			err = dockernetwork.DeleteIP(ip, dockernetwork.DefaultNetworkInterface)
			if err != nil {
				glog.Fatalln("DeleteIP error", err, ip)
			}
			glog.Infoln("deleted ip", ip, "from network", dockernetwork.DefaultNetworkInterface)
		} else {
			glog.Infoln("not require to delete static ip")
		}
		return
	}

	if *op != containersvc.InitContainerOpInit {
		panic("invalid op, please specify init or stop")
	}

	if len(*cluster) == 0 || len(*serviceName) == 0 || *memberIndex == -1 {
		panic("please specify the cluster name, service name and member index")
	}

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

	// TODO implement DB on top of K8s ConfigMap
	dbIns := awsdynamodb.NewDynamoDB(sess, *cluster)
	dnsIns := awsroute53.NewAWSRoute53(sess)
	ec2Ins := awsec2.NewAWSEc2(sess)
	ec2Info, err := awsec2.NewEc2Info(sess)
	if err != nil {
		glog.Fatalln("NewEc2Info error", err)
	}

	netIns := dockernetwork.NewServiceNetwork(dbIns, dnsIns, ec2Ins, ec2Info)

	tstr := strconv.FormatInt(time.Now().Unix(), 10)
	mindexstr := strconv.FormatInt(*memberIndex, 10)
	requuid := *serviceName + common.NameSeparator + mindexstr + common.NameSeparator + tstr

	ctx := context.Background()
	ctx = utils.NewRequestContext(ctx, requuid)

	svc, err := dbIns.GetService(ctx, *cluster, *serviceName)
	if err != nil {
		glog.Fatalln("GetService error", err, *serviceName, *memberIndex, "requuid", requuid)
	}

	attr, err := dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Fatalln("GetServiceAttr error", err, *memberIndex, "requuid", requuid, svc)
	}

	member, err := dbIns.GetServiceMember(ctx, svc.ServiceUUID, *memberIndex)
	if err != nil {
		glog.Fatalln("GetServiceMember error", err, *memberIndex, "requuid", requuid, attr)
	}

	// update DNS if required
	if attr.RegisterDNS && !attr.RequireStaticIP {
		err = netIns.UpdateDNS(ctx, attr.DomainName, attr.HostedZoneID, member)
		if err != nil {
			glog.Fatalln("UpdateDNS error", err, "requuid", requuid, member)
		}
	}

	// update static ip if required
	if attr.RequireStaticIP {
		err = netIns.UpdateStaticIP(ctx, attr.DomainName, member)
		if err != nil {
			glog.Fatalln("UpdateStaticIP error", err, "requuid", requuid, member)
		}

		// create the prestop file, so the container stop could delete the attached ip.
		prestopfilepath := filepath.Join(common.DefaultConfigPath, prestopStaticIPFile)
		exist, err := utils.IsFileExist(prestopfilepath)
		if err != nil {
			glog.Fatalln("check the prestop file error", err, prestopfilepath, "requuid", requuid, member)
		}
		if !exist {
			glog.Infoln("create the prestop file", prestopfilepath, "requuid", requuid, member)
			err = utils.CreateOrOverwriteFile(prestopfilepath, []byte(member.StaticIP), common.DefaultConfigFileMode)
			if err != nil {
				glog.Fatalln("create the prestop file error", err, prestopfilepath, "requuid", requuid, member)
			}
		}

		err = netIns.AddIP(member.StaticIP)
		if err != nil {
			glog.Fatalln("AddIP error", err, "requuid", requuid, member)
		}
	}

	// create the config files if necessary
	err = dockervolume.CreateConfigFile(ctx, common.DefaultConfigPath, member, dbIns)
	if err != nil {
		if attr.RequireStaticIP {
			deliperr := netIns.DeleteIP(member.StaticIP)
			glog.Errorln("DeleteIP error", deliperr, "requuid", requuid, member)
		}

		glog.Fatalln("CreateConfigFile error", err, "requuid", requuid, member)
	}

	glog.Infoln("successfully updated dns record or static ip, and created config file for service",
		*serviceName, "cluster", *cluster, "memberIndex", *memberIndex)
}
