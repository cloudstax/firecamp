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
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/catalog/mongodb"
	"github.com/cloudstax/openmanage/catalog/postgres"
	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/manage"
	"github.com/cloudstax/openmanage/manage/client"
	"github.com/cloudstax/openmanage/server/awsec2"
	"github.com/cloudstax/openmanage/utils"
)

// The tool is a simple demo for how to create the non-catalog service.
// The basic steps are:
// 1) Create the service at the openmanage control plane and the container platform.
//    You would need to generate the service replica configs for your own service.
//    This tool refers to the catalog service internal replica configs.
// 2) Wait till all service containers are running.
// 3) Run the initialization task.

var (
	serverURL  = flag.String("server-url", "", "the management service url, default: "+dns.GetDefaultManageServiceURL("cluster", false))
	tlsEnabled = flag.Bool("tls-enabled", false, "whether tls is enabled")
	caFile     = flag.String("ca-file", "", "the ca file")
	certFile   = flag.String("cert-file", "", "the cert file")
	keyFile    = flag.String("key-file", "", "the key file")

	// the common service creation parameters
	serviceType     = flag.String("service-type", "", "The supported service type by this command: mongodb|postgresql")
	region          = flag.String("region", "", "The target AWS region")
	cluster         = flag.String("cluster", "default", "The ECS cluster")
	service         = flag.String("service", "", "The target service name in ECS")
	replicas        = flag.Int64("replicas", 1, "The number of replicas for the service")
	azs             = flag.String("zones", "", "The availability zones for the replicas, separated by \",\". Example: us-west-2a,us-west-2b,us-west-2c")
	volSizeGB       = flag.Int64("volume-size", 0, "The size of each EBS volume, unit: GB")
	reserveCPUUnits = flag.Int64("cpu-units", 128, "The number of cpu units reserved for the container")
	reserveMemMB    = flag.Int64("memory", 128, "The memory reserved for the container, unit: MB")
	port            = flag.Int64("port", 0, "The service listening port")

	// security parameters
	admin       = flag.String("admin", "admin", "The DB admin")
	adminPasswd = flag.String("passwd", "password", "The DB admin password")

	// The mongodb service creation specific parameters.
	// The mongodb replicaSetName. If not set, use service name
	replSetName = flag.String("replica-set-name", "", "The replica set name, default: service name")

	// The postgres service creation specific parameters.
	replUser   = flag.String("replication-user", "repluser", "The replication user that the standby DB replicates from the primary")
	replPasswd = flag.String("replication-passwd", "replpassword", "The password for the standby DB to access the primary")
)

const (
	serviceMongoDB  = "mongodb"
	servicePostgres = "postgresql"
)

func main() {
	flag.Parse()

	if *serviceType == "" || *service == "" || *volSizeGB == 0 || *port == 0 {
		fmt.Println("Please specify the valid service type, service name, volume size and listening port")
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

	var zones []string
	if *azs != "" {
		// parse azs
		azSep := ","
		zones = strings.Split(*azs, azSep)
	} else {
		config := aws.NewConfig().WithRegion(*region)
		sess, err := session.NewSession(config)
		if err != nil {
			fmt.Println("No availability zone specified, but create aws session failed", err)
			os.Exit(-1)
		}
		zones, err = awsec2.GetRegionAZs(*region, sess)
		if err != nil {
			fmt.Println("No availability zone specified, but get region zones failed", err)
			os.Exit(-1)
		}
	}
	if len(zones) < int(*replicas) {
		fmt.Println("Warning: the number of zones is less than the number of replicas")
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

	if *serverURL == "" {
		// use default server url
		*serverURL = dns.GetDefaultManageServiceURL(*cluster, *tlsEnabled)
	} else {
		// format url
		*serverURL = dns.FormatManageServiceURL(*serverURL, *tlsEnabled)
	}

	cli := client.NewManageClient(*serverURL, tlsConf)
	ctx := context.Background()

	switch strings.ToLower(*serviceType) {
	case serviceMongoDB:
		if *replSetName == "" {
			*replSetName = *service
		}

		replicaCfgs, err := mongodbcatalog.GenReplicaConfigs(zones, *replicas, *replSetName, *port)
		if err != nil {
			fmt.Println("GenReplicaConfigs error", err)
			os.Exit(-1)
		}
		createAndWaitService(ctx, cli, replicaCfgs, mongodbcatalog.ContainerImage)

		initializeMongodb(ctx, cli)

	case servicePostgres:
		replicaCfgs := pgcatalog.GenReplicaConfigs(*cluster, *service, zones, *replicas, *port, *adminPasswd, *replUser, *replPasswd)
		createAndWaitService(ctx, cli, replicaCfgs, pgcatalog.ContainerImage)

		initializePostgreSQL(ctx, cli)

	default:
		fmt.Println("Please specify the service to create: mongodb|postgresql")
		os.Exit(-1)
	}
}

func createAndWaitService(ctx context.Context, cli *client.ManageClient, replicaCfgs []*manage.ReplicaConfig, containerImage string) {
	// create the service at the openmanage control plane and the container platform.
	req := &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      *region,
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

	// wait till all service containers are running
	waitServiceRunning(ctx, cli, req.Service)
}

func initializeMongodb(ctx context.Context, cli *client.ManageClient) {
	// TODO if not waiting enough time, initializeService will get error. Not sure why. wait DNS update?
	fmt.Println("All service containers are running, wait 10 seconds for service to stabilize")
	time.Sleep(time.Duration(10) * time.Second)

	fmt.Println("Start initializing the MongoDB ReplicaSet")

	// generate the parameters for the init task
	envkvs := mongodbcatalog.GenInitTaskEnvKVPairs(*region, *cluster, *service, *replSetName, *replicas, *serverURL, *admin, *adminPasswd)

	initReq := &manage.RunTaskRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      *region,
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
	initializeService(ctx, cli, *region, initReq)

	fmt.Println("The mongodb replicaset are successfully initialized")
}

func initializePostgreSQL(ctx context.Context, cli *client.ManageClient) {
	req := &manage.ServiceCommonRequest{
		Region:      *region,
		Cluster:     *cluster,
		ServiceName: *service,
	}

	err := cli.SetServiceInitialized(ctx, req)
	if err != nil {
		fmt.Println("initializeService error", err)
		os.Exit(-1)
	}

	fmt.Println("The PostgreSQl are successfully initialized")
}

func waitServiceRunning(ctx context.Context, cli *client.ManageClient, r *manage.ServiceCommonRequest) {
	sleepSeconds := time.Duration(common.DefaultRetryWaitSeconds) * time.Second
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
				fmt.Println("All service containers are running")
				return
			}
		}

		time.Sleep(sleepSeconds)
	}

	fmt.Println("not all service containers are running after", common.DefaultServiceWaitSeconds)
	os.Exit(-1)
}

func initializeService(ctx context.Context, cli *client.ManageClient, region string, req *manage.RunTaskRequest) {
	// check if service is initialized.
	r := &manage.ServiceCommonRequest{
		Region:      region,
		Cluster:     req.Service.Cluster,
		ServiceName: req.Service.ServiceName,
	}

	initialized, err := cli.IsServiceInitialized(ctx, r)
	if err != nil {
		fmt.Println("check service initialized error", err)
		os.Exit(-1)
	}
	if initialized {
		fmt.Println("service is initialized")
		return
	}

	delReq := &manage.DeleteTaskRequest{
		Service:  req.Service,
		TaskType: req.TaskType,
	}
	defer cli.DeleteTask(ctx, delReq)

	getReq := &manage.GetTaskStatusRequest{
		Service: req.Service,
		TaskID:  "",
	}

	// run the init task
	for i := 0; i < common.DefaultTaskRetryCounts; i++ {
		taskID, err := cli.RunTask(ctx, req)
		if err != nil {
			fmt.Println("run init task error", err)
			os.Exit(-1)
		}

		// wait init task done
		getReq.TaskID = taskID
		waitTask(ctx, cli, getReq)

		// check if service is initialized
		initialized, err := cli.IsServiceInitialized(ctx, req.Service)
		if err != nil {
			fmt.Println("check service initialized error", err)
		} else {
			if initialized {
				fmt.Println("service is initialized")
				return
			}
			fmt.Println("service is initializing")
		}
	}

	fmt.Println("service is not initialized, please retry it later")
	os.Exit(-1)
}

func waitTask(ctx context.Context, cli *client.ManageClient, r *manage.GetTaskStatusRequest) {
	fmt.Println("wait the running task", r.TaskID)

	sleepSeconds := time.Duration(common.DefaultRetryWaitSeconds) * time.Second
	for sec := int64(0); sec < common.DefaultTaskWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		// wait some time for the task to run
		time.Sleep(sleepSeconds)

		taskStatus, err := cli.GetTaskStatus(ctx, r)
		if err != nil {
			// wait task error, but there is no way to know whether the task finishes its work or not.
			// go ahead to check service status
			fmt.Println("wait init task complete error", err)
		} else {
			// check if task completes
			fmt.Println("task status", taskStatus)
			if taskStatus.Status == common.TaskStatusStopped {
				return
			}
		}
	}
}
