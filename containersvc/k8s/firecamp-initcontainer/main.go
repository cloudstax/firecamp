package main

import (
	"flag"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc/k8s"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/db/awsdynamodb"
	"github.com/cloudstax/firecamp/db/controldb/client"
	"github.com/cloudstax/firecamp/db/k8sconfigdb"
	"github.com/cloudstax/firecamp/dns"
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
var (
	cluster     = flag.String("cluster", "", "The cluster name")
	serviceName = flag.String("service-name", "", "The service name")
	memberIndex = flag.Int64("member-index", -1, "The service member index")
	dbtype      = flag.String("dbtype", common.DBTypeK8sDB, "The db type, such as k8sdb or dynamodb")
)

func main() {
	flag.Parse()

	// log to std
	utils.SetLogToStd()

	testmode := os.Getenv("TESTMODE")
	if ok, _ := strconv.ParseBool(testmode); ok {
		glog.Infoln("test op, cluster", *cluster, "service", *serviceName, "member index", *memberIndex)
		return
	}

	if len(*cluster) == 0 || len(*serviceName) == 0 || *memberIndex == -1 {
		panic("please specify the cluster name, service name and member index")
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

	var dbIns db.DB
	switch *dbtype {
	case common.DBTypeCloudDB:
		dbIns = awsdynamodb.NewDynamoDB(sess, *cluster)

	case common.DBTypeControlDB:
		addr := dns.GetDefaultControlDBAddr(*cluster)
		dbIns = controldbcli.NewControlDBCli(addr)

	case common.DBTypeK8sDB:
		namespace := os.Getenv(common.ENV_K8S_NAMESPACE)
		if len(namespace) == 0 {
			glog.Fatalln("k8s namespace is not set")
		}
		dbIns, err = k8sconfigdb.NewK8sConfigDB(namespace)
		if err != nil {
			glog.Fatalln("NewK8sConfigDB error", err)
		}

	default:
		glog.Fatalln("unknown db type", *dbtype)
	}

	dnsIns := awsroute53.NewAWSRoute53(sess)
	ec2Ins := awsec2.NewAWSEc2(sess)
	ec2Info, err := awsec2.NewEc2Info(sess)
	if err != nil {
		glog.Fatalln("NewEc2Info error", err)
	}

	netIns := dockernetwork.NewServiceNetwork(dbIns, dnsIns, ec2Ins, ec2Info)

	tmstr := strconv.FormatInt(time.Now().Unix(), 10)
	mindexstr := strconv.FormatInt(*memberIndex, 10)
	requuid := *serviceName + common.NameSeparator + mindexstr + common.NameSeparator + tmstr

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

		// create the staticip file, so the container stop could delete the attached ip.
		err = k8ssvc.CreateStaticIPFile(member.StaticIP)
		if err != nil {
			glog.Fatalln("create the staticip file error", err, "requuid", requuid, member)
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

	glog.Flush()
}
