package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"strings"
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
	op           = flag.String("op", "", "The operation type, such as create-service")
	serviceType  = flag.String("service-type", "", "The catalog service type: mongodb|postgresql")
	cluster      = flag.String("cluster", "default", "The ECS cluster")
	serverURL    = flag.String("server-url", "", "the management service url, default: "+dns.GetDefaultManageServiceURL("cluster", false))
	tlsEnabled   = flag.Bool("tls-enabled", false, "whether tls is enabled")
	caFile       = flag.String("ca-file", "", "the ca file")
	certFile     = flag.String("cert-file", "", "the cert file")
	keyFile      = flag.String("key-file", "", "the key file")
	region       = flag.String("region", "", "The target AWS region")
	service      = flag.String("service", "", "The target service name in ECS")
	replicas     = flag.Int64("replicas", 3, "The number of replicas for the service")
	volSizeGB    = flag.Int64("volume-size", 0, "The size of each EBS volume, unit: GB")
	cpuUnits     = flag.Int64("cpu-units", 128, "The number of cpu units to reserve for the container")
	reserveMemMB = flag.Int64("soft-memory", 128, "The memory reserved for the container, unit: MB")

	// The postgres service creation specific parameters.
	admin          = flag.String("admin", "admin", "The DB admin")
	adminPasswd    = flag.String("passwd", "password", "The DB admin password")
	replUser       = flag.String("replication-user", "repluser", "The replication user that the standby DB replicates from the primary")
	replUserPasswd = flag.String("replication-passwd", "replpassword", "The password for the standby DB to access the primary")
)

const (
	opCreate    = "create-service"
	opCheckInit = "check-service-init"
	opDelete    = "delete-service"
	opList      = "list-services"
	opGet       = "get-service"
	opListVols  = "list-volumes"
)

func usage() {
	flag.Usage = func() {
		switch *op {
		case opCreate:
			fmt.Printf("usage: openmanage-catalogservice-cli -op=%s -service-type=<mongodb|postgres> [OPTIONS]\n", opCreate)
			flag.PrintDefaults()
		case opCheckInit:
			fmt.Printf("usage: openmanage-catalogservice-cli -op=%s -region=us-west-1 -cluster=default -service=aaa\n", opCheckInit)
		case opDelete:
			fmt.Printf("usage: openmanage-catalogservice-cli -op=%s -region=us-west-1 -cluster=default -service=aaa\n", opDelete)
		case opList:
			fmt.Printf("usage: openmanage-catalogservice-cli -op=%s -region=us-west-1 -cluster=default\n", opList)
		case opGet:
			fmt.Printf("usage: openmanage-catalogservice-cli -op=%s -region=us-west-1 -cluster=default -service=aaa\n", opGet)
		case opListVols:
			fmt.Printf("usage: openmanage-catalogservice-cli -op=%s -region=us-west-1 -cluster=default -service=aaa\n", opListVols)
		default:
			fmt.Printf("usage: openmanage-catalogservice-cli -op=<%s|%s|%s|%s|%s|%s> --help",
				opCreate, opCheckInit, opDelete, opList, opGet, opListVols)
		}
	}
}

func main() {
	usage()
	flag.Parse()

	if *op != opList && *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *tlsEnabled && (*caFile == "" || *certFile == "" || *keyFile == "") {
		fmt.Printf("tls enabled without ca file %s cert file %s or key file %s\n", caFile, certFile, keyFile)
		os.Exit(-1)
	}

	var err error
	if *region == "" {
		*region, err = awsec2.GetLocalEc2Region()
		if err != nil {
			fmt.Println("please specify the region")
			os.Exit(-1)
		}
	}

	var tlsConf *tls.Config
	if *tlsEnabled {
		// TODO how to pass the ca/cert/key files to container? one option is: store them in S3.
		tlsConf, err = utils.GenClientTLSConfig(*caFile, *certFile, *keyFile)
		if err != nil {
			fmt.Printf("GenClientTLSConfig error %s, ca file %s, cert file %s, key file %s\n", err, *caFile, *certFile, *keyFile)
			os.Exit(-1)
		}
	}

	if *serverURL == "" {
		// use default server url
		*serverURL = dns.GetDefaultManageServiceURL(*cluster, *tlsEnabled)
	} else {
		// format url
		*serverURL = dns.FormatManageServiceURL(*serverURL, *tlsEnabled)
	}

	cli := client.NewManageClient(*serverURL, tlsConf)
	ctx := context.Background()

	*serviceType = strings.ToLower(*serviceType)
	switch *op {
	case opCreate:
		switch *serviceType {
		case catalog.CatalogService_MongoDB:
			createMongoDBService(ctx, cli)
		case catalog.CatalogService_PostgreSQL:
			createPostgreSQLService(ctx, cli)
		default:
			fmt.Printf("Invalid service type, please specify %s|%s\n",
				catalog.CatalogService_MongoDB, catalog.CatalogService_PostgreSQL)
			os.Exit(-1)
		}

	case opCheckInit:
		checkServiceInit(ctx, cli)

	case opDelete:
		deleteService(ctx, cli)

	case opList:
		listServices(ctx, cli)

	case opGet:
		getService(ctx, cli)

	case opListVols:
		vols := listServiceVolumes(ctx, cli)
		fmt.Println(vols)

	default:
		fmt.Printf("Invalid operation, please specify %s|%s|%s|%s|%s|%s\n",
			opCreate, opCheckInit, opDelete, opList, opGet, opListVols)
		os.Exit(-1)
	}
}

func createMongoDBService(ctx context.Context, cli *client.ManageClient) {
	if *replicas == 0 || *volSizeGB == 0 {
		fmt.Println("please specify the valid replica number and volume size")
		os.Exit(-1)
	}

	req := &manage.CatalogCreateMongoDBRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      *region,
			Cluster:     *cluster,
			ServiceName: *service,
		},
		Resource: &common.Resources{
			MaxCPUUnits:     *cpuUnits,
			ReserveCPUUnits: *cpuUnits,
			MaxMemMB:        *reserveMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Replicas:     *replicas,
		VolumeSizeGB: *volSizeGB,
	}

	err := cli.CatalogCreateMongoDBService(ctx, req)
	if err != nil {
		fmt.Println("create catalog mongodb service error", err)
		os.Exit(-1)
	}

	fmt.Println("The catalog service is created, wait till it gets initialized")

	initReq := &manage.CatalogCheckServiceInitRequest{
		ServiceType: catalog.CatalogService_MongoDB,
		Service:     req.Service,
	}

	sleepSeconds := time.Duration(common.DefaultRetryWaitSeconds) * time.Second
	for sec := int64(0); sec < common.DefaultServiceWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		initialized, err := cli.CatalogCheckServiceInit(ctx, initReq)
		if err != nil {
			fmt.Println("check service init error", err)
		} else if initialized {
			fmt.Println("The catalog service is initialized")
			return
		}
		time.Sleep(sleepSeconds)
	}

	fmt.Println("The catalog service is not initialized after", common.DefaultServiceWaitSeconds)
	os.Exit(-1)
}

func createPostgreSQLService(ctx context.Context, cli *client.ManageClient) {
	if *replicas == 0 || *volSizeGB == 0 {
		fmt.Println("please specify the valid replica number and volume size")
		os.Exit(-1)
	}

	req := &manage.CatalogCreatePostgreSQLRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      *region,
			Cluster:     *cluster,
			ServiceName: *service,
		},
		Resource: &common.Resources{
			MaxCPUUnits:     *cpuUnits,
			ReserveCPUUnits: *cpuUnits,
			MaxMemMB:        *reserveMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Replicas:       *replicas,
		VolumeSizeGB:   *volSizeGB,
		DBAdmin:        *admin,
		AdminPasswd:    *adminPasswd,
		DBReplUser:     *replUser,
		ReplUserPasswd: *replUserPasswd,
	}

	err := cli.CatalogCreatePostgreSQLService(ctx, req)
	if err != nil {
		fmt.Println("create postgresql service error", err)
		os.Exit(-1)
	}

	fmt.Println("The postgresql service is created")
}

func checkServiceInit(ctx context.Context, cli *client.ManageClient) {
	req := &manage.CatalogCheckServiceInitRequest{
		ServiceType: *serviceType,
		Service: &manage.ServiceCommonRequest{
			Region:      *region,
			Cluster:     *cluster,
			ServiceName: *service,
		},
	}

	initialized, err := cli.CatalogCheckServiceInit(ctx, req)
	if err != nil {
		fmt.Println("check service init error", err)
		return
	}

	if initialized {
		fmt.Println("service initialized")
	} else {
		fmt.Println("service is initializing")
	}
}

func listServices(ctx context.Context, cli *client.ManageClient) {
	req := &manage.ListServiceRequest{
		Region:  *region,
		Cluster: *cluster,
		Prefix:  "",
	}

	services, err := cli.ListService(ctx, req)
	if err != nil {
		fmt.Println("ListService error", err)
		os.Exit(-1)
	}

	fmt.Println(services)
}

func getService(ctx context.Context, cli *client.ManageClient) {
	req := &manage.ServiceCommonRequest{
		Region:      *region,
		Cluster:     *cluster,
		ServiceName: *service,
	}

	attr, err := cli.GetServiceAttr(ctx, req)
	if err != nil {
		fmt.Println("GetServiceAttr error", err)
		os.Exit(-1)
	}

	fmt.Println(attr)
}

func listServiceVolumes(ctx context.Context, cli *client.ManageClient) (volIDs []string) {
	// list all volumes
	serviceReq := &manage.ServiceCommonRequest{
		Region:      *region,
		Cluster:     *cluster,
		ServiceName: *service,
	}

	listReq := &manage.ListVolumeRequest{
		Service: serviceReq,
	}

	vols, err := cli.ListVolume(ctx, listReq)
	if err != nil {
		fmt.Println("ListVolume error", err)
		os.Exit(-1)
	}

	volIDs = make([]string, len(vols))
	for i, vol := range vols {
		volIDs[i] = vol.VolumeID
	}
	return volIDs
}

func deleteService(ctx context.Context, cli *client.ManageClient) {
	// list all volumes
	volIDs := listServiceVolumes(ctx, cli)

	// delete the service from the control plane.
	serviceReq := &manage.ServiceCommonRequest{
		Region:      *region,
		Cluster:     *cluster,
		ServiceName: *service,
	}

	err := cli.DeleteService(ctx, serviceReq)
	if err != nil {
		fmt.Println("DeleteService error", err)
		os.Exit(-1)
	}

	fmt.Println("Service deleted, please manually delete the EBS volumes\n\t", volIDs)
}
