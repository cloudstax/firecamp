package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/catalog"
	"github.com/cloudstax/openmanage/containersvc/awsecs"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/server/awsec2"
)

// The script is a simple demo for how to create the sc postgres service, the ECS task definition and service.
var (
	cluster    = flag.String("cluster", "default", "The ECS cluster")
	service    = flag.String("service", "", "The target service name in ECS")
	region     = flag.String("region", "", "The target AWS region")
	serverURL  = flag.String("server-url", "", "the management service url, default: "+dns.GetDefaultManageServiceURL("cluster", false))
	tlsEnabled = flag.Bool("tlsEnabled", false, "whether tls is enabled")
	caFile     = flag.String("ca-file", "", "the ca file")
	certFile   = flag.String("cert-file", "", "the cert file")
	keyFile    = flag.String("key-file", "", "the key file")
)

func usage() {
	fmt.Println("usage: openmanage-aws-service-deletion -service=aaa")
}

func main() {
	flag.Parse()
	defer glog.Flush()

	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}

	mgtserverurl := *serverURL
	if mgtserverurl == "" {
		// use default server url
		mgtserverurl = dns.GetDefaultManageServiceURL(*cluster, *tlsEnabled)
	} else {
		// format url
		mgtserverurl = dns.FormatManageServiceURL(mgtserverurl, *tlsEnabled)
	}

	var err error
	awsRegion := *region
	if awsRegion == "" {
		awsRegion, err = awsec2.GetLocalEc2Region()
		if err != nil {
			fmt.Println("please input the correct AWS region")
			os.Exit(-1)
		}
	}

	config := aws.NewConfig().WithRegion(awsRegion)
	sess, err := session.NewSession(config)
	if err != nil {
		glog.Fatalln("failed to create aws session, error", err)
	}

	ecsIns := awsecs.NewAWSEcs(sess)

	sop, err := serviceop.NewServiceOp(ecsIns, awsRegion,
		mgtserverurl, *tlsEnabled, *caFile, *certFile, *keyFile)
	if err != nil {
		glog.Fatalln(err)
	}

	ctx := context.Background()

	// list all volumes
	volIDs, err := sop.ListServiceVolumes(ctx, *cluster, *service)
	if err != nil {
		glog.Fatalln("ListServiceVolumes error", err, "service", *service, "cluster", *cluster)
	}

	fmt.Println("Please manually delete the EBS volumes", volIDs, "for service", *service)

	err = sop.DeleteService(ctx, *cluster, *service)
	if err != nil {
		glog.Fatalln("DeleteService", *service, "error", err, "cluster", *cluster)
	}

	fmt.Println("deleted service", *service)
}
