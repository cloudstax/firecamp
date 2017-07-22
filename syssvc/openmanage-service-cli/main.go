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
	op              = flag.String("op", "", "The operation type, such as create-service")
	serviceType     = flag.String("service-type", "", "The catalog service type: mongodb|postgresql")
	cluster         = flag.String("cluster", "default", "The ECS cluster")
	serverURL       = flag.String("server-url", "", "the management service url, default: "+dns.GetDefaultManageServiceURL("cluster", false))
	region          = flag.String("region", "", "The target AWS region")
	service         = flag.String("service-name", "", "The target service name in ECS")
	replicas        = flag.Int64("replicas", 3, "The number of replicas for the service")
	volSizeGB       = flag.Int64("volume-size", 0, "The size of each EBS volume, unit: GB")
	maxCPUUnits     = flag.Int64("max-cpuunits", common.DefaultMaxCPUUnits, "The max number of cpu units for the container")
	reserveCPUUnits = flag.Int64("reserve-cpuunits", common.DefaultReserveCPUUnits, "The number of cpu units to reserve for the container")
	maxMemMB        = flag.Int64("max-memory", common.DefaultMaxMemoryMB, "The max memory for the container, unit: MB")
	reserveMemMB    = flag.Int64("reserve-memory", common.DefaultReserveMemoryMB, "The memory reserved for the container, unit: MB")

	// security parameters
	admin       = flag.String("admin", "dbadmin", "The DB admin. For PostgreSQL, use default user \"postgres\"")
	adminPasswd = flag.String("passwd", "changeme", "The DB admin password")
	tlsEnabled  = flag.Bool("tls-enabled", false, "whether tls is enabled")
	caFile      = flag.String("ca-file", "", "the ca file")
	certFile    = flag.String("cert-file", "", "the cert file")
	keyFile     = flag.String("key-file", "", "the key file")

	// The postgres service creation specific parameters.
	replUser       = flag.String("replication-user", "repluser", "The replication user that the standby DB replicates from the primary")
	replUserPasswd = flag.String("replication-passwd", "replpassword", "The password for the standby DB to access the primary")

	// the parameters for getting the config file
	serviceUUID = flag.String("service-uuid", "", "The service uuid for getting the service's config file")
	fileID      = flag.String("fileid", "", "The service config file id")
)

const (
	defaultPGAdmin = "postgres"

	opCreate      = "create-service"
	opCheckInit   = "check-service-init"
	opDelete      = "delete-service"
	opList        = "list-services"
	opGet         = "get-service"
	opListMembers = "list-members"
	opGetConfig   = "get-config"
)

func usage() {
	flag.Usage = func() {
		switch *op {
		case opCreate:
			fmt.Printf("usage: openmanage-catalogservice-cli -op=%s -service-type=<mongodb|postgresql> [OPTIONS]\n", opCreate)
			flag.PrintDefaults()
		case opCheckInit:
			fmt.Printf("usage: openmanage-catalogservice-cli -op=%s -region=us-west-1 -cluster=default -service-name=aaa -admin=admin -passwd=passwd\n", opCheckInit)
		case opDelete:
			fmt.Printf("usage: openmanage-catalogservice-cli -op=%s -region=us-west-1 -cluster=default -service-name=aaa\n", opDelete)
		case opList:
			fmt.Printf("usage: openmanage-catalogservice-cli -op=%s -region=us-west-1 -cluster=default\n", opList)
		case opGet:
			fmt.Printf("usage: openmanage-catalogservice-cli -op=%s -region=us-west-1 -cluster=default -service-name=aaa\n", opGet)
		case opListMembers:
			fmt.Printf("usage: openmanage-catalogservice-cli -op=%s -region=us-west-1 -cluster=default -service-name=aaa\n", opListMembers)
		case opGetConfig:
			fmt.Printf("usage: openmanage-catalogservice-cli -op=%s -region=us-west-1 -cluster=default -service-uuid=serviceuuid -fileid=configfileID\n", opGetConfig)
		default:
			fmt.Printf("usage: openmanage-catalogservice-cli -op=<%s|%s|%s|%s|%s|%s|%s> --help\n",
				opCreate, opCheckInit, opDelete, opList, opGet, opListMembers, opGetConfig)
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

	case opListMembers:
		members := listServiceMembers(ctx, cli)
		fmt.Printf("List %d members:\n", len(members))
		for _, member := range members {
			fmt.Printf("\t%+v\n", *member)
			for _, cfg := range member.Configs {
				fmt.Printf("\t\t%+v\n", *cfg)
			}
		}

	case opGetConfig:
		getConfig(ctx, cli)

	default:
		fmt.Printf("Invalid operation, please specify %s|%s|%s|%s|%s|%s\n",
			opCreate, opCheckInit, opDelete, opList, opGet, opListMembers)
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
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Replicas:     *replicas,
		VolumeSizeGB: *volSizeGB,
		Admin:        *admin,
		AdminPasswd:  *adminPasswd,
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
		Admin:       *admin,
		AdminPasswd: *adminPasswd,
	}

	sleepSeconds := time.Duration(5) * time.Second
	for sec := int64(0); sec < common.DefaultServiceWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		initialized, statusMsg, err := cli.CatalogCheckServiceInit(ctx, initReq)
		if err == nil {
			if initialized {
				fmt.Println("The catalog service is initialized")
				return
			}
			fmt.Println(statusMsg)
		} else {
			fmt.Println("check service init error", err)
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
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Replicas:       *replicas,
		VolumeSizeGB:   *volSizeGB,
		Admin:          defaultPGAdmin,
		AdminPasswd:    *adminPasswd,
		ReplUser:       *replUser,
		ReplUserPasswd: *replUserPasswd,
	}

	err := cli.CatalogCreatePostgreSQLService(ctx, req)
	if err != nil {
		fmt.Println("create postgresql service error", err)
		os.Exit(-1)
	}

	fmt.Println("The postgresql service is created, wait for all containers running")

	waitServiceRunning(ctx, cli, req.Service)
}

func waitServiceRunning(ctx context.Context, cli *client.ManageClient, r *manage.ServiceCommonRequest) {
	sleepSeconds := time.Duration(5) * time.Second
	for sec := int64(0); sec < common.DefaultServiceWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		status, err := cli.GetServiceStatus(ctx, r)
		if err != nil {
			// The service is successfully created. It may be possible there are some
			// temporary error, such as network error. For example, ECS may return MISSING
			// for the GET right after the service creation.
			// Here just log the GetServiceStatus error and retry.
			fmt.Println("GetServiceStatus error", err, r)
		} else {
			if status.RunningCount == status.DesiredCount {
				fmt.Println("All service containers are running, RunningCount", status.RunningCount)
				return
			}
			fmt.Println("wait the service containers running, RunningCount", status.RunningCount)
		}

		time.Sleep(sleepSeconds)
	}

	fmt.Println("not all service containers are running after", common.DefaultServiceWaitSeconds)
	os.Exit(-1)
}

func checkServiceInit(ctx context.Context, cli *client.ManageClient) {
	req := &manage.CatalogCheckServiceInitRequest{
		ServiceType: *serviceType,
		Service: &manage.ServiceCommonRequest{
			Region:      *region,
			Cluster:     *cluster,
			ServiceName: *service,
		},
		Admin:       *admin,
		AdminPasswd: *adminPasswd,
	}

	initialized, statusMsg, err := cli.CatalogCheckServiceInit(ctx, req)
	if err != nil {
		fmt.Println("check service init error", err)
		return
	}

	if initialized {
		fmt.Println("service initialized")
	} else {
		fmt.Println(statusMsg)
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

	fmt.Printf("List %d services:\n", len(services))
	for _, svc := range services {
		fmt.Printf("\t%+v\n", *svc)
	}
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

	fmt.Printf("%+v\n", *attr)
}

func listServiceMembers(ctx context.Context, cli *client.ManageClient) []*common.ServiceMember {
	// list all service members
	serviceReq := &manage.ServiceCommonRequest{
		Region:      *region,
		Cluster:     *cluster,
		ServiceName: *service,
	}

	listReq := &manage.ListServiceMemberRequest{
		Service: serviceReq,
	}

	members, err := cli.ListServiceMember(ctx, listReq)
	if err != nil {
		fmt.Println("ListServiceMember error", err)
		os.Exit(-1)
	}

	return members
}

func deleteService(ctx context.Context, cli *client.ManageClient) {
	// list all service members
	members := listServiceMembers(ctx, cli)

	volIDs := make([]string, len(members))
	for i, member := range members {
		volIDs[i] = member.VolumeID
	}

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

func getConfig(ctx context.Context, cli *client.ManageClient) {
	if *serviceUUID == "" || *fileID == "" {
		fmt.Println("Please specify the service uuid and config file id")
		os.Exit(-1)
	}

	req := &manage.GetConfigFileRequest{
		Region:      *region,
		Cluster:     *cluster,
		ServiceUUID: *serviceUUID,
		FileID:      *fileID,
	}

	cfg, err := cli.GetConfigFile(ctx, req)
	if err != nil {
		fmt.Println("GetConfigFile error", err)
		os.Exit(-1)
	}

	fmt.Println("%+v\n", *cfg)
}
