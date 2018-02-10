package main

import (
	"flag"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/dns/awsroute53"
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
)

func main() {
	flag.Parse()

	// log to std
	utils.SetLogToStd()

	if len(*cluster) == 0 || len(*serviceName) == 0 {
		panic(fmt.Sprintf("please specify the cluster name %s, service name %s", *cluster, *serviceName))
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

	dnsIns := awsroute53.NewAWSRoute53(sess)
	ec2Info, err := awsec2.NewEc2Info(sess)
	if err != nil {
		glog.Fatalln("NewEc2Info error", err)
	}

	tmstr := strconv.FormatInt(time.Now().Unix(), 10)
	requuid := fmt.Sprintf("%s-%s", *serviceName, tmstr)

	ctx := context.Background()
	ctx = utils.NewRequestContext(ctx, requuid)

	domain := dns.GenDefaultDomainName(*cluster)
	dnsName := dns.GenDNSName(*serviceName, domain)

	privateIP := ec2Info.GetPrivateIP()
	vpcID := ec2Info.GetLocalVpcID()
	vpcRegion := ec2Info.GetLocalRegion()

	private := true
	hostedZoneID, err := dnsIns.GetHostedZoneIDByName(ctx, domain, vpcID, vpcRegion, private)
	if err != nil {
		glog.Fatalln("GetHostedZoneIDByName error", err, "requuid", requuid, "domain", domain, "vpc", vpcID, "service", *serviceName)
	}

	err = dnsIns.UpdateDNSRecord(ctx, dnsName, privateIP, hostedZoneID)
	if err != nil {
		glog.Fatalln("UpdateDNS error", err, "requuid", requuid, "dnsName", dnsName,
			"service", *serviceName, "privateIP", privateIP, "hostedZoneID", hostedZoneID)
	}

	glog.Infoln("successfully updated dns record for service", *serviceName,
		"cluster", *cluster, "dnsName", dnsName, "privateIP", privateIP)

	glog.Flush()
}
