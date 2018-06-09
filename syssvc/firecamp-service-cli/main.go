package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/catalog"
	catalogclient "github.com/cloudstax/firecamp/api/catalog/client"
	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/api/manage"
	manageclient "github.com/cloudstax/firecamp/api/manage/client"
	"github.com/cloudstax/firecamp/catalog/cassandra"
	"github.com/cloudstax/firecamp/catalog/consul"
	"github.com/cloudstax/firecamp/catalog/elasticsearch"
	"github.com/cloudstax/firecamp/catalog/kafka"
	"github.com/cloudstax/firecamp/catalog/kafkaconnect"
	"github.com/cloudstax/firecamp/catalog/kafkamanager"
	"github.com/cloudstax/firecamp/catalog/kibana"
	"github.com/cloudstax/firecamp/catalog/logstash"
	"github.com/cloudstax/firecamp/catalog/mongodb"
	"github.com/cloudstax/firecamp/catalog/postgres"
	"github.com/cloudstax/firecamp/catalog/redis"
	"github.com/cloudstax/firecamp/catalog/telegraf"
	"github.com/cloudstax/firecamp/catalog/zookeeper"
	"github.com/cloudstax/firecamp/pkg/dns"
	"github.com/cloudstax/firecamp/pkg/server/awsec2"
	"github.com/cloudstax/firecamp/pkg/utils"
)

// The catalog service command tool to create/query the catalog service.

const (
	// max wait 300s for service initialization. The pods of K8s statefulset may take long time to become ready.
	// The pod init may fail a few times with "FailedMount". But after some time, volume could be finally mounted.
	// Not sure why yet. There are many similar issues reported, but no one seems to have the answer.
	// TODO further investigate the reason.
	maxServiceWaitTime = time.Duration(300) * time.Second
	retryWaitTime      = time.Duration(common.CliRetryWaitSeconds) * time.Second

	flagMaxMem     = "max-memory"
	flagReserveMem = "reserve-memory"
	flagMaxCPU     = "max-cpuunits"
	flagReserveCPU = "reserve-cpuunits"

	flagJmxUser   = "jmx-user"
	flagJmxPasswd = "jmx-passwd"

	flagKafkaHeapSize       = "kafka-heap-size"
	flagKafkaRetentionHours = "kafka-retention-hours"
	flagKafkaAllowTopicDel  = "kafka-allow-topic-del"

	flagRedisMemSize     = "redis-memory-size"
	flagRedisAuthPass    = "redis-auth-pass"
	flagRedisReplTimeout = "redis-repl-timeout"
	flagRedisMaxMemPol   = "redis-maxmem-policy"
	flagRedisConfigCmd   = "redis-configcmd-name"

	flagCasHeapSize = "cas-heap-size"

	flagZkHeapSize = "zk-heap-size"
)

var (
	op                  = flag.String("op", "", fmt.Sprintf("The operation type, %s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s", opCreate, opCheckInit, opGet, opUpdate, opUpdateResource, opDelete, opList, opScale, opStop, opStart, opRollingRestart, opUpgrade, opListMembers, opGetConfig))
	serviceType         = flag.String("service-type", "", "The catalog service type: mongodb|postgresql|cassandra|zookeeper|kafka|kafkamanager|kafkasinkes|redis|couchdb|consul|elasticsearch|kibana|logstash|telegraf")
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
	maxCPUUnits         = flag.Int64(flagMaxCPU, common.DefaultMaxCPUUnits, "The max number of cpu units for the container")
	reserveCPUUnits     = flag.Int64(flagReserveCPU, common.DefaultReserveCPUUnits, "The number of cpu units to reserve for the container")
	maxMemMB            = flag.Int64(flagMaxMem, common.DefaultMaxMemoryMB, "The max memory for the container, unit: MB")
	reserveMemMB        = flag.Int64(flagReserveMem, common.DefaultReserveMemoryMB, "The memory reserved for the container, unit: MB")

	// security parameters
	admin       = flag.String("admin", "admin", "The DB admin. For PostgreSQL, use default user \"postgres\"")
	adminPasswd = flag.String("password", "changeme", "The DB admin password")
	tlsEnabled  = flag.Bool("tls-enabled", false, "whether tls is enabled")
	caFile      = flag.String("ca-file", "", "the ca file")
	certFile    = flag.String("cert-file", "", "the cert file")
	keyFile     = flag.String("key-file", "", "the key file")

	jmxUser   = flag.String(flagJmxUser, catalog.JmxDefaultRemoteUser, "The Service JMX remote user")
	jmxPasswd = flag.String(flagJmxPasswd, "", "The Service JMX password. If leave as empty, an uuid will be generated automatically")

	// The MongoDB service specific parameters.
	mongoShards        = flag.Int64("mongo-shards", 1, "The number of MongoDB shards")
	mongoReplPerShard  = flag.Int64("mongo-replicas-pershard", 3, "The number of replicas in one MongoDB shard")
	mongoReplSetOnly   = flag.Bool("mongo-replicaset-only", true, "Whether create a MongoDB ReplicaSet only. To create a sharded cluster, set to false")
	mongoConfigServers = flag.Int64("mongo-configservers", 3, "The number of config servers in the sharded cluster")

	// The Cassandra service specific parameters
	casHeapSizeMB = flag.Int64(flagCasHeapSize, cascatalog.DefaultHeapMB, "The Cassandra JVM heap size, unit: MB")

	// The postgres service creation specific parameters.
	pgReplUser       = flag.String("pg-repluser", "repluser", "The PostgreSQL replication user that the standby DB replicates from the primary")
	pgReplUserPasswd = flag.String("pg-replpasswd", "replpassword", "The PostgreSQL password for the standby DB to access the primary")
	pgContainerImage = flag.String("pg-image", pgcatalog.ContainerImage, "The PostgreSQL container image, "+pgcatalog.ContainerImage+" or "+pgcatalog.PostGISContainerImage)

	// The ZooKeeper service specific parameters
	zkHeapSizeMB = flag.Int64(flagZkHeapSize, zkcatalog.DefaultHeapMB, "The ZooKeeper JVM heap size, unit: MB")

	// The kafka service creation specific parameters
	kafkaHeapSizeMB     = flag.Int64(flagKafkaHeapSize, kafkacatalog.DefaultHeapMB, "The Kafka JVM heap size, unit: MB")
	kafkaAllowTopicDel  = flag.Bool(flagKafkaAllowTopicDel, false, "The Kafka config to enable/disable topic deletion")
	kafkaRetentionHours = flag.Int64(flagKafkaRetentionHours, kafkacatalog.DefaultRetentionHours, "The Kafka log retention hours")
	kafkaZkService      = flag.String("kafka-zk-service", "", "The ZooKeeper service name that Kafka will talk to")

	kcHeapSizeMB            = flag.Int64("kc-heap-size", kccatalog.DefaultHeapMB, "The Kafka Connect JVM heap size, unit: MB")
	kcKafkaService          = flag.String("kc-kafka-service", "", "The Kafka service name that the Connect talks to")
	kcKafkaTopic            = flag.String("kc-kafka-topic", "", "The Kafka topic that the Connect consumes data from")
	kcReplFactor            = flag.Uint("kc-replfactor", kccatalog.DEFAULT_REPLICATION_FACTOR, "The replication factor for the Connector's storage, offset and status topics")
	kcSinkESService         = flag.String("kc-sink-es-service", "", "The ElasticSearch service name that the Connect sinks data to")
	kcSinkESType            = flag.String("kc-sink-es-type", kccatalog.DEFAULT_TYPE_NAME, "The ElasticSearch index type name")
	kcSinkESBufferedRecords = flag.Int("kc-sink-es-buffer-records", kccatalog.DefaultMaxBufferedRecords, "The ElasticSearch max buffered records")
	kcSinkESBatchSize       = flag.Int("kc-sink-es-batch-size", kccatalog.DefaultBatchSize, "The ElasticSearch batch size")

	// The kafka manager service creation specific parameters
	kmHeapSizeMB = flag.Int64("km-heap-size", kmcatalog.DefaultHeapMB, "The Kafka Manager JVM heap size, unit: MB")
	kmUser       = flag.String("km-user", "", "The Kafka Manager user name")
	kmPasswd     = flag.String("km-passwd", "", "The Kafka Manager user password")
	kmZkService  = flag.String("km-zk-service", "", "The ZooKeeper service name that Kafka Manager will talk to")

	// The redis service creation specific parameters.
	redisShards           = flag.Int64("redis-shards", 1, "The number of shards for the Redis service")
	redisReplicasPerShard = flag.Int64("redis-replicas-pershard", 3, "The number of replicas in one Redis shard")
	redisMemSizeMB        = flag.Int64(flagRedisMemSize, 0, "The Redis memory cache size, unit: MB")
	redisDisableAOF       = flag.Bool("redis-disable-aof", false, "Whether disable Redis append only file")
	redisAuthPass         = flag.String(flagRedisAuthPass, "", "The Redis AUTH password")
	redisReplTimeoutSecs  = flag.Int64(flagRedisReplTimeout, rediscatalog.MinReplTimeoutSecs, "The Redis replication timeout value, unit: Seconds")
	redisMaxMemPolicy     = flag.String(flagRedisMaxMemPol, rediscatalog.MaxMemPolicyAllKeysLRU, "The Redis eviction policy when the memory limit is reached")
	redisConfigCmdName    = flag.String(flagRedisConfigCmd, "", "The new name for Redis CONFIG command, empty name means disable the command")

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

	// The telegraf service creation specific parameters
	telCollectIntervalSecs = flag.Int("tel-collect-interval", telcatalog.DefaultCollectIntervalSecs, "The service metrics collect interval, unit: seconds")
	telMonitorServiceName  = flag.String("tel-monitor-service-name", "", "The stateful service to monitor")
	telMonitorServiceType  = flag.String("tel-monitor-service-type", "", "The type of stateful service to minotor")
	telMetricsFile         = flag.String("tel-metrics-file", "", "This is an advanced config if you want to customize the metrics to collect. Please follow Telegraf's usage to specify your custom metrics. It is your responsibility to ensure the format and metrics are correct.")

	// the parameters for getting the config file
	memberName  = flag.String("member-name", "", "The service member name for getting the member's config file")
	cfgFileName = flag.String("config-filename", "", "The config file name")
)

const (
	defaultPGAdmin = "postgres"

	opCreate    = "create-service"
	opCheckInit = "check-service-init"
	opDelete    = "delete-service"
	// update the service configs
	opUpdate = "update-service"
	// update the service resource configs
	opUpdateResource = "update-service-resource"
	// stop all service containers
	opStop = "stop-service"
	// start all service containers
	opStart = "start-service"
	// rolling restart service containers
	opRollingRestart = "restart-service"
	// upgrade service
	opUpgrade = "upgrade-service"
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
			if *serviceType == "" {
				fmt.Println("\nPlease specify the service type and check the detail help for one service. For example, firecamp-service-cli -op=create-service -service-type=cassandra")
				return
			}
			printFlag(flag.Lookup("service-name"))
			printFlag(flag.Lookup(flagMaxCPU))
			printFlag(flag.Lookup(flagReserveCPU))
			printFlag(flag.Lookup(flagMaxMem))
			printFlag(flag.Lookup(flagReserveMem))
			// check stateless service first
			switch *serviceType {
			case common.CatalogService_KafkaManager:
				printFlag(flag.Lookup("km-heap-size"))
				printFlag(flag.Lookup("km-user"))
				printFlag(flag.Lookup("km-passwd"))
				printFlag(flag.Lookup("km-zk-service"))
				return
			case common.CatalogService_Telegraf:
				printFlag(flag.Lookup("tel-collect-interval"))
				printFlag(flag.Lookup("tel-monitor-service-name"))
				printFlag(flag.Lookup("tel-monitor-service-type"))
				printFlag(flag.Lookup("tel-metrics-file"))
				return
			case common.CatalogService_KafkaSinkES:
				printFlag(flag.Lookup("kc-heap-size"))
				printFlag(flag.Lookup("kc-kafka-service"))
				printFlag(flag.Lookup("kc-kafka-topic"))
				printFlag(flag.Lookup("kc-replfactor"))
				printFlag(flag.Lookup("kc-sink-es-service"))
				printFlag(flag.Lookup("kc-sink-es-type"))
				printFlag(flag.Lookup("kc-sink-es-buffer-records"))
				printFlag(flag.Lookup("kc-sink-es-batch-size"))
				return
			}
			// create stateful service
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
				printFlag(flag.Lookup(flagCasHeapSize))
				printFlag(flag.Lookup(flagJmxUser))
				printFlag(flag.Lookup(flagJmxPasswd))
			case common.CatalogService_Redis:
				printFlag(flag.Lookup("redis-shards"))
				printFlag(flag.Lookup("redis-replicas-pershard"))
				printFlag(flag.Lookup(flagRedisMemSize))
				printFlag(flag.Lookup("redis-disable-aof"))
				printFlag(flag.Lookup(flagRedisAuthPass))
				printFlag(flag.Lookup(flagRedisMaxMemPol))
				printFlag(flag.Lookup(flagRedisConfigCmd))
				printFlag(flag.Lookup(flagRedisReplTimeout))
			case common.CatalogService_ZooKeeper:
				printFlag(flag.Lookup("replicas"))
				printFlag(flag.Lookup(flagZkHeapSize))
				printFlag(flag.Lookup(flagJmxUser))
				printFlag(flag.Lookup(flagJmxPasswd))
			case common.CatalogService_Kafka:
				printFlag(flag.Lookup("replicas"))
				printFlag(flag.Lookup(flagKafkaHeapSize))
				printFlag(flag.Lookup(flagKafkaAllowTopicDel))
				printFlag(flag.Lookup(flagKafkaRetentionHours))
				printFlag(flag.Lookup(flagJmxUser))
				printFlag(flag.Lookup(flagJmxPasswd))
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

		case opUpdateResource:
			fmt.Printf("Usage: firecamp-service-cli -op=%s\n", opUpdateResource)
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
			printFlag(flag.Lookup("service-name"))
			printFlag(flag.Lookup(flagMaxMem))
			printFlag(flag.Lookup(flagReserveMem))
			printFlag(flag.Lookup(flagMaxCPU))
			printFlag(flag.Lookup(flagReserveCPU))

		case opUpdate:
			fmt.Printf("Usage: firecamp-service-cli -op=%s\n", opUpdate)
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
			printFlag(flag.Lookup("service-name"))
			fmt.Println("  -service-type=cassandra|redis|kafka|zookeeper")
			switch *serviceType {
			case common.CatalogService_Cassandra:
				printFlag(flag.Lookup(flagCasHeapSize))
				printFlag(flag.Lookup(flagJmxUser))
				printFlag(flag.Lookup(flagJmxPasswd))
			case common.CatalogService_Redis:
				printFlag(flag.Lookup(flagRedisMemSize))
				printFlag(flag.Lookup(flagRedisAuthPass))
				printFlag(flag.Lookup(flagRedisMaxMemPol))
				printFlag(flag.Lookup(flagRedisConfigCmd))
				printFlag(flag.Lookup(flagRedisReplTimeout))
			case common.CatalogService_Kafka:
				printFlag(flag.Lookup(flagKafkaHeapSize))
				printFlag(flag.Lookup(flagKafkaRetentionHours))
				printFlag(flag.Lookup(flagKafkaAllowTopicDel))
				printFlag(flag.Lookup(flagJmxUser))
				printFlag(flag.Lookup(flagJmxPasswd))
			case common.CatalogService_ZooKeeper:
				printFlag(flag.Lookup(flagZkHeapSize))
				printFlag(flag.Lookup(flagJmxUser))
				printFlag(flag.Lookup(flagJmxPasswd))
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

		case opUpgrade:
			fmt.Printf("Upgrade the service to the current release\n")
			fmt.Printf("Usage: firecamp-service-cli -op=%s\n", opUpgrade)
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
			printFlag(flag.Lookup("service-name"))

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
			fmt.Printf("%s stops all service containers in parallel", opStop)
		case opStart:
			fmt.Printf("Usage: firecamp-service-cli -op=%s\n", opStart)
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
			printFlag(flag.Lookup("service-name"))
			fmt.Printf("%s starts all service containers in parallel", opStart)
		case opRollingRestart:
			fmt.Printf("Usage: firecamp-service-cli -op=%s\n", opRollingRestart)
			printFlag(flag.Lookup("region"))
			printFlag(flag.Lookup("cluster"))
			printFlag(flag.Lookup("service-name"))
			fmt.Printf("%s rolling restart the service containers one by one", opRollingRestart)
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
			printFlag(flag.Lookup("service-name"))
			printFlag(flag.Lookup("member-name"))
			printFlag(flag.Lookup("config-filename"))
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

	if *op != opList && (*service == "" || *serviceType == "") {
		fmt.Println("please specify the valid service name and service type")
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

	cli := manageclient.NewManageClient(*serverURL, tlsConf)
	catalogcli := catalogclient.NewCatalogServiceClient(*serverURL, tlsConf)

	ctx := context.Background()

	commonReq := &manage.ServiceCommonRequest{
		Region:             *region,
		Cluster:            *cluster,
		ServiceName:        *service,
		CatalogServiceType: *serviceType,
	}

	*serviceType = strings.ToLower(*serviceType)
	switch *op {
	case opCreate:
		if *maxMemMB != common.DefaultMaxMemoryMB && *maxMemMB < *reserveMemMB {
			fmt.Println("Invalid request, max-memory", *maxMemMB, "should be larger than reserve-memory", *reserveMemMB)
			os.Exit(-1)
		}
		if *maxCPUUnits != common.DefaultMaxCPUUnits && *maxCPUUnits < *reserveCPUUnits {
			fmt.Println("Invalid request, max-cpuunits", *maxCPUUnits, "should be larger than reserve-cpuunits", *reserveCPUUnits)
			os.Exit(-1)
		}
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
			createMongoDBService(ctx, catalogcli, commonReq, journalVol)
		case common.CatalogService_PostgreSQL:
			createPostgreSQLService(ctx, catalogcli, commonReq, journalVol)
		case common.CatalogService_Cassandra:
			createCassandraService(ctx, catalogcli, commonReq, journalVol)
		case common.CatalogService_ZooKeeper:
			createZkService(ctx, catalogcli, commonReq)
		case common.CatalogService_Kafka:
			createKafkaService(ctx, catalogcli, commonReq)
		case common.CatalogService_KafkaSinkES:
			createKafkaSinkESService(ctx, catalogcli, commonReq)
		case common.CatalogService_KafkaManager:
			createKafkaManagerService(ctx, catalogcli, commonReq)
		case common.CatalogService_Redis:
			createRedisService(ctx, catalogcli, commonReq)
		case common.CatalogService_CouchDB:
			createCouchDBService(ctx, catalogcli, commonReq)
		case common.CatalogService_Consul:
			createConsulService(ctx, catalogcli, commonReq)
		case common.CatalogService_ElasticSearch:
			createESService(ctx, catalogcli, commonReq)
		case common.CatalogService_Kibana:
			createKibanaService(ctx, catalogcli, commonReq)
		case common.CatalogService_Logstash:
			createLogstashService(ctx, catalogcli, commonReq)
		case common.CatalogService_Telegraf:
			createTelService(ctx, catalogcli, commonReq)
		default:
			fmt.Printf("Invalid service type, please specify %s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s\n",
				common.CatalogService_MongoDB, common.CatalogService_PostgreSQL,
				common.CatalogService_Cassandra, common.CatalogService_ZooKeeper,
				common.CatalogService_Kafka, common.CatalogService_KafkaManager,
				common.CatalogService_Redis, common.CatalogService_CouchDB,
				common.CatalogService_Consul, common.CatalogService_ElasticSearch,
				common.CatalogService_Kibana, common.CatalogService_Logstash,
				common.CatalogService_Telegraf)
			os.Exit(-1)
		}

		waitServiceRunning(ctx, cli, commonReq)

	case opUpdateResource:
		updateServiceResource(ctx, cli, commonReq)

	case opUpdate:
		switch *serviceType {
		case common.CatalogService_Cassandra:
			updateCassandraService(ctx, cli, commonReq)
		case common.CatalogService_Redis:
			updateRedisService(ctx, cli, commonReq)
		case common.CatalogService_Kafka:
			updateKafkaService(ctx, cli, commonReq)
		case common.CatalogService_ZooKeeper:
			updateZkService(ctx, cli, commonReq)
		default:
			fmt.Printf("Invalid service type, update service only support cassandra|redis|kafka|zookeeper\n")
			os.Exit(-1)
		}

	case opScale:
		switch *serviceType {
		case common.CatalogService_Cassandra:
			scaleCassandraService(ctx, catalogcli, commonReq)
		default:
			fmt.Printf("Invalid service type, scale service only support cassandra\n")
			os.Exit(-1)
		}

		waitServiceRunning(ctx, cli, commonReq)

	case opStop:
		stopService(ctx, cli, commonReq)

	case opStart:
		startService(ctx, cli, commonReq)

	case opUpgrade:
		upgradeService(ctx, cli, commonReq)

	case opRollingRestart:
		rollingRestartService(ctx, cli, commonReq)

	case opCheckInit:
		checkServiceInit(ctx, catalogcli, commonReq)

	case opDelete:
		deleteService(ctx, cli, commonReq)

	case opList:
		listServices(ctx, cli)

	case opGet:
		getService(ctx, cli, commonReq)

	case opListMembers:
		members := listServiceMembers(ctx, cli, commonReq)
		fmt.Printf("List %d members:\n", len(members))
		for _, member := range members {
			fmt.Printf("\t%+v\n", *member)
			for _, cfg := range member.Spec.Configs {
				fmt.Printf("\t\t%+v\n", cfg)
			}
		}

	case opGetConfig:
		getConfigFile(ctx, cli, commonReq)

	default:
		fmt.Printf("Invalid operation, please specify %s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s\n",
			opCreate, opCheckInit, opUpdate, opScale, opStop, opStart, opRollingRestart, opDelete, opList, opGet, opListMembers, opGetConfig)
		os.Exit(-1)
	}
}

func createMongoDBService(ctx context.Context, cli *catalogclient.CatalogServiceClient, commonReq *manage.ServiceCommonRequest, journalVol *common.ServiceVolume) {
	if *replicas == 0 || *volSizeGB == 0 {
		fmt.Println("please specify the valid replica number and volume size")
		os.Exit(-1)
	}
	if journalVol == nil {
		fmt.Println("please specify the separate journal volume for MongoDB")
		os.Exit(-1)
	}

	req := &catalog.CatalogCreateMongoDBRequest{
		Service: commonReq,
		Resource: &common.Resources{
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Options: &catalog.CatalogMongoDBOptions{
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

	initReq := &catalog.CatalogCheckServiceInitRequest{
		Service: req.Service,
	}
	waitServiceInit(ctx, cli, initReq)
}

func createCassandraService(ctx context.Context, cli *catalogclient.CatalogServiceClient, commonReq *manage.ServiceCommonRequest, journalVol *common.ServiceVolume) {
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

	req := &catalog.CatalogCreateCassandraRequest{
		Service: commonReq,
		Resource: &common.Resources{
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Options: &catalog.CatalogCassandraOptions{
			Replicas: *replicas,
			Volume: &common.ServiceVolume{
				VolumeType:   *volType,
				Iops:         *volIops,
				VolumeSizeGB: *volSizeGB,
				Encrypted:    *volEncrypted,
			},
			JournalVolume:   journalVol,
			HeapSizeMB:      *casHeapSizeMB,
			JmxRemoteUser:   *jmxUser,
			JmxRemotePasswd: *jmxPasswd,
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

	initReq := &catalog.CatalogCheckServiceInitRequest{
		Service: req.Service,
	}

	if req.Options.Replicas > 1 {
		waitServiceInit(ctx, cli, initReq)
	}
}

func updateCassandraService(ctx context.Context, cli *manageclient.ManageClient, commonReq *manage.ServiceCommonRequest) {
	// get all set command-line flags
	flagset := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { flagset[f.Name] = true })

	heapSizeMB := int64(0)
	if flagset[flagCasHeapSize] {
		heapSizeMB = *casHeapSizeMB
		if heapSizeMB < cascatalog.DefaultHeapMB {
			fmt.Printf("the heap size is less than %d. Please increase it for production system\n", cascatalog.DefaultHeapMB)
		}
		if heapSizeMB < cascatalog.MinHeapMB {
			fmt.Printf("the heap size is lessn than %d, Cassandra JVM may stall long time at GC\n", cascatalog.MinHeapMB)
		}
		if heapSizeMB > cascatalog.MaxHeapMB {
			fmt.Errorf("invalid parameters - max cassandra heap size is %d", cascatalog.MaxHeapMB)
			os.Exit(-1)
		}
	}
	user := ""
	passwd := ""
	if flagset[flagJmxUser] {
		user = *jmxUser
		passwd = *jmxPasswd
	}

	updateServiceHeapAndJMX(ctx, cli, commonReq, heapSizeMB, user, passwd)

	fmt.Println(time.Now().UTC(), "The catalog service is updated. Please stop and start the service to load the new configs")
}

func updateServiceHeapAndJMX(ctx context.Context, cli *manageclient.ManageClient, commonReq *manage.ServiceCommonRequest, heapSizeMB int64, jmxUser string, jmxPasswd string) {
	err := catalog.ValidateUpdateOptions(heapSizeMB, jmxUser, jmxPasswd)
	if err != nil {
		fmt.Errorf("invalid parameters - %s", err)
		os.Exit(-1)
	}

	cfgFile := getServiceConfigFile(ctx, cli, commonReq)

	newContent := catalog.UpdateServiceConfigHeapAndJMX(cfgFile.Spec.Content, heapSizeMB, jmxUser, jmxPasswd)

	// update service config
	r := &manage.UpdateServiceConfigRequest{
		Service:           commonReq,
		ConfigFileName:    cfgFile.Meta.FileName,
		ConfigFileContent: newContent,
	}
	err = cli.UpdateServiceConfig(ctx, r)
	if err != nil {
		fmt.Errorf("update service error %s", err)
		os.Exit(-2)
	}
}

func getServiceConfigFile(ctx context.Context, cli *manageclient.ManageClient, commonReq *manage.ServiceCommonRequest) *common.ConfigFile {
	req := &manage.GetServiceConfigFileRequest{
		Service:        commonReq,
		ConfigFileName: catalog.SERVICE_FILE_NAME,
	}

	cfgFile, err := cli.GetServiceConfigFile(ctx, req)
	if err != nil {
		fmt.Errorf("GetConfigFile error %s", err)
		os.Exit(-2)
	}

	return cfgFile
}

func scaleCassandraService(ctx context.Context, cli *catalogclient.CatalogServiceClient, commonReq *manage.ServiceCommonRequest) {
	req := &catalog.CatalogScaleCassandraRequest{
		Service:  commonReq,
		Replicas: *replicas,
	}

	err := cli.CatalogScaleCassandraService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "scale cassandra service error", err)
		os.Exit(-1)
	}

	fmt.Println("The cassandra service is scaled to", *replicas, "nodes, wait for the containers running")
}

func waitServiceInit(ctx context.Context, cli *catalogclient.CatalogServiceClient, initReq *catalog.CatalogCheckServiceInitRequest) {
	for sec := time.Duration(0); sec < maxServiceWaitTime; sec += retryWaitTime {
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
		time.Sleep(retryWaitTime)
	}

	fmt.Println(time.Now().UTC(), "The catalog service is not initialized after", maxServiceWaitTime)
	os.Exit(-1)
}

func createZkService(ctx context.Context, cli *catalogclient.CatalogServiceClient, commonReq *manage.ServiceCommonRequest) {
	if *replicas == 0 || *volSizeGB == 0 {
		fmt.Println("please specify the valid replica number and volume size")
		os.Exit(-1)
	}
	if *zkHeapSizeMB < zkcatalog.DefaultHeapMB {
		fmt.Printf("The ZooKeeper heap size is less than %d. Please increase it for production system\n", zkcatalog.DefaultHeapMB)
	}

	req := &catalog.CatalogCreateZooKeeperRequest{
		Service: commonReq,
		Resource: &common.Resources{
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Options: &catalog.CatalogZooKeeperOptions{
			Replicas: *replicas,
			Volume: &common.ServiceVolume{
				VolumeType:   *volType,
				Iops:         *volIops,
				VolumeSizeGB: *volSizeGB,
				Encrypted:    *volEncrypted,
			},
			HeapSizeMB:      *zkHeapSizeMB,
			JmxRemoteUser:   *jmxUser,
			JmxRemotePasswd: *jmxPasswd,
		},
	}

	jmxUser, jmxPasswd, err := cli.CatalogCreateZooKeeperService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "create zookeeper service error", err)
		os.Exit(-1)
	}

	fmt.Println(time.Now().UTC(), "The zookeeper service is created, jmx user", jmxUser, "password", jmxPasswd)
	fmt.Println(time.Now().UTC(), "Wait for all containers running")
}

func updateZkService(ctx context.Context, cli *manageclient.ManageClient, commonReq *manage.ServiceCommonRequest) {
	// get all set command-line flags
	flagset := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { flagset[f.Name] = true })

	heapSizeMB := int64(0)
	if flagset[flagZkHeapSize] {
		heapSizeMB = *zkHeapSizeMB
	}
	user := ""
	passwd := ""
	if flagset[flagJmxUser] {
		user = *jmxUser
		passwd = *jmxPasswd
	}

	updateServiceHeapAndJMX(ctx, cli, commonReq, heapSizeMB, user, passwd)

	fmt.Println("The catalog service is updated. Please restart the service to load the new configs")
}

func createKafkaService(ctx context.Context, cli *catalogclient.CatalogServiceClient, commonReq *manage.ServiceCommonRequest) {
	if *replicas == 0 || *volSizeGB == 0 || *kafkaZkService == "" {
		fmt.Println("please specify the valid replica number, volume size and zookeeper service name")
		os.Exit(-1)
	}
	if *kafkaHeapSizeMB < kafkacatalog.DefaultHeapMB {
		fmt.Printf("The Kafka heap size is less than %d. Please increase it for production system\n", kafkacatalog.DefaultHeapMB)
	}

	req := &catalog.CatalogCreateKafkaRequest{
		Service: commonReq,
		Resource: &common.Resources{
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Options: &catalog.CatalogKafkaOptions{
			Replicas: *replicas,
			Volume: &common.ServiceVolume{
				VolumeType:   *volType,
				Iops:         *volIops,
				VolumeSizeGB: *volSizeGB,
				Encrypted:    *volEncrypted,
			},

			HeapSizeMB:      *kafkaHeapSizeMB,
			AllowTopicDel:   *kafkaAllowTopicDel,
			RetentionHours:  *kafkaRetentionHours,
			ZkServiceName:   *kafkaZkService,
			JmxRemoteUser:   *jmxUser,
			JmxRemotePasswd: *jmxPasswd,
		},
	}

	jmxUser, jmxPasswd, err := cli.CatalogCreateKafkaService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "create kafka service error", err)
		os.Exit(-1)
	}

	fmt.Println(time.Now().UTC(), "The kafka service is created, jmx user", jmxUser, "password", jmxPasswd)
	fmt.Println(time.Now().UTC(), "Wait for all containers running")
}

func createKafkaSinkESService(ctx context.Context, cli *catalogclient.CatalogServiceClient, commonReq *manage.ServiceCommonRequest) {
	if *replicas == 0 || *kcKafkaService == "" || *kcKafkaTopic == "" || *kcSinkESService == "" {
		fmt.Println("please specify the valid replica number, kafka service name, kafka topic, and elasticsearch service name")
		os.Exit(-1)
	}
	if *kcHeapSizeMB < kccatalog.DefaultHeapMB {
		fmt.Printf("The Kafka connec heap size is less than %d. Please increase it for production system\n", kccatalog.DefaultHeapMB)
	}

	req := &catalog.CatalogCreateKafkaSinkESRequest{
		Service: commonReq,
		Resource: &common.Resources{
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Options: &catalog.CatalogKafkaSinkESOptions{
			Replicas:           *replicas,
			HeapSizeMB:         *kcHeapSizeMB,
			KafkaServiceName:   *kcKafkaService,
			Topic:              *kcKafkaTopic,
			ESServiceName:      *kcSinkESService,
			TypeName:           *kcSinkESType,
			MaxBufferedRecords: *kcSinkESBufferedRecords,
			BatchSize:          *kcSinkESBatchSize,
			ReplFactor:         *kcReplFactor,
		},
	}

	err := cli.CatalogCreateKafkaSinkESService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "create kafka connect service error", err)
		os.Exit(-1)
	}

	fmt.Println(time.Now().UTC(), "The service is created, wait till it gets initialized")

	initReq := &catalog.CatalogCheckServiceInitRequest{
		Service: req.Service,
	}

	waitServiceInit(ctx, cli, initReq)
}

func updateKafkaService(ctx context.Context, cli *manageclient.ManageClient, commonReq *manage.ServiceCommonRequest) {
	// get all set command-line flags
	flagset := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { flagset[f.Name] = true })

	heapSizeMB := int64(0)
	if flagset[flagKafkaHeapSize] {
		heapSizeMB = *kafkaHeapSizeMB
	}
	retentionHours := int64(0)
	if flagset[flagKafkaRetentionHours] {
		retentionHours = *kafkaRetentionHours
	}
	user := ""
	passwd := ""
	if flagset[flagJmxUser] {
		user = *jmxUser
		passwd = *jmxPasswd
	}
	var allowTopicDel *bool
	if flagset[flagKafkaAllowTopicDel] {
		allowTopicDel = utils.BoolPtr(*kafkaAllowTopicDel)
	}

	opts := &kafkacatalog.KafkaOptions{
		HeapSizeMB:      heapSizeMB,
		AllowTopicDel:   allowTopicDel,
		RetentionHours:  retentionHours,
		JmxRemoteUser:   user,
		JmxRemotePasswd: passwd,
	}

	err := kafkacatalog.ValidateUpdateOptions(opts)
	if err != nil {
		fmt.Errorf("invalid parameters - %s", err)
		os.Exit(-1)
	}

	cfgFile := getServiceConfigFile(ctx, cli, commonReq)

	newContent := kafkacatalog.UpdateServiceConfigs(cfgFile.Spec.Content, opts)

	// update service config
	r := &manage.UpdateServiceConfigRequest{
		Service:           commonReq,
		ConfigFileName:    cfgFile.Meta.FileName,
		ConfigFileContent: newContent,
	}
	err = cli.UpdateServiceConfig(ctx, r)
	if err != nil {
		fmt.Errorf("update service error %s", err)
		os.Exit(-2)
	}

	fmt.Println("The catalog service is updated. Please restart the service to load the new configs")
}

func createKafkaManagerService(ctx context.Context, cli *catalogclient.CatalogServiceClient, commonReq *manage.ServiceCommonRequest) {
	if *kmZkService == "" {
		fmt.Println("please specify the valid zookeeper service name")
		os.Exit(-1)
	}
	if *kmHeapSizeMB < kmcatalog.DefaultHeapMB {
		fmt.Printf("The Kafka Manager heap size is less than %d. Please increase it for production system\n", kmcatalog.DefaultHeapMB)
	}

	req := &catalog.CatalogCreateKafkaManagerRequest{
		Service: commonReq,
		Resource: &common.Resources{
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Options: &catalog.CatalogKafkaManagerOptions{
			HeapSizeMB:    *kmHeapSizeMB,
			User:          *kmUser,
			Password:      *kmPasswd,
			ZkServiceName: *kmZkService,
		},
	}

	err := kmcatalog.ValidateRequest(req.Options)
	if err != nil {
		fmt.Println("invalid request", err)
		os.Exit(-1)
	}

	err = cli.CatalogCreateKafkaManagerService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "create kafka manager service error", err)
		os.Exit(-1)
	}

	fmt.Println(time.Now().UTC(), "The kafka manager service is created, wait for all containers running")
}

func createRedisService(ctx context.Context, cli *catalogclient.CatalogServiceClient, commonReq *manage.ServiceCommonRequest) {
	if *redisMemSizeMB <= 0 || *volSizeGB <= 0 {
		fmt.Println("please specify the valid memory and volume size")
		os.Exit(-1)
	}

	req := &catalog.CatalogCreateRedisRequest{
		Service: commonReq,
		Resource: &common.Resources{
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Options: &catalog.CatalogRedisOptions{
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

		initReq := &catalog.CatalogCheckServiceInitRequest{
			Service: req.Service,
		}

		waitServiceInit(ctx, cli, initReq)
	}

	fmt.Println(time.Now().UTC(), "The service is created, wait for all containers running")
}

func updateRedisService(ctx context.Context, cli *manageclient.ManageClient, commonReq *manage.ServiceCommonRequest) {
	if *redisMemSizeMB < 0 {
		fmt.Println("please specify the valid memory size")
	}

	// get all set command-line flags
	flagset := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { flagset[f.Name] = true })

	memSizeMB := int64(0)
	if flagset[flagRedisMemSize] {
		memSizeMB = *redisMemSizeMB
	}
	authPass := ""
	if flagset[flagRedisAuthPass] {
		authPass = *redisAuthPass
	}
	replTimeout := int64(0)
	if flagset[flagRedisReplTimeout] {
		replTimeout = *redisReplTimeoutSecs
	}
	maxMemPol := ""
	if flagset[flagRedisMaxMemPol] {
		maxMemPol = *redisMaxMemPolicy
	}
	var cfgcmdName *string
	if flagset[flagRedisConfigCmd] {
		cfgcmdName = &(*redisConfigCmdName)
	}

	opts := &rediscatalog.RedisOptions{
		MemoryCacheSizeMB: memSizeMB,
		AuthPass:          authPass,
		ReplTimeoutSecs:   replTimeout,
		MaxMemPolicy:      maxMemPol,
		ConfigCmdName:     cfgcmdName,
	}

	err := rediscatalog.ValidateUpdateOptions(opts)
	if err != nil {
		fmt.Errorf("invalid parameters - %s", err)
		os.Exit(-1)
	}

	cfgFile := getServiceConfigFile(ctx, cli, commonReq)

	newContent := rediscatalog.UpdateServiceConfigs(cfgFile.Spec.Content, opts)

	// update service config
	r := &manage.UpdateServiceConfigRequest{
		Service:           commonReq,
		ConfigFileName:    cfgFile.Meta.FileName,
		ConfigFileContent: newContent,
	}
	err = cli.UpdateServiceConfig(ctx, r)
	if err != nil {
		fmt.Errorf("update service error %s", err)
		os.Exit(-2)
	}

	fmt.Println("The catalog service is updated. Please stop and start the service to load the new configs")
}

func createCouchDBService(ctx context.Context, cli *catalogclient.CatalogServiceClient, commonReq *manage.ServiceCommonRequest) {
	if *volSizeGB == 0 {
		fmt.Println("please specify the valid volume size")
		os.Exit(-1)
	}

	req := &catalog.CatalogCreateCouchDBRequest{
		Service: commonReq,
		Resource: &common.Resources{
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Options: &catalog.CatalogCouchDBOptions{
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

	initReq := &catalog.CatalogCheckServiceInitRequest{
		Service: req.Service,
	}

	waitServiceInit(ctx, cli, initReq)
}

func createConsulService(ctx context.Context, cli *catalogclient.CatalogServiceClient, commonReq *manage.ServiceCommonRequest) {
	if *volSizeGB == 0 {
		fmt.Println("please specify the valid volume size")
		os.Exit(-1)
	}

	req := &catalog.CatalogCreateConsulRequest{
		Service: commonReq,
		Resource: &common.Resources{
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Options: &catalog.CatalogConsulOptions{
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
}

func createESService(ctx context.Context, cli *catalogclient.CatalogServiceClient, commonReq *manage.ServiceCommonRequest) {
	if *replicas == 0 || *volSizeGB == 0 {
		fmt.Println("please specify the valid replica number and volume size")
		os.Exit(-1)
	}
	if *esHeapSizeMB <= escatalog.DefaultHeapMB {
		fmt.Printf("The ElasticSearch heap size equals to or less than %d. Please increase it for production system\n", escatalog.DefaultHeapMB)
	}

	req := &catalog.CatalogCreateElasticSearchRequest{
		Service: commonReq,
		Resource: &common.Resources{
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Options: &catalog.CatalogElasticSearchOptions{
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
}

func createKibanaService(ctx context.Context, cli *catalogclient.CatalogServiceClient, commonReq *manage.ServiceCommonRequest) {
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

	req := &catalog.CatalogCreateKibanaRequest{
		Service: commonReq,
		Resource: &common.Resources{
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Options: &catalog.CatalogKibanaOptions{
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
}

func createLogstashService(ctx context.Context, cli *catalogclient.CatalogServiceClient, commonReq *manage.ServiceCommonRequest) {
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

	req := &catalog.CatalogCreateLogstashRequest{
		Service: commonReq,
		Resource: &common.Resources{
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Options: &catalog.CatalogLogstashOptions{
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
}

func createPostgreSQLService(ctx context.Context, cli *catalogclient.CatalogServiceClient, commonReq *manage.ServiceCommonRequest, journalVol *common.ServiceVolume) {
	if *replicas == 0 || *volSizeGB == 0 {
		fmt.Println("please specify the valid replica number and volume size")
		os.Exit(-1)
	}
	if journalVol == nil {
		fmt.Println("please specify the separate journal volume for PostgreSQL")
		os.Exit(-1)
	}

	req := &catalog.CatalogCreatePostgreSQLRequest{
		Service: commonReq,
		Resource: &common.Resources{
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Options: &catalog.CatalogPostgreSQLOptions{
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
}

func createTelService(ctx context.Context, cli *catalogclient.CatalogServiceClient, commonReq *manage.ServiceCommonRequest) {
	if *telMonitorServiceName == "" || *telMonitorServiceType == "" {
		fmt.Println("please specify the valid monitor service name and type")
		os.Exit(-1)
	}

	req := &catalog.CatalogCreateTelegrafRequest{
		Service: commonReq,
		Resource: &common.Resources{
			MaxCPUUnits:     *maxCPUUnits,
			ReserveCPUUnits: *reserveCPUUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},
		Options: &catalog.CatalogTelegrafOptions{
			CollectIntervalSecs: *telCollectIntervalSecs,
			MonitorServiceName:  *telMonitorServiceName,
			MonitorServiceType:  *telMonitorServiceType,
		},
	}

	if *telMetricsFile != "" {
		// load the content of the metrics file
		b, err := ioutil.ReadFile(*telMetricsFile)
		if err != nil {
			fmt.Println("read metrics file error", err, *telMetricsFile)
			os.Exit(-1)
		}
		req.Options.MonitorMetrics = string(b)
	}

	err := telcatalog.ValidateRequest(req)
	if err != nil {
		fmt.Println("invalid parameters", err)
		os.Exit(-1)
	}

	err = cli.CatalogCreateTelegrafService(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "create service error", err)
		os.Exit(-1)
	}

	fmt.Println(time.Now().UTC(), "service is created, wait for all containers running")
}

func waitServiceRunning(ctx context.Context, cli *manageclient.ManageClient, commonReq *manage.ServiceCommonRequest) {
	// sometimes, the container orchestration framework returns NotFound when GetServiceStatus right
	// after the service is created. sleep some time.
	time.Sleep(retryWaitTime)

	for sec := time.Duration(0); sec < maxServiceWaitTime; sec += retryWaitTime {
		status, err := cli.GetServiceStatus(ctx, commonReq)
		if err != nil {
			// The service is successfully created. It may be possible there are some
			// temporary error, such as network error. For example, ECS may return MISSING
			// for the GET right after the service creation.
			// Here just log the GetServiceStatus error and retry.
			fmt.Println(time.Now().UTC(), "GetServiceStatus error", err, commonReq)
		} else {
			// ECS may return 0 desired count for the service right after the service is created.
			// If the desired count is 0, wait and retry.
			if status.RunningCount == status.DesiredCount && status.DesiredCount != 0 {
				fmt.Println(time.Now().UTC(), "All service containers are running, RunningCount", status.RunningCount)
				return
			}
			fmt.Println(time.Now().UTC(), "wait the service containers running, RunningCount", status.RunningCount)
		}

		time.Sleep(retryWaitTime)
	}

	fmt.Println(time.Now().UTC(), "not all service containers are running after", maxServiceWaitTime)
	os.Exit(-1)
}

func checkServiceInit(ctx context.Context, cli *catalogclient.CatalogServiceClient, commonReq *manage.ServiceCommonRequest) {
	req := &catalog.CatalogCheckServiceInitRequest{
		Service: commonReq,
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

func listServices(ctx context.Context, cli *manageclient.ManageClient) {
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

func getService(ctx context.Context, cli *manageclient.ManageClient, commonReq *manage.ServiceCommonRequest) {
	attr, err := cli.GetServiceAttr(ctx, commonReq)
	if err != nil {
		fmt.Println(time.Now().UTC(), "GetServiceAttr error", err)
		os.Exit(-1)
	}

	fmt.Printf("%+v\n", *attr)

	cfgFile := getServiceConfigFile(ctx, cli, commonReq)
	fmt.Println(cfgFile.Spec.Content)
}

func listServiceMembers(ctx context.Context, cli *manageclient.ManageClient, commonReq *manage.ServiceCommonRequest) []*common.ServiceMember {
	// list all service members
	listReq := &manage.ListServiceMemberRequest{
		Service: commonReq,
	}

	members, err := cli.ListServiceMember(ctx, listReq)
	if err != nil {
		fmt.Println(time.Now().UTC(), "ListServiceMember error", err)
		os.Exit(-1)
	}

	return members
}

func updateServiceResource(ctx context.Context, cli *manageclient.ManageClient, commonReq *manage.ServiceCommonRequest) {
	// get all set command-line flags
	flagset := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { flagset[f.Name] = true })

	req := &manage.UpdateServiceResourceRequest{
		Service: commonReq,
	}
	if flagset[flagMaxCPU] {
		req.MaxCPUUnits = utils.Int64Ptr(*maxCPUUnits)
	}
	if flagset[flagReserveCPU] {
		req.ReserveCPUUnits = utils.Int64Ptr(*reserveCPUUnits)
	}
	if flagset[flagMaxMem] {
		req.MaxMemMB = utils.Int64Ptr(*maxMemMB)
	}
	if flagset[flagReserveMem] {
		req.ReserveMemMB = utils.Int64Ptr(*reserveMemMB)
	}

	err := cli.UpdateServiceResource(ctx, req)
	if err != nil {
		fmt.Println(time.Now().UTC(), "update service resource error", err)
		os.Exit(-1)
	}

	fmt.Println("The service is updated")
}

func stopService(ctx context.Context, cli *manageclient.ManageClient, commonReq *manage.ServiceCommonRequest) {
	// stop the service containers
	err := cli.StopService(ctx, commonReq)
	if err != nil {
		fmt.Println(time.Now().UTC(), "StopService error", err)
		os.Exit(-1)
	}

	fmt.Println("Service stopped")
}

func startService(ctx context.Context, cli *manageclient.ManageClient, commonReq *manage.ServiceCommonRequest) {
	// start the service containers
	err := cli.StartService(ctx, commonReq)
	if err != nil {
		fmt.Println(time.Now().UTC(), "StartService error", err)
		os.Exit(-1)
	}

	fmt.Println("Service started")

	waitServiceRunning(ctx, cli, commonReq)
}

func upgradeService(ctx context.Context, cli *manageclient.ManageClient, commonReq *manage.ServiceCommonRequest) {
	req := &manage.UpgradeServiceRequest{
		Service: commonReq,
	}

	if *serviceType == common.CatalogService_KafkaManager {
		req.ContainerImage = kmcatalog.ReleaseContainerImage
	} else if *serviceType == common.CatalogService_KafkaSinkES {
		req.ContainerImage = kccatalog.ReleaseContainerImage
	}

	err := cli.UpgradeService(ctx, commonReq)
	if err != nil {
		fmt.Println(time.Now().UTC(), "UpgradeService error", err)
		os.Exit(-1)
	}

	fmt.Println("Service upgraded")
}

func rollingRestartService(ctx context.Context, cli *manageclient.ManageClient, commonReq *manage.ServiceCommonRequest) {
	// start the service containers
	err := cli.RollingRestartService(ctx, commonReq)
	if err != nil {
		fmt.Println(time.Now().UTC(), "RollingRestartService error", err)
		os.Exit(-1)
	}

	fmt.Println("Service rolling restart is triggered")

	// wait till all containers are rolling restarted
	for true {
		complete, statusMsg, err := cli.GetServiceTaskStatus(ctx, commonReq)
		if err != nil {
			fmt.Println(time.Now().UTC(), "GetServiceTaskStatus error", err)
			os.Exit(-1)
		}
		if complete {
			fmt.Println(time.Now().UTC(), "complete:", statusMsg)
			return
		}
		fmt.Println(time.Now().UTC(), statusMsg)
		time.Sleep(retryWaitTime)
	}
}

func deleteService(ctx context.Context, cli *manageclient.ManageClient, commonReq *manage.ServiceCommonRequest) {
	// delete the service from the control plane.
	serviceReq := &manage.DeleteServiceRequest{
		Service: commonReq,
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

func getConfigFile(ctx context.Context, cli *manageclient.ManageClient, commonReq *manage.ServiceCommonRequest) {
	if *cfgFileName == "" {
		fmt.Println("Please specify the service config file name")
		os.Exit(-1)
	}

	if *memberName == "" {
		// get service config file
		req := &manage.GetServiceConfigFileRequest{
			Service:        commonReq,
			ConfigFileName: *cfgFileName,
		}

		cfg, err := cli.GetServiceConfigFile(ctx, req)
		if err != nil {
			fmt.Println(time.Now().UTC(), "GetServiceConfigFile error", err)
			os.Exit(-1)
		}

		fmt.Printf("%+v\n", *cfg)
	} else {
		// get service config file
		req := &manage.GetMemberConfigFileRequest{
			Service:        commonReq,
			MemberName:     *memberName,
			ConfigFileName: *cfgFileName,
		}

		cfg, err := cli.GetMemberConfigFile(ctx, req)
		if err != nil {
			fmt.Println(time.Now().UTC(), "GetMemberConfigFile error", err)
			os.Exit(-1)
		}

		fmt.Printf("%+v\n", *cfg)
	}
}
