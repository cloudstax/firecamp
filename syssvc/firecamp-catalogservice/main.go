package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"

	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/jazzl0ver/firecamp/catalog/manageservice"
	"github.com/jazzl0ver/firecamp/pkg/dns"
	"github.com/jazzl0ver/firecamp/pkg/dns/awsroute53"
	"github.com/jazzl0ver/firecamp/pkg/server/awsec2"
	"github.com/jazzl0ver/firecamp/pkg/utils"
)

var (
	platform      = flag.String("container-platform", common.ContainerPlatformECS, "The underline container platform: ecs or swarm, default: ecs")
	cluster       = flag.String("cluster", "", "The cluster name")
	zones         = flag.String("availability-zones", "", "The availability zones for the system, example: us-east-1a,us-east-1b,us-east-1c")
	serverDNSName = flag.String("dnsname", "", "the dns name of the catalog service. Default: "+dns.GetDefaultCatalogServiceDNSName("cluster"))
	serverPort    = flag.Int("port", common.CatalogServerHTTPPort, "port that the catalog http service listens on")
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

	glog.Infoln("create catalog service, container platform", *platform, ", availability zones", azs, ", k8snamespace", k8snamespace)

	dnsIns := awsroute53.NewAWSRoute53(sess)

	err = catalogsvc.StartServer(*platform, *cluster, azs, *serverDNSName, *serverPort,
		dnsIns, serverInfo, *tlsEnabled, *caFile, *certFile, *keyFile)
	if err != nil {
		glog.Fatalln("StartServer error", err)
	}
}
