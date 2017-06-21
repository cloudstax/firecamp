package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"time"

	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/catalog"
	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/manage"
	"github.com/cloudstax/openmanage/manage/client"
	"github.com/cloudstax/openmanage/server/awsec2"
	"github.com/cloudstax/openmanage/utils"
)

// The catalog service command tool to create/query the catalog service.

var (
	op           = flag.String("op", opCreate, "The operation type: create, query")
	serviceType  = flag.String("service-type", "", "The catalog service type, such as MongoDBReplicaSet, PostgreSQL, etc")
	cluster      = flag.String("cluster", "default", "The ECS cluster")
	serverURL    = flag.String("server-url", "", "the management service url, default: "+dns.GetDefaultManageServiceURL("cluster", false))
	tlsEnabled   = flag.Bool("tls-enabled", false, "whether tls is enabled")
	caFile       = flag.String("ca-file", "", "the ca file")
	certFile     = flag.String("cert-file", "", "the cert file")
	keyFile      = flag.String("key-file", "", "the key file")
	region       = flag.String("region", "", "The target AWS region")
	service      = flag.String("service", "", "The target service name in ECS")
	volSizeGB    = flag.Int64("volume-size", 0, "The size of each EBS volume, unit: GB")
	cpuUnits     = flag.Int64("cpu-units", 128, "The number of cpu units to reserve for the container")
	reserveMemMB = flag.Int64("soft-memory", 128, "The memory reserved for the container, unit: MB")
)

const (
	opCreate = "create"
	opQuery  = "query"
)

func usage() {
	fmt.Println("usage: openmanage-catalogservice -op=create -cluster=c1 -service=mongodb -volume-size=100")
}

func main() {
	flag.Parse()

	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *tlsEnabled && (*caFile == "" || *certFile == "" || *keyFile == "") {
		fmt.Println("tls enabled without ca file %s cert file %s or key file %s", caFile, certFile, keyFile)
		os.Exit(-1)
	}

	switch *serviceType {
	case catalog.CatalogService_MongoDBReplicaSet:
	case catalog.CatalogService_PostgreSQL:
	default:
		fmt.Println("please specify the valid catalog service type")
		os.Exit(-1)
	}

	var err error
	awsRegion := *region
	if awsRegion == "" {
		awsRegion, err = awsec2.GetLocalEc2Region()
		if err != nil {
			fmt.Println("please specify the correct AWS region")
			os.Exit(-1)
		}
	}

	var tlsConf *tls.Config
	if *tlsEnabled {
		// TODO how to pass the ca/cert/key files to container? one option is: store them in S3.
		tlsConf, err = utils.GenClientTLSConfig(*caFile, *certFile, *keyFile)
		if err != nil {
			fmt.Println("GenClientTLSConfig error", err, "ca file", *caFile, "cert file", *certFile, "key file", *keyFile)
			os.Exit(-1)
		}
	}

	mgtserverurl := *serverURL
	if mgtserverurl == "" {
		// use default server url
		mgtserverurl = dns.GetDefaultManageServiceURL(*cluster, *tlsEnabled)
	} else {
		// format url
		mgtserverurl = dns.FormatManageServiceURL(mgtserverurl, *tlsEnabled)
	}

	cli := client.NewManageClient(mgtserverurl, tlsConf)
	ctx := context.Background()

	switch *op {
	case opCreate:
		createService(ctx, cli, awsRegion)
	case opQuery:
		queryService(ctx, cli, awsRegion)
	default:
		fmt.Println("Invalid operation, please specify ", opCreate, " or ", opQuery)
	}
}

func createService(ctx context.Context, cli *client.ManageClient, region string) {
	if *volSizeGB == 0 {
		fmt.Println("please specify the valid volume size")
		os.Exit(-1)
	}

	req := &manage.CatalogCreateServiceRequest{
		ServiceType: *serviceType,
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     *cluster,
			ServiceName: *service,
		},
		Resource: &common.Resources{
			MaxCPUUnits:     *cpuUnits,
			ReserveCPUUnits: *cpuUnits,
			MaxMemMB:        *reserveMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		VolumeSizeGB: *volSizeGB,
	}

	err := cli.CatalogCreateService(ctx, req)
	if err != nil {
		fmt.Println("create catalog service error", err, "cluster", *cluster, "service", *service)
		return
	}

	fmt.Println("The catalog service is created, wait till it gets initialized")

	initReq := &manage.CatalogCheckServiceInitRequest{
		ServiceType: *serviceType,
		Service:     req.Service,
	}
	sleepSeconds := time.Duration(common.DefaultRetryWaitSeconds) * time.Second
	for sec := int64(0); sec < common.DefaultServiceWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		initialized, err := cli.CatalogCheckServiceInit(ctx, initReq)
		if err != nil {
			fmt.Println("check service init error", err, "cluster", *cluster, "service", *service)
		} else if initialized {
			fmt.Println("The catalog service is initialized")
			return
		}
		time.Sleep(sleepSeconds)
	}

	fmt.Println("The catalog service is not initialized after", common.DefaultServiceWaitSeconds)
	os.Exit(-1)
}

func queryService(ctx context.Context, cli *client.ManageClient, region string) {
	req := &manage.CatalogCheckServiceInitRequest{
		ServiceType: *serviceType,
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     *cluster,
			ServiceName: *service,
		},
	}

	initialized, err := cli.CatalogCheckServiceInit(ctx, req)
	if err != nil {
		fmt.Println("check service init error", err, "cluster", *cluster, "service", *service)
		return
	}

	if initialized {
		fmt.Println("service initialized")
	} else {
		fmt.Println("service is initializing")
	}
}
