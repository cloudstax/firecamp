package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/catalog/mongodb"
	"github.com/cloudstax/openmanage/catalog/postgres"
	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/manage"
	"github.com/cloudstax/openmanage/manage/client"
	"github.com/cloudstax/openmanage/server/awsec2"
	"github.com/cloudstax/openmanage/syssvc"
	"github.com/cloudstax/openmanage/utils"
)

// The script is a simple demo for how to create the sc postgres service, the ECS task definition and service.
var (
	op          = flag.String("op", "", "The service operation: create|delete")
	serviceType = flag.String("service-type", "", "The supported service type by this command: mongodb|postgres")
	region      = flag.String("region", "", "The target AWS region")
	cluster     = flag.String("cluster", "default", "The ECS cluster")
	service     = flag.String("service", "", "The target service name in ECS")
	serverURL   = flag.String("server-url", "", "the management service url, default: "+dns.GetDefaultManageServiceURL("cluster", false))
	tlsEnabled  = flag.Bool("tls-enabled", false, "whether tls is enabled")
	caFile      = flag.String("ca-file", "", "the ca file")
	certFile    = flag.String("cert-file", "", "the cert file")
	keyFile     = flag.String("key-file", "", "the key file")

	// the common service creation parameters
	replicas        = flag.Int64("replicas", 1, "The number of replicas for the service")
	azs             = flag.String("zones", "", "The availability zones for the replicas, separated by \",\". Example: us-west-2a,us-west-2b,us-west-2c")
	volSizeGB       = flag.Int64("volume-size", 0, "The size of each EBS volume, unit: GB")
	reserveCPUUnits = flag.Int64("cpu-units", 128, "The number of cpu units reserved for the container")
	reserveMemMB    = flag.Int64("memory", 128, "The memory reserved for the container, unit: MB")
	port            = flag.Int64("port", 0, "The service listening port")

	// The mongodb service creation specific parameters.
	// The mongodb replicaSetName. If not set, use service name
	replicaSetName = flag.String("replica-set-name", "", "The replica set name, default: service name")

	// The postgres service creation specific parameters.
	pgpasswd   = flag.String("passwd", "password", "The PostgreSQL DB password")
	replUser   = flag.String("replication-user", "repluser", "The replication user that the standby DB replicates from the primary")
	replPasswd = flag.String("replication-passwd", "replpassword", "The password for the standby DB to access the primary")
)

const (
	opCreate        = "create"
	opDelete        = "delete"
	serviceMongoDB  = "mongodb"
	servicePostgres = "postgres"
)

func usage() {
	flag.Usage = func() {
		switch *op {
		case opCreate:
			fmt.Println("usage: openmanage-service-cli -op=create -service-type=<mongodb|postgres> [OPTIONS]")
			flag.PrintDefaults()
		case opDelete:
			fmt.Println("usage: openmanage-service-cli -op=delete -region=us-west-1 -cluster=default -service=aaa")
		default:
			fmt.Println("usage: openmanage-service-cli -op=<create|delete> --help")
		}
	}
}

func main() {
	usage()
	flag.Parse()
	defer glog.Flush()

	switch *op {
	case opCreate:
		fmt.Println("create")
	case opDelete:
		fmt.Println("delete")
	default:
		fmt.Println("Please specify the valid operation command: create|delete", *op)
		flag.Usage()
		os.Exit(-1)
	}

	if *service == "" {
		fmt.Println("Please specify the valid service name")
		flag.Usage()
		os.Exit(-1)
	}

	var err error
	awsRegion := *region
	if awsRegion == "" {
		awsRegion, err = awsec2.GetLocalEc2Region()
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
		createService(ctx, cli, awsRegion, mgtserverurl)

	case opDelete:
		deleteService(ctx, cli, awsRegion)

	default:
		fmt.Println("Invalid operation, please specify create or delete")
		os.Exit(-1)
	}
}

func createService(ctx context.Context, cli *client.ManageClient, region string, mgtserverurl string) {
	var zones []string
	if *azs != "" {
		// parse azs
		azSep := ","
		zones = strings.Split(*azs, azSep)
	} else {
		config := aws.NewConfig().WithRegion(region)
		sess, err := session.NewSession(config)
		if err != nil {
			fmt.Println("No availability zone specified, but create aws session failed", err)
			os.Exit(-1)
		}
		zones, err = awsec2.GetRegionAZs(region, sess)
		if err != nil {
			fmt.Println("No availability zone specified, but get region zones failed", err)
			os.Exit(-1)
		}
	}
	if len(zones) < int(*replicas) {
		fmt.Println("Warning: the number of zones is less than the number of replicas")
	}

	if *volSizeGB == 0 || *port == 0 {
		fmt.Println("Please specify availability zones, volume size and listening port")
		os.Exit(-1)
	}

	switch *serviceType {
	case serviceMongoDB:
		replSetName := *replicaSetName
		if replSetName == "" {
			replSetName = *service
		}

		replicaCfgs := mongodbcatalog.GenReplicaConfigs(zones, *replicas, replSetName, *port)
		createAndWaitService(ctx, cli, region, replicaCfgs, mongodbcatalog.ContainerImage)

		initializeMongodb(ctx, cli, region, replSetName, mgtserverurl)

	case servicePostgres:
		replicaCfgs := pgcatalog.GenReplicaConfigs(*cluster, *service, zones, *replicas, *port, *pgpasswd, *replUser, *replPasswd)
		createAndWaitService(ctx, cli, region, replicaCfgs, pgcatalog.ContainerImage)

		initializePostgreSQL(ctx, cli, region)

	default:
		fmt.Println("Please specify the service to create: mongodb|postgres")
		os.Exit(-1)
	}
}

func deleteService(ctx context.Context, cli *client.ManageClient, region string) {
	// list all volumes
	volIDs, err := serviceophelper.ListServiceVolumes(ctx, cli, region, *cluster, *service)
	if err != nil {
		fmt.Println("ListServiceVolumes error", err)
		os.Exit(-1)
	}

	// delete the service
	err = serviceophelper.DeleteService(ctx, cli, region, *cluster, *service)
	if err != nil {
		fmt.Println("DeleteService error", err)
		os.Exit(-1)
	}

	fmt.Println("Service deleted, please manually delete the EBS volumes", volIDs)
}

func createAndWaitService(ctx context.Context, cli *client.ManageClient, region string, replicaCfgs []*manage.ReplicaConfig, containerImage string) {
	req := &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     *cluster,
			ServiceName: *service,
		},

		Resource: &common.Resources{
			MaxCPUUnits:     *reserveCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *reserveMemMB,
			ReserveMemMB:    *reserveMemMB,
		},

		ContainerImage: containerImage,
		Replicas:       *replicas,
		VolumeSizeGB:   *volSizeGB,
		ContainerPath:  common.DefaultContainerMountPath,
		Port:           *port,

		HasMembership:  true,
		ReplicaConfigs: replicaCfgs,
	}

	err := cli.CreateService(ctx, req)
	if err != nil {
		fmt.Println("CreateService error", err)
		os.Exit(-1)
	}

	fmt.Println("Service is created, wait for all containers running")

	err = serviceophelper.WaitServiceRunning(ctx, cli, req.Service)
	if err != nil {
		fmt.Println("WaitServiceRunning error", err)
		os.Exit(-1)
	}

	fmt.Println("All service containers are running")
}

func initializeMongodb(ctx context.Context, cli *client.ManageClient, region string, replSetName string, mgtserverurl string) {
	// TODO if not waiting enough time, InitializeService will get error. Not sure why. wait DNS update?
	fmt.Println("All service containers are running, wait 10 seconds for service to stabilize")
	time.Sleep(time.Duration(10) * time.Second)

	fmt.Println("Start initializing the MongoDB ReplicaSet")

	// generate the parameters for the init task
	envkvs := mongodbcatalog.GenInitTaskEnvKVPairs(region, *cluster, *service, replSetName, *replicas, mgtserverurl)

	initReq := &manage.RunTaskRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     *cluster,
			ServiceName: *service,
		},

		Resource: &common.Resources{
			MaxCPUUnits:     *reserveCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *reserveMemMB,
			ReserveMemMB:    *reserveMemMB,
		},

		ContainerImage: mongodbcatalog.InitContainerImage,
		TaskType:       common.TaskTypeInit,
		Envkvs:         envkvs,
	}

	// run the init task to initialize the service
	err := serviceophelper.InitializeService(ctx, cli, region, initReq)
	if err != nil {
		fmt.Println("InitializeService error", err)
		os.Exit(-1)
	}

	fmt.Println("The mongodb replicaset are successfully initialized")
}

func initializePostgreSQL(ctx context.Context, cli *client.ManageClient, region string) {
	req := &manage.ServiceCommonRequest{
		Region:      region,
		Cluster:     *cluster,
		ServiceName: *service,
	}

	err := cli.SetServiceInitialized(ctx, req)
	if err != nil {
		fmt.Println("InitializeService error", err)
		os.Exit(-1)
	}

	fmt.Println("The PostgreSQl are successfully initialized")
}
