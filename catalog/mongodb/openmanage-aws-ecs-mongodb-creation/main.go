package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/catalog"
	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/containersvc/awsecs"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/manage"
	"github.com/cloudstax/openmanage/server/awsec2"
	"github.com/cloudstax/openmanage/utils"
)

// The script is a simple demo for how to create the openmanage mongodb service, the ECS task definition and service.
var (
	cluster    = flag.String("cluster", "default", "The ECS cluster")
	serverURL  = flag.String("server-url", "", "the management service url, default: "+dns.GetDefaultManageServiceURL("cluster", false))
	tlsEnabled = flag.Bool("tlsEnabled", false, "whether tls is enabled")
	caFile     = flag.String("ca-file", "", "the ca file")
	certFile   = flag.String("cert-file", "", "the cert file")
	keyFile    = flag.String("key-file", "", "the key file")
	region     = flag.String("region", "", "The target AWS region")
	az         = flag.String("availability-zone", "", "The avaibility zone in which to create the volume")
	service    = flag.String("service", "", "The target service name in ECS")
	replicas   = flag.Int64("replicas", 1, "The number of replicas for the service")
	// The mongodb replicaSetName. If not set, use service name
	replicaSetName = flag.String("replica-set-name", "", "The replica set name, default: service name")
	volSizeGB      = flag.Int64("volume-size", 0, "The size of each EBS volume, unit: GB")
	cpuUnits       = flag.Int64("cpu-units", 128, "The number of cpu units to reserve for the container")
	maxMemMB       = flag.Int64("max-memory", 128, "The hard limit for memory, unit: MB")
	reserveMemMB   = flag.Int64("soft-memory", 128, "The memory reserved for the container, unit: MB")
	port           = flag.Int64("port", 27017, "The mongodb listening port")
)

const (
	ContainerImage     = common.ContainerNamePrefix + "mongodb"
	InitContainerImage = common.ContainerNamePrefix + "mongodb-init"
	EnvReplicaSetName  = "REPLICA_SET_NAME"
	ContainerPath      = common.DefaultContainerMountPath
)

func usage() {
	fmt.Println("usage: openmanage-aws-ecs-mongodb-creation -op=create-service -region=us-west-1")
}

func main() {
	flag.Parse()
	defer glog.Flush()

	if *service == "" || *replicas == 0 || *volSizeGB == 0 {
		fmt.Println("please specify the valid service name, replicas, volume size")
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

	replSetName := *replicaSetName
	if replSetName == "" {
		replSetName = *service
	}

	var err error
	awsRegion := *region
	awsAz := *az
	if awsRegion == "" {
		awsRegion, err = awsec2.GetLocalEc2Region()
		if err != nil {
			fmt.Println("please input the correct AWS region")
			os.Exit(-1)
		}
	}
	if awsAz == "" {
		awsAz, err = awsec2.GetLocalEc2AZ()
		if err != nil {
			fmt.Println("please input the correct AWS availability zone")
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

	// generate service ReplicaConfigs
	replicaCfgs := genReplicaConfigs(*replicas, awsAz, replSetName, *port)

	req := &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      awsRegion,
			Cluster:     *cluster,
			ServiceName: *service,
		},

		Resource: &common.Resources{
			MaxCPUUnits:     *cpuUnits,
			ReserveCPUUnits: *cpuUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},

		ContainerImage: ContainerImage,
		Replicas:       *replicas,
		VolumeSizeGB:   *volSizeGB,
		ContainerPath:  ContainerPath,
		Port:           *port,

		HasMembership:  true,
		ReplicaConfigs: replicaCfgs,
	}

	err = sop.CreateService(ctx, req, common.DefaultTaskWaitSeconds)
	if err != nil {
		glog.Fatalln("create service error", err, "cluster", *cluster, "service", *service)
	}

	// TODO if not waiting enough time, InitializeService will get error. Not sure why. wait DNS update?
	fmt.Println("wait 20 seconds for service to stabilize")
	time.Sleep(time.Duration(20) * time.Second)

	fmt.Println("Start initializing the MongoDB ReplicaSet")

	// generate the parameters for the init task
	envkvs := genInitTaskEnvKVPairs(awsRegion, *cluster, *service, replSetName, *replicas, mgtserverurl)

	initReq := &manage.RunTaskRequest{
		Service:        req.Service,
		Resource:       req.Resource,
		ContainerImage: InitContainerImage,
		TaskType:       common.TaskTypeInit,
		Envkvs:         envkvs,
	}

	// run the init task to initialize the service
	err = sop.InitializeService(ctx, initReq)
	if err != nil {
		glog.Fatalln("initialize service error", err, "cluster", *cluster, "service", *service)
	}

	fmt.Println("The mongodb replicaset are successfully initialized")
}

func genReplicaConfigs(replicas int64, az string, replSetName string, port int64) []*manage.ReplicaConfig {
	// TODO distribute replicas to different zones
	replicaCfgs := make([]*manage.ReplicaConfig, replicas)
	for i := int64(0); i < replicas; i++ {
		netcontent := fmt.Sprintf(mongoDBConfNetwork, port)
		replcontent := fmt.Sprintf(mongoDBConfRepl, replSetName)
		content := mongoDBConfHead + mongoDBConfStorage + mongoDBConfLog + netcontent + replcontent + mongoDBConfEnd
		cfg := &manage.ReplicaConfigFile{FileName: mongoDBConfFileName, Content: content}
		configs := []*manage.ReplicaConfigFile{cfg}
		replicaCfg := &manage.ReplicaConfig{Zone: az, Configs: configs}
		replicaCfgs[i] = replicaCfg
	}
	return replicaCfgs
}

func genInitTaskEnvKVPairs(region string, cluster string, service string, replSetName string, replicas int64, mgtserverurl string) []*common.EnvKeyValuePair {
	kvregion := &common.EnvKeyValuePair{Name: common.ENV_REGION, Value: region}
	kvcluster := &common.EnvKeyValuePair{Name: common.ENV_CLUSTER, Value: cluster}
	kvservice := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_NAME, Value: service}
	kvreplSet := &common.EnvKeyValuePair{Name: EnvReplicaSetName, Value: replSetName}
	kvmgtserver := &common.EnvKeyValuePair{Name: common.ENV_MANAGE_SERVER_URL, Value: mgtserverurl}
	kvop := &common.EnvKeyValuePair{Name: common.ENV_OP, Value: manage.ServiceInitializedOp}

	domain := dns.GenDefaultDomainName(cluster)
	masterDNS := dns.GenDNSName(utils.GenServiceMemberName(service, 0), domain)
	kvmaster := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_MASTER, Value: masterDNS}

	envkvs := []*common.EnvKeyValuePair{kvregion, kvcluster, kvservice, kvreplSet, kvmgtserver, kvop, kvmaster}
	if replicas == 1 {
		return envkvs
	}

	slaves := dns.GenDNSName(utils.GenServiceMemberName(service, 1), domain)
	for i := int64(2); i < replicas; i++ {
		slaves = slaves + common.ENV_VALUE_SEPARATOR + dns.GenDNSName(utils.GenServiceMemberName(service, i), domain)
	}
	kvslaves := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_MEMBERS, Value: slaves}
	envkvs = append(envkvs, kvslaves)
	return envkvs
}

const (
	mongoDBConfFileName = "mongod.conf"

	mongoDBConfHead = `
# mongod.conf

# for documentation of all options, see:
#   http://docs.mongodb.org/manual/reference/configuration-options/
`

	mongoDBConfStorage = `
# Where and how to store data.
storage:
  dbPath: /data/db
  journal:
    enabled: true
  engine: wiredTiger
#  mmapv1:
  wiredTiger:
    engineConfig:
      cacheSizeGB: 1
`

	mongoDBConfLog = `
# where to write logging data.
systemLog:
  destination: file
  path: /var/log/mongodb/mongod.log
`

	mongoDBConfNetwork = `
# network interfaces, bind 0.0.0.0
net:
  port: %d
`

	mongoDBConfRepl = `
replication:
  replSetName: %s
`

	mongoDBConfEnd = `
#sharding:

#processManagement:

#security:

#operationProfiling:

## Enterprise-Only Options:

#auditLog:

#snmp:
`
)
