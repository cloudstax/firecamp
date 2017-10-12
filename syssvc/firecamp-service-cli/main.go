package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"golang.org/x/net/context"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/catalog/elasticsearch"
	"github.com/cloudstax/firecamp/catalog/redis"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/dns/awsroute53"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/manage/client"
	"github.com/cloudstax/firecamp/server/awsec2"
	"github.com/cloudstax/firecamp/utils"
)

// The catalog service command tool to create/query the catalog service.

var (
	op              = flag.String("op", "", "The operation type, such as create-service")
	serviceType     = flag.String("service-type", "", "The catalog service type: mongodb|postgresql|cassandra|zookeeper|kafka|redis|couchdb|consul|elasticsearch")
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
	admin       = flag.String("admin", "admin", "The DB admin. For PostgreSQL, use default user \"postgres\"")
	adminPasswd = flag.String("password", "changeme", "The DB admin password")
	tlsEnabled  = flag.Bool("tls-enabled", false, "whether tls is enabled")
	caFile      = flag.String("ca-file", "", "the ca file")
	certFile    = flag.String("cert-file", "", "the cert file")
	keyFile     = flag.String("key-file", "", "the key file")

	// The postgres service creation specific parameters.
	pgReplUser       = flag.String("pg-repluser", "repluser", "The PostgreSQL replication user that the standby DB replicates from the primary")
	pgReplUserPasswd = flag.String("pg-replpasswd", "replpassword", "The PostgreSQL password for the standby DB to access the primary")

	// The kafka service creation specific parameters
	kafkaAllowTopicDel  = flag.Bool("kafka-allow-topic-del", false, "The Kafka config to enable/disable topic deletion, default: false")
	kafkaRetentionHours = flag.Int64("kafka-retention-hours", 168, "The Kafka log retention hours, default: 168 hours")
	kafkaZkService      = flag.String("kafka-zk-service", "", "The ZooKeeper service name that Kafka will talk to")

	// The redis service creation specific parameters.
	redisShards           = flag.Int64("redis-shards", 1, "The number of shards for the Redis service")
	redisReplicasPerShard = flag.Int64("redis-replicas-pershard", 1, "The number of replicas in one Redis shard")
	redisDisableAOF       = flag.Bool("redis-disable-aof", false, "Whether disable Redis append only file")
	redisAuthPass         = flag.String("redis-auth-pass", "", "The Redis AUTH password")
	redisReplTimeoutSecs  = flag.Int64("redis-repl-timeout", 60, "The Redis replication timeout value, unit: Seconds")
	redisMaxMemPolicy     = flag.String("redis-maxmem-policy", "noeviction", "The Redis eviction policy when the memory limit is reached")
	redisConfigCmdName    = flag.String("redis-configcmd-name", "", "The new name for Redis CONFIG command, empty name means disable the command")

	// The couchdb service creation specific parameters.
	couchdbEnableCors = flag.Bool("couchdb-enable-cors", false, "Whether enable CouchDB Cors")
	couchdbEnableCred = flag.Bool("couchdb-enable-cred", false, "Whether enable credential support for CouchDB Cors")
	couchdbOrigins    = flag.String("couchdb-origins", "*", "CouchDB Cors origins")
	couchdbHeaders    = flag.String("couchdb-headers", "", "CouchDB Cors accpeted headers")
	couchdbMethods    = flag.String("couchdb-methods", "GET", "CouchDB Cors accepted methods")
	couchdbEnableSSL  = flag.Bool("couchdb-enable-ssl", false, "Whether enable CouchDB SSL")
	couchdbCertFile   = flag.String("couchdb-cert-file", "", "The CouchDB cert file")
	couchdbKeyFile    = flag.String("couchdb-key-file", "", "The CouchDB key file")
	couchdbCACertFile = flag.String("couchdb-cacert-file", "", "The CouchDB cacert file")

	// The consul service creation specific parameters.
	consulDc         = flag.String("consul-datacenter", "", "Consul datacenter")
	consulDomain     = flag.String("consul-domain", "", "Consul domain")
	consulEncrypt    = flag.String("consul-encrypt", "", "Consul encrypt")
	consulEnableTLS  = flag.Bool("consul-enable-tls", false, "Whether enable Consul TLS")
	consulCertFile   = flag.String("consul-cert-file", "", "The Consul cert file")
	consulKeyFile    = flag.String("consul-key-file", "", "The Consul key file")
	consulCACertFile = flag.String("consul-cacert-file", "", "The Consul cacert file")
	consulHTTPSPort  = flag.Int64("consul-https-port", 8080, "The Consul HTTPS port")

	// The elasticsearch service creation specific parameters.
	esDisableDedicatedMaster = flag.Bool("es-disable-dedicated-master", false, "Whether disables the dedicated master for ElasticSearch")
	esDisableForceAware      = flag.Bool("es-disable-force-awareness", false, "Whether disables the force awareness for ElasticSearch")

	// the parameters for getting the config file
	serviceUUID = flag.String("service-uuid", "", "The service uuid for getting the service's config file")
	fileID      = flag.String("fileid", "", "The service config file id")

	// the parameter for cleanDNS operation
	vpcID = flag.String("vpcid", "", "The VPCID that the cluster runs on")
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
	// opCleanDNS will cleanup the left over manage server dns record and HostedZone in AWS Route53,
	// as CloudFormation stack deletion could not delete them from Route53.
	opCleanDNS = "clean-dns"
)

func usage() {
	flag.Usage = func() {
		switch *op {
		case opCreate:
			fmt.Printf("usage: firecamp-catalogservice-cli -op=%s -service-type=<mongodb|postgresql|cassandra|zookeeper|kafka|redis|couchdb|consul|elasticsearch> [OPTIONS]\n", opCreate)
			flag.PrintDefaults()
		case opCheckInit:
			fmt.Printf("usage: firecamp-catalogservice-cli -op=%s -region=us-west-1 -cluster=default -service-name=aaa -admin=admin -passwd=passwd\n", opCheckInit)
		case opDelete:
			fmt.Printf("usage: firecamp-catalogservice-cli -op=%s -region=us-west-1 -cluster=default -service-name=aaa\n", opDelete)
		case opList:
			fmt.Printf("usage: firecamp-catalogservice-cli -op=%s -region=us-west-1 -cluster=default\n", opList)
		case opGet:
			fmt.Printf("usage: firecamp-catalogservice-cli -op=%s -region=us-west-1 -cluster=default -service-name=aaa\n", opGet)
		case opListMembers:
			fmt.Printf("usage: firecamp-catalogservice-cli -op=%s -region=us-west-1 -cluster=default -service-name=aaa\n", opListMembers)
		case opGetConfig:
			fmt.Printf("usage: firecamp-catalogservice-cli -op=%s -region=us-west-1 -cluster=default -service-uuid=serviceuuid -fileid=configfileID\n", opGetConfig)
		case opCleanDNS:
			fmt.Printf("usage: firecamp-catalogservice-cli -op=%s -region=us-west-1 -cluster=default -vpcid=vpcid\n", opCleanDNS)
		default:
			fmt.Printf("usage: firecamp-catalogservice-cli -op=<%s|%s|%s|%s|%s|%s|%s|%s> --help\n",
				opCreate, opCheckInit, opDelete, opList, opGet, opListMembers, opGetConfig, opCleanDNS)
		}
	}
}

func main() {
	usage()
	flag.Parse()

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
		case catalog.CatalogService_Cassandra:
			createCassandraService(ctx, cli)
		case catalog.CatalogService_ZooKeeper:
			createZkService(ctx, cli)
		case catalog.CatalogService_Kafka:
			createKafkaService(ctx, cli)
		case catalog.CatalogService_Redis:
			createRedisService(ctx, cli)
		case catalog.CatalogService_CouchDB:
			createCouchDBService(ctx, cli)
		case catalog.CatalogService_Consul:
			createConsulService(ctx, cli)
		case catalog.CatalogService_ElasticSearch:
			createESService(ctx, cli)
		default:
			fmt.Printf("Invalid service type, please specify %s|%s|%s|%s|%s|%s|%s|%s|%s\n",
				catalog.CatalogService_MongoDB, catalog.CatalogService_PostgreSQL,
				catalog.CatalogService_Cassandra, catalog.CatalogService_ZooKeeper,
				catalog.CatalogService_Kafka, catalog.CatalogService_Redis,
				catalog.CatalogService_CouchDB, catalog.CatalogService_Consul,
				catalog.CatalogService_ElasticSearch)
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

	case opCleanDNS:
		cleanDNS(ctx, cli)

	default:
		fmt.Printf("Invalid operation, please specify %s|%s|%s|%s|%s|%s|%s|%s\n",
			opCreate, opCheckInit, opDelete, opList, opGet, opListMembers, opGetConfig, opCleanDNS)
		os.Exit(-1)
	}
}

func createMongoDBService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
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
	waitServiceInit(ctx, cli, initReq)
	waitServiceRunning(ctx, cli, req.Service)
}

func createCassandraService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *replicas == 0 || *volSizeGB == 0 {
		fmt.Println("please specify the valid replica number and volume size")
		os.Exit(-1)
	}

	req := &manage.CatalogCreateCassandraRequest{
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
	}

	err := cli.CatalogCreateCassandraService(ctx, req)
	if err != nil {
		fmt.Println("create cassandra service error", err)
		os.Exit(-1)
	}

	fmt.Println("The catalog service is created, wait till it gets initialized")

	initReq := &manage.CatalogCheckServiceInitRequest{
		ServiceType: catalog.CatalogService_Cassandra,
		Service:     req.Service,
	}

	waitServiceInit(ctx, cli, initReq)
	waitServiceRunning(ctx, cli, req.Service)
}

func waitServiceInit(ctx context.Context, cli *client.ManageClient, initReq *manage.CatalogCheckServiceInitRequest) {
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

func createZkService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *replicas == 0 || *volSizeGB == 0 {
		fmt.Println("please specify the valid replica number and volume size")
		os.Exit(-1)
	}

	req := &manage.CatalogCreateZooKeeperRequest{
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
	}

	err := cli.CatalogCreateZooKeeperService(ctx, req)
	if err != nil {
		fmt.Println("create zookeeper service error", err)
		os.Exit(-1)
	}

	fmt.Println("The zookeeper service is created, wait for all containers running")

	waitServiceRunning(ctx, cli, req.Service)
}

func createKafkaService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *replicas == 0 || *volSizeGB == 0 || *kafkaZkService == "" {
		fmt.Println("please specify the valid replica number, volume size and zookeeper service name")
		os.Exit(-1)
	}

	req := &manage.CatalogCreateKafkaRequest{
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

		AllowTopicDel:  *kafkaAllowTopicDel,
		RetentionHours: *kafkaRetentionHours,
		ZkServiceName:  *kafkaZkService,
	}

	err := cli.CatalogCreateKafkaService(ctx, req)
	if err != nil {
		fmt.Println("create kafka service error", err)
		os.Exit(-1)
	}

	fmt.Println("The kafka service is created, wait for all containers running")

	waitServiceRunning(ctx, cli, req.Service)
}

func createRedisService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *maxMemMB == common.DefaultMaxMemoryMB || *volSizeGB == 0 {
		fmt.Println("please specify the valid max memory and volume size")
		os.Exit(-1)
	}

	req := &manage.CatalogCreateRedisRequest{
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
		Options: &manage.CatalogRedisOptions{
			Shards:           *redisShards,
			ReplicasPerShard: *redisReplicasPerShard,
			VolumeSizeGB:     *volSizeGB,

			DisableAOF:      *redisDisableAOF,
			AuthPass:        *redisAuthPass,
			ReplTimeoutSecs: *redisReplTimeoutSecs,
			MaxMemPolicy:    *redisMaxMemPolicy,
			ConfigCmdName:   *redisConfigCmdName,
		},
	}

	err := cli.CatalogCreateRedisService(ctx, req)
	if err != nil {
		fmt.Println("create redis service error", err)
		os.Exit(-1)
	}

	if rediscatalog.IsClusterMode(*redisShards) {
		fmt.Println("The service is created, wait till it gets initialized")

		initReq := &manage.CatalogCheckServiceInitRequest{
			ServiceType:      catalog.CatalogService_Redis,
			Service:          req.Service,
			Shards:           *redisShards,
			ReplicasPerShard: *redisReplicasPerShard,
		}

		waitServiceInit(ctx, cli, initReq)
	}

	fmt.Println("The service is created, wait for all containers running")
	waitServiceRunning(ctx, cli, req.Service)
}

func createCouchDBService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *volSizeGB == 0 {
		fmt.Println("please specify the valid volume size")
		os.Exit(-1)
	}

	req := &manage.CatalogCreateCouchDBRequest{
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
		Options: &manage.CatalogCouchDBOptions{
			Replicas:     *replicas,
			VolumeSizeGB: *volSizeGB,
			Admin:        *admin,
			AdminPasswd:  *adminPasswd,
		},
	}

	if *couchdbEnableCors {
		req.Options.EnableCors = *couchdbEnableCors
		req.Options.Credentials = *couchdbEnableCred
		req.Options.Origins = *couchdbOrigins
		req.Options.Headers = *couchdbHeaders
		req.Options.Methods = *couchdbMethods
	}

	if *couchdbEnableSSL {
		req.Options.EnableSSL = true

		// load the content of the ssl files
		certBytes, err := ioutil.ReadFile(*couchdbCertFile)
		if err != nil {
			fmt.Println("read cert file error", err, *couchdbCertFile)
			os.Exit(-1)
		}
		req.Options.CertFileContent = string(certBytes)

		keyBytes, err := ioutil.ReadFile(*couchdbKeyFile)
		if err != nil {
			fmt.Println("read key file error", err, *couchdbKeyFile)
			os.Exit(-1)
		}
		req.Options.KeyFileContent = string(keyBytes)

		if len(*couchdbCACertFile) != 0 {
			caBytes, err := ioutil.ReadFile(*couchdbCACertFile)
			if err != nil {
				fmt.Println("read ca cert file error", err, *couchdbCACertFile)
				os.Exit(-1)
			}
			req.Options.CACertFileContent = string(caBytes)
		}
	}

	err := cli.CatalogCreateCouchDBService(ctx, req)
	if err != nil {
		fmt.Println("create couchdb service error", err)
		os.Exit(-1)
	}

	fmt.Println("The service is created, wait till it gets initialized")

	initReq := &manage.CatalogCheckServiceInitRequest{
		ServiceType: catalog.CatalogService_CouchDB,
		Service:     req.Service,
		Admin:       *admin,
		AdminPasswd: *adminPasswd,
	}

	waitServiceInit(ctx, cli, initReq)
	waitServiceRunning(ctx, cli, req.Service)
}

func createConsulService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *volSizeGB == 0 {
		fmt.Println("please specify the valid volume size")
		os.Exit(-1)
	}

	req := &manage.CatalogCreateConsulRequest{
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
		Options: &manage.CatalogConsulOptions{
			Replicas:     *replicas,
			VolumeSizeGB: *volSizeGB,
			Datacenter:   *consulDc,
			Domain:       *consulDomain,
			Encrypt:      *consulEncrypt,
		},
	}

	if *consulEnableTLS {
		req.Options.EnableTLS = true

		// load the content of the tls files
		certBytes, err := ioutil.ReadFile(*consulCertFile)
		if err != nil {
			fmt.Println("read cert file error", err, *consulCertFile)
			os.Exit(-1)
		}
		req.Options.CertFileContent = string(certBytes)

		keyBytes, err := ioutil.ReadFile(*consulKeyFile)
		if err != nil {
			fmt.Println("read key file error", err, *consulKeyFile)
			os.Exit(-1)
		}
		req.Options.KeyFileContent = string(keyBytes)

		caBytes, err := ioutil.ReadFile(*consulCACertFile)
		if err != nil {
			fmt.Println("read ca cert file error", err, *consulCACertFile)
			os.Exit(-1)
		}
		req.Options.CACertFileContent = string(caBytes)

		req.Options.HTTPSPort = *consulHTTPSPort
	}

	serverips, err := cli.CatalogCreateConsulService(ctx, req)
	if err != nil {
		fmt.Println("create consul service error", err)
		os.Exit(-1)
	}

	fmt.Println("The consul service created, Consul server ips", serverips)
	fmt.Println("Wait for all containers running")

	waitServiceRunning(ctx, cli, req.Service)
}

func createESService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *replicas == 0 || *volSizeGB == 0 {
		fmt.Println("please specify the valid replica number and volume size")
		os.Exit(-1)
	}
	if *reserveMemMB == common.DefaultReserveMemoryMB {
		*reserveMemMB = escatalog.DefaultESHeapMB
	}
	if *reserveMemMB <= escatalog.DefaultESHeapMB {
		fmt.Printf("The ElasticSearch heap size equals or less than %d. Please increase it for production system\n", escatalog.DefaultESHeapMB)
	}

	req := &manage.CatalogCreateElasticSearchRequest{
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
		Options: &manage.CatalogElasticSearchOptions{
			Replicas:               *replicas,
			VolumeSizeGB:           *volSizeGB,
			DisableDedicatedMaster: *esDisableDedicatedMaster,
			DisableForceAwareness:  *esDisableForceAware,
		},
	}

	err := cli.CatalogCreateElasticSearchService(ctx, req)
	if err != nil {
		fmt.Println("create service error", err)
		os.Exit(-1)
	}

	fmt.Println("The service is created, wait for all containers running")

	waitServiceRunning(ctx, cli, req.Service)
}

func createPostgreSQLService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
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
		ReplUser:       *pgReplUser,
		ReplUserPasswd: *pgReplUserPasswd,
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
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
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
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
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
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
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
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
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

	fmt.Printf("%+v\n", *cfg)
}

func cleanDNS(ctx context.Context, cli *client.ManageClient) {
	if *vpcID == "" {
		fmt.Println("please specify the valid cluster VPCID")
		os.Exit(-1)
	}

	config := aws.NewConfig().WithRegion(*region)
	sess, err := session.NewSession(config)
	if err != nil {
		fmt.Println("failed to create session, error", err)
		os.Exit(-1)
	}
	dnsIns := awsroute53.NewAWSRoute53(sess)

	domain := dns.GenDefaultDomainName(*cluster)

	private := true
	hostedZoneID, err := dnsIns.GetOrCreateHostedZoneIDByName(ctx, domain, *vpcID, *region, private)
	if err != nil {
		fmt.Println("get aws route53 hostedZoneID error", err)
		os.Exit(-1)
	}

	dnsname := dns.GenDNSName(common.ManageServiceName, domain)
	hostname, err := dnsIns.GetDNSRecord(ctx, dnsname, hostedZoneID)
	if err != nil {
		if err == dns.ErrHostedZoneNotFound {
			fmt.Println("aws route53 hostedZone is already deleted")
			return
		}
		if err != dns.ErrDNSRecordNotFound {
			fmt.Println("delete the manage server dns record error", err)
			os.Exit(-1)
		}
		// err == dns.ErrDNSRecordNotFound, continue to delete hosted zone
	} else {
		// delete the dns record
		err = dnsIns.DeleteDNSRecord(ctx, dnsname, hostname, hostedZoneID)
		if err != nil {
			fmt.Println("delete the manage server dns record error", err)
			os.Exit(-1)
		}
	}

	// delete the route53 hosted zone
	err = dnsIns.DeleteHostedZone(ctx, hostedZoneID)
	if err != nil && err != dns.ErrHostedZoneNotFound {
		fmt.Println("delete the aws route53 hostedZone error", err)
		os.Exit(-1)
	}

	fmt.Println("deleted the manage server dns record and aws route53 hostedZone")
}
