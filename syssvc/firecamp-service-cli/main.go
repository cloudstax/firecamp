package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/catalog/cassandra"
	"github.com/cloudstax/firecamp/catalog/consul"
	"github.com/cloudstax/firecamp/catalog/elasticsearch"
	"github.com/cloudstax/firecamp/catalog/kafka"
	"github.com/cloudstax/firecamp/catalog/kafkamanager"
	"github.com/cloudstax/firecamp/catalog/kibana"
	"github.com/cloudstax/firecamp/catalog/logstash"
	"github.com/cloudstax/firecamp/catalog/mongodb"
	"github.com/cloudstax/firecamp/catalog/postgres"
	"github.com/cloudstax/firecamp/catalog/redis"
	"github.com/cloudstax/firecamp/catalog/zookeeper"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/manage/client"
	"github.com/cloudstax/firecamp/server/awsec2"
	"github.com/cloudstax/firecamp/utils"
)

// The catalog service command tool to create/query the catalog service.

const (
	// max wait 300s for service initialization. The pods of K8s statefulset may take long time to become ready.
	// The pod init may fail a few times with "FailedMount". But after some time, volume could be finally mounted.
	// Not sure why yet. There are many similar issues reported, but no one seems to have the answer.
	// TODO further investigate the reason.
	maxServiceInitWaitSeconds = time.Duration(300) * time.Second
)

var (
	op                  = flag.String("op", "", fmt.Sprintf("The operation type, %s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s", opCreate, opCheckInit, opGet, opUpdate, opDelete, opList, opScale, opStop, opStart, opListMembers, opGetConfig))
	serviceType         = flag.String("service-type", "", "The catalog service type: mongodb|postgresql|cassandra|zookeeper|kafka|kafkamanager|redis|couchdb|consul|elasticsearch|kibana|logstash")
	cluster             = flag.String("cluster", "mycluster", "The cluster name. Can only contain letters, numbers, or hyphens")
	serverURL           = flag.String("server-url", "", "the management service url, default: "+dns.GetDefaultManageServiceURL("mycluster", false))
	region              = flag.String("region", "", "The AWS region")
	service             = flag.String("service-name", "", fmt.Sprintf("The service name. Can only contain letters, numbers, or hyphens. The max length is %d", common.MaxServiceNameLength))
	replicas            = flag.Int64("replicas", 3, "The number of replicas for the service")
	volType             = flag.String("volume-type", common.VolumeTypeGPSSD, "The EBS volume type: gp2|io1|st1")
	volIops             = flag.Int64("volume-iops", 100, "The EBS volume Iops when io1 type is chosen, otherwise ignored")
	volSizeGB           = flag.Int64("volume-size", 0, "The size of each EBS volume, unit: GB")
	volEncrypted        = flag.Bool("volume-encrypted", false, "whether to create encrypted volume")
	journalVolType      = flag.String("journal-volume-type", common.VolumeTypeGPSSD, "The service journal EBS volume type: gp2|io1|st1")
	journalVolIops      = flag.Int64("journal-volume-iops", 0, "The service journal EBS volume Iops when io1 type is chosen, otherwise ignored")
	journalVolSizeGB    = flag.Int64("journal-volume-size", 0, "The service journal EBS volume size, unit: GB")
	journalVolEncrypted = flag.Bool("journal-volume-encrypted", false, "whether to create encrypted journal volume")
	maxCPUUnits         = flag.Int64("max-cpuunits", common.DefaultMaxCPUUnits, "The max number of cpu units for the container")
	reserveCPUUnits     = flag.Int64("reserve-cpuunits", common.DefaultReserveCPUUnits, "The number of cpu units to reserve for the container")
	maxMemMB            = flag.Int64("max-memory", common.DefaultMaxMemoryMB, "The max memory for the container, unit: MB")
	reserveMemMB        = flag.Int64("reserve-memory", common.DefaultReserveMemoryMB, "The memory reserved for the container, unit: MB")

	// security parameters
	admin       = flag.String("admin", "admin", "The DB admin. For PostgreSQL, use default user \"postgres\"")
	adminPasswd = flag.String("password", "changeme", "The DB admin password")
	tlsEnabled  = flag.Bool("tls-enabled", false, "whether tls is enabled")
	caFile      = flag.String("ca-file", "", "the ca file")
	certFile    = flag.String("cert-file", "", "the cert file")
	keyFile     = flag.String("key-file", "", "the key file")

	// The MongoDB service specific parameters.
	mongoShards        = flag.Int64("mongo-shards", 1, "The number of MongoDB shards")
	mongoReplPerShard  = flag.Int64("mongo-replicas-pershard", 3, "The number of replicas in one MongoDB shard")
	mongoReplSetOnly   = flag.Bool("mongo-replicaset-only", true, "Whether create a MongoDB ReplicaSet only. To create a sharded cluster, set to false")
	mongoConfigServers = flag.Int64("mongo-configservers", 3, "The number of config servers in the sharded cluster")

	// The Cassandra service specific parameters
	casHeapSizeMB = flag.Int64("cas-heap-size", cascatalog.DefaultHeapMB, "The Cassandra JVM heap size, unit: MB")
	casJmxUser    = flag.String("cas-jmx-user", cascatalog.DefaultJmxRemoteUser, "The Cassandra JMX remote user")
	casJmxPasswd  = flag.String("cas-jmx-passwd", "", "The Cassandra JMX. If leave as empty, an uuid will be generated automatically")

	// The postgres service creation specific parameters.
	pgReplUser       = flag.String("pg-repluser", "repluser", "The PostgreSQL replication user that the standby DB replicates from the primary")
	pgReplUserPasswd = flag.String("pg-replpasswd", "replpassword", "The PostgreSQL password for the standby DB to access the primary")
	pgContainerImage = flag.String("pg-image", pgcatalog.ContainerImage, "The PostgreSQL container image, "+pgcatalog.ContainerImage+" or "+pgcatalog.PostGISContainerImage)

	// The ZooKeeper service specific parameters
	zkHeapSizeMB = flag.Int64("zk-heap-size", zkcatalog.DefaultHeapMB, "The ZooKeeper JVM heap size, unit: MB")

	// The kafka service creation specific parameters
	kafkaHeapSizeMB     = flag.Int64("kafka-heap-size", kafkacatalog.DefaultHeapMB, "The Kafka JVM heap size, unit: MB")
	kafkaAllowTopicDel  = flag.Bool("kafka-allow-topic-del", false, "The Kafka config to enable/disable topic deletion")
	kafkaRetentionHours = flag.Int64("kafka-retention-hours", 168, "The Kafka log retention hours")
	kafkaZkService      = flag.String("kafka-zk-service", "", "The ZooKeeper service name that Kafka will talk to")

	// The kafka manager service creation specific parameters
	kmHeapSizeMB = flag.Int64("km-heap-size", kafkamanagercatalog.DefaultHeapMB, "The Kafka Manager JVM heap size, unit: MB")
	kmUser       = flag.String("km-user", "", "The Kafka Manager user name")
	kmPasswd     = flag.String("km-passwd", "", "The Kafka Manager user password")
	kmZkService  = flag.String("km-zk-service", "", "The ZooKeeper service name that Kafka Manager will talk to")

	// The redis service creation specific parameters.
	redisShards           = flag.Int64("redis-shards", 1, "The number of shards for the Redis service")
	redisReplicasPerShard = flag.Int64("redis-replicas-pershard", 3, "The number of replicas in one Redis shard")
	redisMemSizeMB        = flag.Int64("redis-memory-size", 0, "The Redis memory cache size, unit: MB")
	redisDisableAOF       = flag.Bool("redis-disable-aof", false, "Whether disable Redis append only file")
	redisAuthPass         = flag.String("redis-auth-pass", "", "The Redis AUTH password")
	redisReplTimeoutSecs  = flag.Int64("redis-repl-timeout", rediscatalog.MinReplTimeoutSecs, "The Redis replication timeout value, unit: Seconds")
	redisMaxMemPolicy     = flag.String("redis-maxmem-policy", rediscatalog.MaxMemPolicyAllKeysLRU, "The Redis eviction policy when the memory limit is reached")
	redisConfigCmdName    = flag.String("redis-configcmd-name", "", "The new name for Redis CONFIG command, empty name means disable the command")
	redisDisableConfigCmd = flag.Bool("redis-disable-configcmd", false, "Disable Redis CONFIG command in the update request")

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
	esHeapSizeMB             = flag.Int64("es-heap-size", escatalog.DefaultHeapMB, "The ElasticSearch JVM heap size, unit: MB")
	esDedicatedMasters       = flag.Int64("es-dedicated-masters", 3, "The number of dedicated masters for ElasticSearch")
	esDisableDedicatedMaster = flag.Bool("es-disable-dedicated-master", false, "Whether disables the dedicated master for ElasticSearch")
	esDisableForceAware      = flag.Bool("es-disable-force-awareness", false, "Whether disables the force awareness for ElasticSearch")

	// The kibana service creation specific parameters.
	kbESServiceName = flag.String("kb-es-service", "", "The ElasticSearch service the Kibana service talks to")
	kbProxyBasePath = flag.String("kb-proxy-basepath", "", "The proxy base path for the Kibana service")
	kbEnableSSL     = flag.Bool("kb-enable-ssl", false, "Whether enables SSL for the Kibana service")
	kbKeyFile       = flag.String("kb-key-file", "", "The Kibana service key file")
	kbCertFile      = flag.String("kb-cert-file", "", "The Kibana service certificate file")

	// The logstash service creation specific parameters.
	lsHeapSizeMB            = flag.Int64("ls-heap-size", logstashcatalog.DefaultHeapMB, "The Logstash JVM heap size, unit: MB")
	lsContainerImage        = flag.String("ls-container-image", logstashcatalog.ContainerImage, "The Logstash container image: "+logstashcatalog.ContainerImage+" or "+logstashcatalog.InputCouchDBContainerImage)
	lsQueueType             = flag.String("ls-queue-type", logstashcatalog.QueueTypeMemory, "The Logstash service queue type: memory or persisted")
	lsEnableDLQ             = flag.Bool("ls-enable-dlq", true, "Whether enables the Dead Letter Queue for Logstash")
	lsPipelineFile          = flag.String("ls-pipeline-file", "", "The Logstash pipeline config file")
	lsPipelineWorkers       = flag.Int("ls-workers", 0, "The number of worker threads for Logstash filter and output processing, 0 means using the default logstash workers")
	lsPipelineOutputWorkers = flag.Int("ls-outputworkers", 0, "The number of worker threads for each output, 0 means using the default logstash value")
	lsPipelineBatchSize     = flag.Int("ls-batch-size", 0, "How many events to retrieve from inputs before sending to filters+workers, 0 means using the default logstash batch size")
	lsPipelineBatchDelay    = flag.Int("ls-batch-delay", 0, "How long to wait before dispatching an undersized batch to filters+workers, Unit: milliseconds, 0 means using the default logstash batch delay")

	// the parameters for getting the config file
	serviceUUID = flag.String("service-uuid", "", "The service uuid for getting the service's config file")
	fileID      = flag.String("fileid", "", "The service config file id")
)

const (
	defaultPGAdmin = "postgres"

	opCreate    = "create-service"
	opCheckInit = "check-service-init"
	opDelete    = "delete-service"
	// update the service configs
	opUpdate = "update-service"
	// stop all service containers
	opStop = "stop-service"
	// start all service containers
	opStart = "start-service"
	// scale the service. scale service up will create new volumes for the new service containers.
	// TODO scale service down will simply mark the members as inactive and stop the corresponding containers.
	opScale       = "scale-service"
	opList        = "list-services"
	opGet         = "get-service"
	opListMembers = "list-members"
	opGetConfig   = "get-config"
)

func printFlag(f *flag.Flag) {
	s := fmt.Sprintf("  -%s", f.Name) // Two spaces before -; see next two comments.
	name, usage := flag.UnquoteUsage(f)
	if len(name) > 0 {
		s += " " + name
	}
	// Boolean flags of one ASCII letter are so common we
	// treat them specially, putting their usage on the same line.
	if len(s) <= 4 { // space, space, '-', 'x'.
		s += "\t"
	} else {
		// Four spaces before the tab triggers good alignment
		// for both 4- and 8-space tab stops.
		s += "\n    \t"
	}
	s += usage
	if !isZeroValue(f, f.DefValue) {
		s += fmt.Sprintf(". default: %v", f.DefValue)
	}
	fmt.Println(s)
}

// isZeroValue guesses whether the string represents the zero
// value for a flag. It is not accurate but in practice works OK.
func isZeroValue(f *flag.Flag, value string) bool {
	// for the "false" bool value, return false
	if value == "false" {
		return false
	}

	// Build a zero value of the flag's Value type, and see if the
	// result of calling its String method equals the value passed in.
	// This works unless the Value type is itself an interface type.
	typ := reflect.TypeOf(f.Value)
	var z reflect.Value
	if typ.Kind() == reflect.Ptr {
		z = reflect.New(typ.Elem())
	} else {
		z = reflect.Zero(typ)
	}
	if value == z.Interface().(flag.Value).String() {
		return true
	}

	switch value {
	case "false":
		return true
	case "":
		return true
	case "0":
		return true
	}
	return false
}

func usage() {
	flag.Usage = func() {
		switch *op {
		case opCreate:
			fmt.Printf("Usage: firecamp-service-cli -op=%s\n", opCreate)
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
			printFlag(flag.Lookup("service-type"))
			printFlag(flag.Lookup("service-name"))
			if *serviceType == common.CatalogService_KafkaManager {
				printFlag(flag.Lookup("km-heap-size"))
				printFlag(flag.Lookup("km-user"))
				printFlag(flag.Lookup("km-passwd"))
				printFlag(flag.Lookup("km-zk-service"))
				return
			}
			printFlag(flag.Lookup("volume-type"))
			printFlag(flag.Lookup("volume-size"))
			printFlag(flag.Lookup("volume-iops"))
			printFlag(flag.Lookup("volume-encrypted"))
			switch *serviceType {
			case common.CatalogService_MongoDB:
				printFlag(flag.Lookup("journal-volume-type"))
				printFlag(flag.Lookup("journal-volume-size"))
				printFlag(flag.Lookup("journal-volume-iops"))
				printFlag(flag.Lookup("journal-volume-encrypted"))
				printFlag(flag.Lookup("mongo-shards"))
				printFlag(flag.Lookup("mongo-replicas-pershard"))
				printFlag(flag.Lookup("mongo-replicaset-only"))
				printFlag(flag.Lookup("mongo-configservers"))
				printFlag(flag.Lookup("admin"))
				printFlag(flag.Lookup("password"))
			case common.CatalogService_PostgreSQL:
				printFlag(flag.Lookup("journal-volume-type"))
				printFlag(flag.Lookup("journal-volume-size"))
				printFlag(flag.Lookup("journal-volume-iops"))
				printFlag(flag.Lookup("journal-volume-encrypted"))
				printFlag(flag.Lookup("replicas"))
				printFlag(flag.Lookup("password"))
				printFlag(flag.Lookup("pg-image"))
				printFlag(flag.Lookup("pg-repluser"))
				printFlag(flag.Lookup("pg-replpasswd"))
			case common.CatalogService_Cassandra:
				printFlag(flag.Lookup("journal-volume-type"))
				printFlag(flag.Lookup("journal-volume-size"))
				printFlag(flag.Lookup("journal-volume-iops"))
				printFlag(flag.Lookup("journal-volume-encrypted"))
				printFlag(flag.Lookup("replicas"))
				printFlag(flag.Lookup("cas-heap-size"))
				printFlag(flag.Lookup("cas-jmx-user"))
				printFlag(flag.Lookup("cas-jmx-passwd"))
			case common.CatalogService_Redis:
				printFlag(flag.Lookup("redis-memory-size"))
				printFlag(flag.Lookup("redis-shards"))
				printFlag(flag.Lookup("redis-replicas-pershard"))
				printFlag(flag.Lookup("redis-disable-aof"))
				printFlag(flag.Lookup("redis-auth-pass"))
				printFlag(flag.Lookup("redis-maxmem-policy"))
				printFlag(flag.Lookup("redis-configcmd-name"))
				printFlag(flag.Lookup("redis-repl-timeout"))
			case common.CatalogService_ZooKeeper:
				printFlag(flag.Lookup("replicas"))
				printFlag(flag.Lookup("zk-heap-size"))
			case common.CatalogService_Kafka:
				printFlag(flag.Lookup("replicas"))
				printFlag(flag.Lookup("kafka-heap-size"))
				printFlag(flag.Lookup("kafka-allow-topic-del"))
				printFlag(flag.Lookup("kafka-retention-hours"))
				printFlag(flag.Lookup("kafka-zk-service"))
			case common.CatalogService_ElasticSearch:
				printFlag(flag.Lookup("replicas"))
				printFlag(flag.Lookup("es-heap-size"))
				printFlag(flag.Lookup("es-dedicated-masters"))
				printFlag(flag.Lookup("es-disable-dedicated-master"))
				printFlag(flag.Lookup("es-disable-force-awareness"))
			case common.CatalogService_Kibana:
				printFlag(flag.Lookup("replicas"))
				printFlag(flag.Lookup("kb-es-service"))
				printFlag(flag.Lookup("kb-proxy-basepath"))
				printFlag(flag.Lookup("kb-enable-ssl"))
				printFlag(flag.Lookup("kb-key-file"))
				printFlag(flag.Lookup("kb-cert-file"))
			case common.CatalogService_Logstash:
				printFlag(flag.Lookup("replicas"))
				printFlag(flag.Lookup("ls-heap-size"))
				printFlag(flag.Lookup("ls-container-image"))
				printFlag(flag.Lookup("ls-queue-type"))
				printFlag(flag.Lookup("ls-enable-dlq"))
				printFlag(flag.Lookup("ls-pipeline-file"))
				printFlag(flag.Lookup("ls-workers"))
				printFlag(flag.Lookup("ls-outputworkers"))
				printFlag(flag.Lookup("ls-batch-size"))
				printFlag(flag.Lookup("ls-batch-delay"))
			case common.CatalogService_Consul:
				printFlag(flag.Lookup("replicas"))
				printFlag(flag.Lookup("consul-datacenter"))
				printFlag(flag.Lookup("consul-domain"))
				printFlag(flag.Lookup("consul-encrypt"))
				printFlag(flag.Lookup("consul-enable-tls"))
				printFlag(flag.Lookup("consul-cert-file"))
				printFlag(flag.Lookup("consul-key-file"))
				printFlag(flag.Lookup("consul-cacert-file"))
				printFlag(flag.Lookup("consul-https-port"))
			case common.CatalogService_CouchDB:
				printFlag(flag.Lookup("replicas"))
				printFlag(flag.Lookup("couchdb-enable-cors"))
				printFlag(flag.Lookup("couchdb-enable-cred"))
				printFlag(flag.Lookup("couchdb-origins"))
				printFlag(flag.Lookup("couchdb-headers"))
				printFlag(flag.Lookup("couchdb-methods"))
				printFlag(flag.Lookup("couchdb-enable-ssl"))
				printFlag(flag.Lookup("couchdb-cert-file"))
				printFlag(flag.Lookup("couchdb-key-file"))
				printFlag(flag.Lookup("couchdb-cacert-file"))
			}

		case opUpdate:
			fmt.Printf("Usage: firecamp-service-cli -op=%s\n", opUpdate)
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
			fmt.Println("  -service-type=cassandra|redis")
			printFlag(flag.Lookup("service-name"))
			switch *serviceType {
			case common.CatalogService_Cassandra:
				printFlag(flag.Lookup("cas-heap-size"))
				printFlag(flag.Lookup("cas-jmx-user"))
				printFlag(flag.Lookup("cas-jmx-passwd"))
			case common.CatalogService_Redis:
				printFlag(flag.Lookup("redis-memory-size"))
				printFlag(flag.Lookup("redis-auth-pass"))
				printFlag(flag.Lookup("redis-repl-timeout"))
				printFlag(flag.Lookup("redis-maxmem-policy"))
				printFlag(flag.Lookup("redis-configcmd-name"))
				printFlag(flag.Lookup("redis-disable-configcmd"))
			}

		case opScale:
			fmt.Printf("Usage: firecamp-service-cli -op=%s\n", opScale)
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
			fmt.Println("  -service-type=cassandra")
			printFlag(flag.Lookup("service-name"))
			switch *serviceType {
			case common.CatalogService_Cassandra:
				printFlag(flag.Lookup("replicas"))
			}

		case opCheckInit:
			fmt.Printf("Usage: firecamp-service-cli -op=%s\n", opCheckInit)
			fmt.Println("  for MongoDB and CouchDB, please set -admin and -password")
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
			printFlag(flag.Lookup("service-name"))

		case opStop:
			fmt.Printf("Usage: firecamp-service-cli -op=%s\n", opStop)
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
			printFlag(flag.Lookup("service-name"))
		case opStart:
			fmt.Printf("Usage: firecamp-service-cli -op=%s\n", opStart)
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
			printFlag(flag.Lookup("service-name"))
		case opDelete:
			fmt.Printf("Usage: firecamp-service-cli -op=%s\n", opDelete)
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
			printFlag(flag.Lookup("service-name"))
			printFlag(flag.Lookup("service-type"))
		case opGet:
			fmt.Printf("Usage: firecamp-service-cli -op=%s\n", opGet)
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
			printFlag(flag.Lookup("service-name"))
		case opListMembers:
			fmt.Printf("Usage: firecamp-service-cli -op=%s\n", opListMembers)
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
			printFlag(flag.Lookup("service-name"))
		case opGetConfig:
			fmt.Printf("Usage: firecamp-service-cli -op=%s\n", opGetConfig)
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
			printFlag(flag.Lookup("service-uuid"))
			printFlag(flag.Lookup("fileid"))
		case opList:
			fmt.Printf("Usage: firecamp-service-cli -op=%s\n", opList)
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
		default:
			fmt.Println("Usage: firecamp-service-cli")
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
			printFlag(flag.Lookup("op"))
		}
	}
}

func main() {
	usage()
	flag.Parse()

	if *op != opList && *op != opGetConfig {
		validName := regexp.MustCompile(common.ServiceNamePattern)
		if !validName.MatchString(*service) {
			fmt.Println("service name must start with a letter and can only contain letters, numbers, or hyphens.")
			os.Exit(-1)
		}
	}
	if len(*service) > common.MaxServiceNameLength {
		fmt.Println("service name max length is", common.MaxServiceNameLength)
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
		var journalVol *common.ServiceVolume
		if *journalVolSizeGB != 0 {
			journalVol = &common.ServiceVolume{
				VolumeType:   *journalVolType,
				VolumeSizeGB: *journalVolSizeGB,
				Iops:         *journalVolIops,
				Encrypted:    *journalVolEncrypted,
			}
		}
		switch *serviceType {
		case common.CatalogService_MongoDB:
			createMongoDBService(ctx, cli, journalVol)
		case common.CatalogService_PostgreSQL:
			createPostgreSQLService(ctx, cli, journalVol)
		case common.CatalogService_Cassandra:
			createCassandraService(ctx, cli, journalVol)
		case common.CatalogService_ZooKeeper:
			createZkService(ctx, cli)
		case common.CatalogService_Kafka:
			createKafkaService(ctx, cli)
		case common.CatalogService_KafkaManager:
			createKafkaManagerService(ctx, cli)
		case common.CatalogService_Redis:
			createRedisService(ctx, cli)
		case common.CatalogService_CouchDB:
			createCouchDBService(ctx, cli)
		case common.CatalogService_Consul:
			createConsulService(ctx, cli)
		case common.CatalogService_ElasticSearch:
			createESService(ctx, cli)
		case common.CatalogService_Kibana:
			createKibanaService(ctx, cli)
		case common.CatalogService_Logstash:
			createLogstashService(ctx, cli)
		default:
			fmt.Printf("Invalid service type, please specify %s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s\n",
				common.CatalogService_MongoDB, common.CatalogService_PostgreSQL,
				common.CatalogService_Cassandra, common.CatalogService_ZooKeeper,
				common.CatalogService_Kafka, common.CatalogService_KafkaManager,
				common.CatalogService_Redis, common.CatalogService_CouchDB,
				common.CatalogService_Consul, common.CatalogService_ElasticSearch,
				common.CatalogService_Kibana, common.CatalogService_Logstash)
			os.Exit(-1)
		}

	case opUpdate:
		switch *serviceType {
		case common.CatalogService_Cassandra:
			updateCassandraService(ctx, cli)
		case common.CatalogService_Redis:
			updateRedisService(ctx, cli)
		default:
			fmt.Printf("Invalid service type, update service only support cassandra|redis\n")
			os.Exit(-1)
		}

	case opScale:
		switch *serviceType {
		case common.CatalogService_Cassandra:
			scaleCassandraService(ctx, cli)
		default:
			fmt.Printf("Invalid service type, scale service only support cassandra\n")
			os.Exit(-1)
		}

	case opStop:
		stopService(ctx, cli)

	case opStart:
		startService(ctx, cli)

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
		fmt.Printf("Invalid operation, please specify %s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s\n",
			opCreate, opCheckInit, opUpdate, opScale, opStop, opStart, opDelete, opList, opGet, opListMembers, opGetConfig)
		os.Exit(-1)
	}
}

func createMongoDBService(ctx context.Context, cli *client.ManageClient, journalVol *common.ServiceVolume) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *replicas == 0 || *volSizeGB == 0 {
		fmt.Println("please specify the valid replica number and volume size")
		os.Exit(-1)
	}
	if journalVol == nil {
		fmt.Println("please specify the separate journal volume for MongoDB")
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
		Options: &manage.CatalogMongoDBOptions{
			Shards:           *mongoShards,
			ReplicasPerShard: *mongoReplPerShard,
			ReplicaSetOnly:   *mongoReplSetOnly,
			ConfigServers:    *mongoConfigServers,
			Volume: &common.ServiceVolume{
				VolumeType:   *volType,
				Iops:         *volIops,
				VolumeSizeGB: *volSizeGB,
				Encrypted:    *volEncrypted,
			},
			JournalVolume: journalVol,
			Admin:         *admin,
			AdminPasswd:   *adminPasswd,
		},
	}

	err := mongodbcatalog.ValidateRequest(req)
	if err != nil {
		fmt.Println("invalid parameters", err)
		os.Exit(-1)
	}

	keyfileContent, err := cli.CatalogCreateMongoDBService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "create catalog mongodb service error", err)
		os.Exit(-1)
	}

	if req.Options.Shards == 1 && req.Options.ReplicaSetOnly {
		fmt.Println(time.Now().UTC(), "The catalog service is created, wait till it gets initialized")
	} else {
		// mongodb sharded cluster
		fmt.Println(time.Now().UTC(), "The MongoDB Sharded cluster is created, please run the mongos with keyfile content", keyfileContent)
		fmt.Println(time.Now().UTC(), "Wait till the sharded cluster gets initialized")
	}

	initReq := &manage.CatalogCheckServiceInitRequest{
		ServiceType: common.CatalogService_MongoDB,
		Service:     req.Service,
		Admin:       *admin,
		AdminPasswd: *adminPasswd,
	}
	waitServiceInit(ctx, cli, initReq)
	waitServiceRunning(ctx, cli, req.Service)
}

func createCassandraService(ctx context.Context, cli *client.ManageClient, journalVol *common.ServiceVolume) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *replicas == 0 || *volSizeGB == 0 {
		fmt.Println("please specify the valid replica number and volume size")
		os.Exit(-1)
	}
	if journalVol == nil {
		fmt.Println("please specify the separate journal volume for Cassandra")
		os.Exit(-1)
	}
	if *casHeapSizeMB < cascatalog.DefaultHeapMB {
		fmt.Printf("the heap size is less than %d. Please increase it for production system\n", cascatalog.DefaultHeapMB)
	}
	if *casHeapSizeMB < cascatalog.MinHeapMB {
		fmt.Printf("the heap size is lessn than %d, Cassandra JVM may stall long time at GC\n", cascatalog.MinHeapMB)
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
		Options: &manage.CatalogCassandraOptions{
			Replicas: *replicas,
			Volume: &common.ServiceVolume{
				VolumeType:   *volType,
				Iops:         *volIops,
				VolumeSizeGB: *volSizeGB,
				Encrypted:    *volEncrypted,
			},
			JournalVolume:   journalVol,
			HeapSizeMB:      *casHeapSizeMB,
			JmxRemoteUser:   *casJmxUser,
			JmxRemotePasswd: *casJmxPasswd,
		},
	}

	err := cascatalog.ValidateRequest(req)
	if err != nil {
		fmt.Println("invalid parameters", err)
		os.Exit(-1)
	}

	jmxUser, jmxPasswd, err := cli.CatalogCreateCassandraService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "create cassandra service error", err)
		os.Exit(-1)
	}

	fmt.Println(time.Now().UTC(), "The catalog service is created, jmx user", jmxUser, "password", jmxPasswd)
	fmt.Println(time.Now().UTC(), "wait till the service gets initialized")

	initReq := &manage.CatalogCheckServiceInitRequest{
		ServiceType: common.CatalogService_Cassandra,
		Service:     req.Service,
	}

	if req.Options.Replicas > 1 {
		waitServiceInit(ctx, cli, initReq)
	}
	waitServiceRunning(ctx, cli, req.Service)
}

func updateCassandraService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *casHeapSizeMB < cascatalog.DefaultHeapMB {
		fmt.Printf("the heap size is less than %d. Please increase it for production system\n", cascatalog.DefaultHeapMB)
	}
	if *casHeapSizeMB < cascatalog.MinHeapMB {
		fmt.Printf("the heap size is lessn than %d, Cassandra JVM may stall long time at GC\n", cascatalog.MinHeapMB)
	}

	req := &manage.CatalogUpdateCassandraRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      *region,
			Cluster:     *cluster,
			ServiceName: *service,
		},
		HeapSizeMB:      *casHeapSizeMB,
		JmxRemoteUser:   *casJmxUser,
		JmxRemotePasswd: *casJmxPasswd,
	}

	err := cascatalog.ValidateUpdateRequest(req)
	if err != nil {
		fmt.Println("invalid parameters", err)
		os.Exit(-1)
	}

	err = cli.CatalogUpdateCassandraService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "update cassandra service error", err)
		os.Exit(-1)
	}

	fmt.Println("The catalog service is updated. Please stop and start the service to load the new configs")
}

func scaleCassandraService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}

	req := &manage.CatalogScaleCassandraRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      *region,
			Cluster:     *cluster,
			ServiceName: *service,
		},
		Replicas: *replicas,
	}

	err := cli.CatalogScaleCassandraService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "scale cassandra service error", err)
		os.Exit(-1)
	}

	fmt.Println("The cassandra service is scaled to", *replicas, "nodes, wait for the containers running")

	waitServiceRunning(ctx, cli, req.Service)
}

func waitServiceInit(ctx context.Context, cli *client.ManageClient, initReq *manage.CatalogCheckServiceInitRequest) {
	sleepSeconds := time.Duration(5) * time.Second
	for sec := time.Duration(0); sec < maxServiceInitWaitSeconds; sec += sleepSeconds {
		initialized, statusMsg, err := cli.CatalogCheckServiceInit(ctx, initReq)
		if err == nil {
			if initialized {
				fmt.Println(time.Now().UTC(), "The catalog service is initialized")
				return
			}
			fmt.Println(time.Now().UTC(), statusMsg)
		} else {
			fmt.Println(time.Now().UTC(), "check service init error", err)
		}
		time.Sleep(sleepSeconds)
	}

	fmt.Println(time.Now().UTC(), "The catalog service is not initialized after", common.DefaultServiceWaitSeconds)
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
	if *zkHeapSizeMB < zkcatalog.DefaultHeapMB {
		fmt.Printf("The ZooKeeper heap size is less than %d. Please increase it for production system\n", zkcatalog.DefaultHeapMB)
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
		Options: &manage.CatalogZooKeeperOptions{
			Replicas:   *replicas,
			HeapSizeMB: *zkHeapSizeMB,
			Volume: &common.ServiceVolume{
				VolumeType:   *volType,
				Iops:         *volIops,
				VolumeSizeGB: *volSizeGB,
				Encrypted:    *volEncrypted,
			},
		},
	}

	err := cli.CatalogCreateZooKeeperService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "create zookeeper service error", err)
		os.Exit(-1)
	}

	fmt.Println(time.Now().UTC(), "The zookeeper service is created, wait for all containers running")

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
	if *kafkaHeapSizeMB < kafkacatalog.DefaultHeapMB {
		fmt.Printf("The Kafka heap size is less than %d. Please increase it for production system\n", kafkacatalog.DefaultHeapMB)
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
		Options: &manage.CatalogKafkaOptions{
			Replicas: *replicas,
			Volume: &common.ServiceVolume{
				VolumeType:   *volType,
				Iops:         *volIops,
				VolumeSizeGB: *volSizeGB,
				Encrypted:    *volEncrypted,
			},

			HeapSizeMB:     *kafkaHeapSizeMB,
			AllowTopicDel:  *kafkaAllowTopicDel,
			RetentionHours: *kafkaRetentionHours,
			ZkServiceName:  *kafkaZkService,
		},
	}

	err := cli.CatalogCreateKafkaService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "create kafka service error", err)
		os.Exit(-1)
	}

	fmt.Println(time.Now().UTC(), "The kafka service is created, wait for all containers running")

	waitServiceRunning(ctx, cli, req.Service)
}

func createKafkaManagerService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" || *kmZkService == "" {
		fmt.Println("please specify the valid service name and zookeeper service name")
		os.Exit(-1)
	}
	if *kmHeapSizeMB < kafkamanagercatalog.DefaultHeapMB {
		fmt.Printf("The Kafka Manager heap size is less than %d. Please increase it for production system\n", kafkamanagercatalog.DefaultHeapMB)
	}

	req := &manage.CatalogCreateKafkaManagerRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      *region,
			Cluster:     *cluster,
			ServiceName: *service,
			ServiceType: common.ServiceTypeStateless,
		},
		Resource: &common.Resources{
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Options: &manage.CatalogKafkaManagerOptions{
			HeapSizeMB:    *kmHeapSizeMB,
			User:          *kmUser,
			Password:      *kmPasswd,
			ZkServiceName: *kmZkService,
		},
	}

	err := cli.CatalogCreateKafkaManagerService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "create kafka manager service error", err)
		os.Exit(-1)
	}

	fmt.Println(time.Now().UTC(), "The kafka manager service is created, wait for all containers running")

	waitServiceRunning(ctx, cli, req.Service)
}

func createRedisService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *redisMemSizeMB <= 0 || *volSizeGB <= 0 {
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
			Shards:            *redisShards,
			ReplicasPerShard:  *redisReplicasPerShard,
			MemoryCacheSizeMB: *redisMemSizeMB,

			Volume: &common.ServiceVolume{
				VolumeType:   *volType,
				Iops:         *volIops,
				VolumeSizeGB: *volSizeGB,
				Encrypted:    *volEncrypted,
			},

			DisableAOF:      *redisDisableAOF,
			AuthPass:        *redisAuthPass,
			ReplTimeoutSecs: *redisReplTimeoutSecs,
			MaxMemPolicy:    *redisMaxMemPolicy,
			ConfigCmdName:   *redisConfigCmdName,
		},
	}

	err := rediscatalog.ValidateRequest(req)
	if err != nil {
		fmt.Println("invalid request", err)
		os.Exit(-1)
	}

	err = cli.CatalogCreateRedisService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "create redis service error", err)
		os.Exit(-1)
	}

	if rediscatalog.IsClusterMode(*redisShards) {
		fmt.Println(time.Now().UTC(), "The service is created, wait till it gets initialized")

		initReq := &manage.CatalogCheckServiceInitRequest{
			ServiceType: common.CatalogService_Redis,
			Service:     req.Service,
		}

		waitServiceInit(ctx, cli, initReq)
	}

	fmt.Println(time.Now().UTC(), "The service is created, wait for all containers running")
	waitServiceRunning(ctx, cli, req.Service)
}

func updateRedisService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *redisMemSizeMB < 0 {
		fmt.Println("please specify the valid max memory")
	}

	req := &manage.CatalogUpdateRedisRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      *region,
			Cluster:     *cluster,
			ServiceName: *service,
		},
		MemoryCacheSizeMB: *redisMemSizeMB,
		AuthPass:          *redisAuthPass,
		ReplTimeoutSecs:   *redisReplTimeoutSecs,
		MaxMemPolicy:      *redisMaxMemPolicy,
		ConfigCmdName:     *redisConfigCmdName,
		DisableConfigCmd:  *redisDisableConfigCmd,
	}

	err := rediscatalog.ValidateUpdateRequest(req)
	if err != nil {
		fmt.Println("invalid parameters", err)
		os.Exit(-1)
	}

	err = cli.CatalogUpdateRedisService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "update redis service error", err)
		os.Exit(-1)
	}

	fmt.Println("The catalog service is updated. Please stop and start the service to load the new configs")
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
			Replicas:    *replicas,
			Admin:       *admin,
			AdminPasswd: *adminPasswd,

			Volume: &common.ServiceVolume{
				VolumeType:   *volType,
				Iops:         *volIops,
				VolumeSizeGB: *volSizeGB,
				Encrypted:    *volEncrypted,
			},
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
		fmt.Println(time.Now().UTC(), "create couchdb service error", err)
		os.Exit(-1)
	}

	fmt.Println(time.Now().UTC(), "The service is created, wait till it gets initialized")

	initReq := &manage.CatalogCheckServiceInitRequest{
		ServiceType: common.CatalogService_CouchDB,
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
			Replicas: *replicas,
			Volume: &common.ServiceVolume{
				VolumeType:   *volType,
				Iops:         *volIops,
				VolumeSizeGB: *volSizeGB,
				Encrypted:    *volEncrypted,
			},

			Datacenter: *consulDc,
			Domain:     *consulDomain,
			Encrypt:    *consulEncrypt,
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

	err := consulcatalog.ValidateRequest(req)
	if err != nil {
		fmt.Println("invalid parameters", err)
		os.Exit(-1)
	}

	serverips, err := cli.CatalogCreateConsulService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "create consul service error", err)
		os.Exit(-1)
	}

	fmt.Println(time.Now().UTC(), "The consul service created, Consul server ips", serverips)
	fmt.Println(time.Now().UTC(), "Wait for all containers running")

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
	if *esHeapSizeMB <= escatalog.DefaultHeapMB {
		fmt.Printf("The ElasticSearch heap size equals to or less than %d. Please increase it for production system\n", escatalog.DefaultHeapMB)
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
			Replicas: *replicas,
			Volume: &common.ServiceVolume{
				VolumeType:   *volType,
				Iops:         *volIops,
				VolumeSizeGB: *volSizeGB,
				Encrypted:    *volEncrypted,
			},

			HeapSizeMB:             *esHeapSizeMB,
			DedicatedMasters:       *esDedicatedMasters,
			DisableDedicatedMaster: *esDisableDedicatedMaster,
			DisableForceAwareness:  *esDisableForceAware,
		},
	}

	err := escatalog.ValidateRequest(req)
	if err != nil {
		fmt.Println("invalid parameters", err)
		os.Exit(-1)
	}

	err = cli.CatalogCreateElasticSearchService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "create service error", err)
		os.Exit(-1)
	}

	fmt.Println(time.Now().UTC(), "The service is created, wait for all containers running")

	waitServiceRunning(ctx, cli, req.Service)
}

func createKibanaService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *volSizeGB == 0 {
		fmt.Println("please specify the valid volume size")
		os.Exit(-1)
	}
	if *kbESServiceName == "" {
		fmt.Println("please specify the valid elasticsearch service name")
		os.Exit(-1)
	}
	if *reserveMemMB == common.DefaultReserveMemoryMB {
		*reserveMemMB = kibanacatalog.DefaultReserveMemoryMB
	}
	if *reserveMemMB < kibanacatalog.DefaultReserveMemoryMB {
		fmt.Printf("The reserved memory for Kibana service is less than %d. Please increase it for production system\n", kibanacatalog.DefaultReserveMemoryMB)
	}

	req := &manage.CatalogCreateKibanaRequest{
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
		Options: &manage.CatalogKibanaOptions{
			Replicas: *replicas,
			Volume: &common.ServiceVolume{
				VolumeType:   *volType,
				Iops:         *volIops,
				VolumeSizeGB: *volSizeGB,
				Encrypted:    *volEncrypted,
			},
			ESServiceName: *kbESServiceName,
			ProxyBasePath: *kbProxyBasePath,
		},
	}

	if *kbEnableSSL {
		req.Options.EnableSSL = true

		// load the content of the ssl key and cert files
		keyBytes, err := ioutil.ReadFile(*kbKeyFile)
		if err != nil {
			fmt.Println("read key file error", err, *kbKeyFile)
			os.Exit(-1)
		}
		req.Options.SSLKey = string(keyBytes)

		certBytes, err := ioutil.ReadFile(*kbCertFile)
		if err != nil {
			fmt.Println("read cert file error", err, *kbCertFile)
			os.Exit(-1)
		}
		req.Options.SSLCert = string(certBytes)
	}

	err := kibanacatalog.ValidateRequest(req)
	if err != nil {
		fmt.Println("invalid parameters", err)
		os.Exit(-1)
	}

	err = cli.CatalogCreateKibanaService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "create kibana service error", err)
		os.Exit(-1)
	}

	fmt.Println(time.Now().UTC(), "The kibana service created, wait for all containers running")

	waitServiceRunning(ctx, cli, req.Service)
}

func createLogstashService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *volSizeGB == 0 {
		fmt.Println("please specify the valid volume size")
		os.Exit(-1)
	}
	if *lsPipelineFile == "" {
		fmt.Println("please specify the valid pipeline config file")
		os.Exit(-1)
	}
	if *lsHeapSizeMB < logstashcatalog.DefaultHeapMB {
		fmt.Printf("The reserved memory for Logstash service is less than %d. Please increase it for production system\n", logstashcatalog.DefaultHeapMB)
	}

	// load the content of the pipeline config file
	// TODO how to validate the config file. for now, rely on the user to ensure it.
	cfgBytes, err := ioutil.ReadFile(*lsPipelineFile)
	if err != nil {
		fmt.Println("read the pipeline config file error", err, *lsPipelineFile)
		os.Exit(-1)
	}

	req := &manage.CatalogCreateLogstashRequest{
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
		Options: &manage.CatalogLogstashOptions{
			Replicas: *replicas,
			Volume: &common.ServiceVolume{
				VolumeType:   *volType,
				Iops:         *volIops,
				VolumeSizeGB: *volSizeGB,
				Encrypted:    *volEncrypted,
			},
			HeapSizeMB:            *lsHeapSizeMB,
			ContainerImage:        *lsContainerImage,
			QueueType:             *lsQueueType,
			EnableDeadLetterQueue: *lsEnableDLQ,
			PipelineConfigs:       string(cfgBytes),
			PipelineWorkers:       *lsPipelineWorkers,
			PipelineOutputWorkers: *lsPipelineOutputWorkers,
			PipelineBatchSize:     *lsPipelineBatchSize,
			PipelineBatchDelay:    *lsPipelineBatchDelay,
		},
	}

	err = logstashcatalog.ValidateRequest(req)
	if err != nil {
		fmt.Println("invalid parameters", err)
		os.Exit(-1)
	}

	err = cli.CatalogCreateLogstashService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "create logstash service error", err)
		os.Exit(-1)
	}

	fmt.Println(time.Now().UTC(), "The logstash service created, wait for all containers running")

	waitServiceRunning(ctx, cli, req.Service)
}

func createPostgreSQLService(ctx context.Context, cli *client.ManageClient, journalVol *common.ServiceVolume) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}
	if *replicas == 0 || *volSizeGB == 0 {
		fmt.Println("please specify the valid replica number and volume size")
		os.Exit(-1)
	}
	if journalVol == nil {
		fmt.Println("please specify the separate journal volume for PostgreSQL")
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
		Options: &manage.CatalogPostgreSQLOptions{
			Replicas: *replicas,
			Volume: &common.ServiceVolume{
				VolumeType:   *volType,
				Iops:         *volIops,
				VolumeSizeGB: *volSizeGB,
				Encrypted:    *volEncrypted,
			},
			JournalVolume:  journalVol,
			ContainerImage: *pgContainerImage,
			AdminPasswd:    *adminPasswd,
			ReplUser:       *pgReplUser,
			ReplUserPasswd: *pgReplUserPasswd,
		},
	}

	err := pgcatalog.ValidateRequest(req)
	if err != nil {
		fmt.Println("invalid parameters", err)
		os.Exit(-1)
	}

	err = cli.CatalogCreatePostgreSQLService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "create postgresql service error", err)
		os.Exit(-1)
	}

	fmt.Println(time.Now().UTC(), "The postgresql service is created, wait for all containers running")

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
			fmt.Println(time.Now().UTC(), "GetServiceStatus error", err, r)
		} else {
			// ECS may return 0 desired count for the service right after the service is created.
			// If the desired count is 0, wait and retry.
			if status.RunningCount == status.DesiredCount && status.DesiredCount != 0 {
				fmt.Println(time.Now().UTC(), "All service containers are running, RunningCount", status.RunningCount)
				return
			}
			fmt.Println(time.Now().UTC(), "wait the service containers running, RunningCount", status.RunningCount)
		}

		time.Sleep(sleepSeconds)
	}

	fmt.Println(time.Now().UTC(), "not all service containers are running after", common.DefaultServiceWaitSeconds)
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
		fmt.Println(time.Now().UTC(), "check service init error", err)
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
		fmt.Println(time.Now().UTC(), "ListService error", err)
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
		fmt.Println(time.Now().UTC(), "GetServiceAttr error", err)
		os.Exit(-1)
	}

	fmt.Printf("%+v\n", *attr)

	if attr.UserAttr != nil {
		switch attr.UserAttr.ServiceType {
		case common.CatalogService_Cassandra:
			ua := &common.CasUserAttr{}
			err = json.Unmarshal(attr.UserAttr.AttrBytes, ua)
			if err != nil {
				fmt.Println("Unmarshal CasUserAttr error", err)
				os.Exit(-1)
			}
			fmt.Println("Cassandra jmx user", ua.JmxRemoteUser, "password", ua.JmxRemotePasswd)

		case common.CatalogService_MongoDB:
			ua := &common.MongoDBUserAttr{}
			err = json.Unmarshal(attr.UserAttr.AttrBytes, ua)
			if err != nil {
				fmt.Println("Unmarshal MongoDBUserAttr error", err)
				os.Exit(-1)
			}
			fmt.Printf("%+v\n", *ua)
		}
	}
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
		fmt.Println(time.Now().UTC(), "ListServiceMember error", err)
		os.Exit(-1)
	}

	return members
}

func stopService(ctx context.Context, cli *client.ManageClient) {
	// stop the service containers
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}

	serviceReq := &manage.ServiceCommonRequest{
		Region:      *region,
		Cluster:     *cluster,
		ServiceName: *service,
	}

	err := cli.StopService(ctx, serviceReq)
	if err != nil {
		fmt.Println(time.Now().UTC(), "StopService error", err)
		os.Exit(-1)
	}

	fmt.Println("Service stopped")
}

func startService(ctx context.Context, cli *client.ManageClient) {
	// start the service containers
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}

	serviceReq := &manage.ServiceCommonRequest{
		Region:      *region,
		Cluster:     *cluster,
		ServiceName: *service,
	}

	err := cli.StartService(ctx, serviceReq)
	if err != nil {
		fmt.Println(time.Now().UTC(), "StartService error", err)
		os.Exit(-1)
	}

	fmt.Println("Service started")

	waitServiceRunning(ctx, cli, serviceReq)
}

func deleteService(ctx context.Context, cli *client.ManageClient) {
	if *service == "" {
		fmt.Println("please specify the valid service name")
		os.Exit(-1)
	}

	// delete the service from the control plane.
	serviceReq := &manage.DeleteServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      *region,
			Cluster:     *cluster,
			ServiceName: *service,
		},
	}

	volIDs, err := cli.DeleteService(ctx, serviceReq)
	if err != nil {
		fmt.Println(time.Now().UTC(), "DeleteService error", err)
		os.Exit(-1)
	}

	if len(volIDs) != 0 {
		fmt.Println("Service deleted, please manually delete the EBS volumes\n\t", volIDs)
	} else {
		fmt.Println("service deleted")
	}
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
		fmt.Println(time.Now().UTC(), "GetConfigFile error", err)
		os.Exit(-1)
	}

	fmt.Printf("%+v\n", *cfg)
}
