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

	"github.com/cloudstax/firecamp/api/catalog"
	"github.com/cloudstax/firecamp/api/manage"
	"github.com/cloudstax/firecamp/api/manage/client"
	"github.com/cloudstax/firecamp/catalog/cassandra"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/server/awsec2"
	"github.com/cloudstax/firecamp/utils"
)

// The tool is a simple demo for how to create the non-catalog service.
// The basic steps are:
// 1) Create the service at the firecamp control plane and the container platform.
//    You would need to generate the service replica configs for your own service.
//    This tool refers to the catalog service internal replica configs.
// 2) Wait till all service containers are running.
// 3) Run the initialization task.

const (
	serviceCassandra = "cassandra"
)

var (
	platform   = flag.String("container-platform", common.ContainerPlatformECS, "The underline container platform: ecs or swarm, default: ecs")
	tlsEnabled = flag.Bool("tls-enabled", false, "whether tls is enabled")
	caFile     = flag.String("ca-file", "", "the ca file")
	certFile   = flag.String("cert-file", "", "the cert file")
	keyFile    = flag.String("key-file", "", "the key file")

	// the common service creation parameters
	serviceType = flag.String("service-type", serviceCassandra, "The service type")
	region      = flag.String("region", "", "The target AWS region")
	cluster     = flag.String("cluster", "", "The ECS cluster")
	service     = flag.String("service", "", "The service name")
	replicas    = flag.Int64("replicas", 1, "The number of replicas for the service")
	azs         = flag.String("zones", "", "The availability zones for the replicas, separated by \",\". Example: us-west-2a,us-west-2b,us-west-2c")
	volSizeGB   = flag.Int64("volume-size", 0, "The size of each EBS volume, unit: GB")

	// The cassandra service creation specific parameters.
	heapSizeMB = flag.Int64("cas-heap-size", 512, "The cassandra service heap size, unit: MB")
)

func main() {
	flag.Parse()

	if *serviceType == "" || *cluster == "" || *service == "" || *volSizeGB == 0 {
		fmt.Println("Please specify the valid service type, cluster name, service name, volume size")
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

	// use default server url
	serverURL := dns.GetDefaultManageServiceURL(*cluster, *tlsEnabled)

	cli := client.NewManageClient(serverURL, tlsConf)
	ctx := context.Background()

	switch strings.ToLower(*serviceType) {
	case serviceCassandra:
		// 1. generate the service creation request
		opts := &catalog.CatalogCassandraOptions{
			Replicas: *replicas,
			Volume: &common.ServiceVolume{
				VolumeType:   common.VolumeTypeGPSSD,
				VolumeSizeGB: *volSizeGB,
			},
			JournalVolume: &common.ServiceVolume{
				VolumeType:   common.VolumeTypeGPSSD,
				VolumeSizeGB: *volSizeGB,
			},
			HeapSizeMB:      *heapSizeMB,
			JmxRemoteUser:   "jmxUser",
			JmxRemotePasswd: "jmxPasswd",
		}
		res := &common.Resources{}
		createReq, _, _ := cascatalog.GenDefaultCreateServiceRequest(*platform, *region, zones, *cluster, *service, opts, res)

		// 2. create the service
		err := cli.CreateService(ctx, createReq)
		if err != nil {
			fmt.Println("CreateService error", err)
			os.Exit(-1)
		}

		fmt.Println("Service is created, wait for all containers running")

		// 3. wait till all service containers are running
		waitServiceRunning(ctx, cli, createReq.Service)

		// 4. run the cassandra init task
		envs := cascatalog.GenInitTaskEnvKVPairs(*region, *cluster, *service, serverURL)
		initReq := &manage.RunTaskRequest{
			Service: createReq.Service,
			Resource: &common.Resources{
				MaxCPUUnits:     common.DefaultMaxCPUUnits,
				ReserveCPUUnits: common.DefaultReserveCPUUnits,
				MaxMemMB:        common.DefaultMaxMemoryMB,
				ReserveMemMB:    common.DefaultReserveMemoryMB,
			},
			ContainerImage: cascatalog.InitContainerImage,
			TaskType:       common.TaskTypeInit,
			Envkvs:         envs,
		}
		initializeService(ctx, cli, *region, initReq)

	default:
		fmt.Println("Please specify the service to create: mongodb|postgresql")
		os.Exit(-1)
	}
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
