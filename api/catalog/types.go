package catalog

import (
	"github.com/cloudstax/firecamp/api/manage"
	"github.com/cloudstax/firecamp/api/common"
)

const (
	CatalogOpPrefix              = manage.SpecialOpPrefix + "Catalog-"
	CatalogCreateMongoDBOp       = CatalogOpPrefix + "Create-MongoDB"
	CatalogCreatePostgreSQLOp    = CatalogOpPrefix + "Create-PostgreSQL"
	CatalogCreateCassandraOp     = CatalogOpPrefix + "Create-Cassandra"
	CatalogCreateZooKeeperOp     = CatalogOpPrefix + "Create-ZooKeeper"
	CatalogCreateKafkaOp         = CatalogOpPrefix + "Create-Kafka"
	CatalogCreateKafkaSinkESOp   = CatalogOpPrefix + "Create-Kafka-SinkES"
	CatalogCreateKafkaManagerOp  = CatalogOpPrefix + "Create-Kafka-Manager"
	CatalogCreateRedisOp         = CatalogOpPrefix + "Create-Redis"
	CatalogCreateCouchDBOp       = CatalogOpPrefix + "Create-CouchDB"
	CatalogCreateConsulOp        = CatalogOpPrefix + "Create-Consul"
	CatalogCreateElasticSearchOp = CatalogOpPrefix + "Create-ElasticSearch"
	CatalogCreateKibanaOp        = CatalogOpPrefix + "Create-Kibana"
	CatalogCreateLogstashOp      = CatalogOpPrefix + "Create-Logstash"
	CatalogCreateTelegrafOp      = CatalogOpPrefix + "Create-Telegraf"
	CatalogCheckServiceInitOp    = CatalogOpPrefix + "Check-Service-Init"
	CatalogSetServiceInitOp      = CatalogOpPrefix + "Set-Service-Init"
	CatalogScaleCassandraOp      = CatalogOpPrefix + "Scale-Cassandra"

	// The service configurable variables and some service specific variables
	SERVICE_FILE_NAME = "service.conf"
	// The specific variables for one service member
	MEMBER_FILE_NAME = "member.conf"

	BindAllIP = "0.0.0.0"

	// the default password is uuid for better security
	// every service will define its own jmx port
	JmxDefaultRemoteUser = "jmxuser"
	JmxReadOnlyAccess    = "readonly"
	JmxReadWriteAccess   = "readwrite"
)

// The integrated catalog services supported by the system.

// CatalogMongoDBOptions includes the config options for MongoDB.
type CatalogMongoDBOptions struct {
	// if ReplicaSetOnly == true and Shards == 1, create a single replicaset, else create a sharded cluster.
	Shards           int64
	ReplicasPerShard int64
	ReplicaSetOnly   bool
	// the number of config servers, ignored if ReplicaSetOnly == true and Shards == 1.
	ConfigServers int64

	Volume        *common.ServiceVolume
	JournalVolume *common.ServiceVolume

	Admin       string
	AdminPasswd string
}

// CatalogCreateMongoDBRequest creates a MongoDB ReplicaSet service.
type CatalogCreateMongoDBRequest struct {
	Service  *manage.ServiceCommonRequest
	Resource *common.Resources
	Options  *CatalogMongoDBOptions
}

// CatalogCreateMongoDBResponse returns the keyfile content for mongos to access the sharded cluster.
type CatalogCreateMongoDBResponse struct {
	KeyFileContent string
}

// CatalogPostgreSQLOptions includes the config options for PostgreSQL.
type CatalogPostgreSQLOptions struct {
	Replicas      int64
	Volume        *common.ServiceVolume
	JournalVolume *common.ServiceVolume

	// The container image for the service, such as cloudstax/firecamp-postgres:version or cloudstax/firecamp-postgres-postgis:version
	ContainerImage string

	// the default admin user is: postgres
	AdminPasswd    string
	ReplUser       string
	ReplUserPasswd string
}

// CatalogCreatePostgreSQLRequest creates a PostgreSQL service.
type CatalogCreatePostgreSQLRequest struct {
	Service  *manage.ServiceCommonRequest
	Resource *common.Resources
	Options  *CatalogPostgreSQLOptions
}

// CatalogCassandraOptions includes the config options for Cassandra.
type CatalogCassandraOptions struct {
	Replicas      int64
	Volume        *common.ServiceVolume
	JournalVolume *common.ServiceVolume

	// Cassandra JVM heap size. The default volue is 8GB.
	HeapSizeMB int64
	// The user for the JMX remote access. If empty, will be set as "jmxuser".
	JmxRemoteUser string
	// The password for the JMX remote access. If empty, a uuid will be generated
	// as the password. This will be used by nodetool to access replica remotely.
	JmxRemotePasswd string
}

// CatalogCreateCassandraRequest creates a Cassandra service.
type CatalogCreateCassandraRequest struct {
	Service  *manage.ServiceCommonRequest
	Resource *common.Resources
	Options  *CatalogCassandraOptions
}

// CatalogCreateCassandraResponse returns the Cassandra JMX user and password.
type CatalogCreateCassandraResponse struct {
	JmxRemoteUser   string
	JmxRemotePasswd string
}

// CatalogScaleCassandraRequest scales the Cassandra service.
type CatalogScaleCassandraRequest struct {
	Service  *manage.ServiceCommonRequest
	Replicas int64
}

// CatalogZooKeeperOptions includes the options for ZooKeeper.
type CatalogZooKeeperOptions struct {
	Replicas int64
	Volume   *common.ServiceVolume

	// ZooKeeper JVM heap size
	HeapSizeMB int64

	// The user for the JMX remote access. If empty, will be set as "jmxuser".
	JmxRemoteUser string
	// The password for the JMX remote access. If empty, a uuid will be generated as the password.
	JmxRemotePasswd string
}

// CatalogCreateZooKeeperRequest creates a ZooKeeper service.
type CatalogCreateZooKeeperRequest struct {
	Service  *manage.ServiceCommonRequest
	Resource *common.Resources
	Options  *CatalogZooKeeperOptions
}

// CatalogCreateZooKeeperResponse returns the ZooKeeper JMX user and password.
type CatalogCreateZooKeeperResponse struct {
	JmxRemoteUser   string
	JmxRemotePasswd string
}

// CatalogKafkaOptions includes the options for Kafka.
type CatalogKafkaOptions struct {
	Replicas int64
	Volume   *common.ServiceVolume

	// Kafka JVM heap size
	HeapSizeMB int64

	AllowTopicDel  bool
	RetentionHours int64
	// the existing ZooKeeper service that Kafka will use.
	ZkServiceName string

	JmxRemoteUser   string
	JmxRemotePasswd string
}

// CatalogCreateKafkaRequest creates a Kafka service.
type CatalogCreateKafkaRequest struct {
	Service  *manage.ServiceCommonRequest
	Resource *common.Resources
	Options  *CatalogKafkaOptions
}

// CatalogCreateKafkaResponse returns the Kafka JMX user and password.
type CatalogCreateKafkaResponse struct {
	JmxRemoteUser   string
	JmxRemotePasswd string
}

// CatalogKafkaSinkESOptions includes the options for Kafka ElasticSearch Sink Connect.
type CatalogKafkaSinkESOptions struct {
	Replicas int64

	// Kafka Connect JVM heap size
	HeapSizeMB int64

	// The Kafka service to sink from
	KafkaServiceName string

	// The Kafka Topic to read data from
	Topic string

	// The replication factor for storage, offset and status topics
	ReplFactor uint

	// The ElasticSearch service to sink to
	ESServiceName string

	// https://docs.confluent.io/3.1.0/connect/connect-elasticsearch/docs/configuration_options.html
	// MaxBufferedRecords and BatchSize should be adjusted according to the load.
	// The ElasticSearch connector default max.buffer.records is 20000, default batch.size is 2000.
	// For example, one record is 1KB. By default, will buffer 20MB, and batch 2MB.
	MaxBufferedRecords int
	BatchSize          int

	// The ElasticSearch index type. This could not be empty for kafka elasticsearch connector.
	// The "type" is deprecated in ElasticSearch 6.0.0.
	// https://www.elastic.co/guide/en/elasticsearch/reference/current/removal-of-types.html
	// https://www.elastic.co/guide/en/elasticsearch/reference/current/mapping-type-field.html
	// https://www.elastic.co/blog/index-vs-type
	TypeName string
}

// CatalogCreateKafkaSinkESRequest creates a Kafka ElasticSearch Sink Connect service.
type CatalogCreateKafkaSinkESRequest struct {
	Service  *manage.ServiceCommonRequest
	Resource *common.Resources
	Options  *CatalogKafkaSinkESOptions
}

// CatalogKafkaManagerOptions includes the options for Kafka Manager.
// Currently support 1 replica only.
type CatalogKafkaManagerOptions struct {
	// Kafka Manager JVM heap size
	HeapSizeMB int64

	// Kafka Manager user and password
	User     string
	Password string

	// The existing ZooKeeper service that Kafka Manager will use.
	ZkServiceName string
}

// CatalogCreateKafkaManagerRequest creates a Kafka Manager service.
type CatalogCreateKafkaManagerRequest struct {
	Service  *manage.ServiceCommonRequest
	Resource *common.Resources
	Options  *CatalogKafkaManagerOptions
}

// CatalogRedisOptions includes the config options for Redis.
type CatalogRedisOptions struct {
	// The number of shards for Redis cluster, minimal 3.
	// Setting to 1 will disable cluster mode. 2 is invalid.
	Shards           int64
	ReplicasPerShard int64

	// Redis memory cache size
	MemoryCacheSizeMB int64

	Volume *common.ServiceVolume

	// whether disable Redis "append only file", not recommended unless for cache only.
	DisableAOF bool
	// The AUTH password, recommended to set it for production environment. Empty string means disable it.
	AuthPass string
	// minimal 60s, not recommend to change unless necessary. see Redis Readme for details.
	ReplTimeoutSecs int64
	// how Redis will select what to remove when maxmemory is reached
	MaxMemPolicy string
	// rename the CONFIG command
	ConfigCmdName string
}

// CatalogCreateRedisRequest creates a Redis service.
type CatalogCreateRedisRequest struct {
	Service *manage.ServiceCommonRequest

	// Resources.MaxMemoryMB should always be set.
	// if cluster mode is enabled (Shards >= 3), the actual redis memory will be
	// Resources.MaxMemoryMB - default output buffer for the slaves (512MB).
	Resource *common.Resources

	Options *CatalogRedisOptions
}

// CatalogCouchDBOptions includes the config options for CouchDB.
type CatalogCouchDBOptions struct {
	Replicas int64
	Volume   *common.ServiceVolume

	// CouchDB admin username and password
	Admin       string
	AdminPasswd string

	// CouchDB Cors configs
	EnableCors  bool
	Credentials bool
	Origins     string
	Headers     string
	Methods     string

	// CouchDB SSL configs
	EnableSSL         bool
	CertFileContent   string
	KeyFileContent    string
	CACertFileContent string
}

// CatalogCreateCouchDBRequest creates a CouchDB service.
type CatalogCreateCouchDBRequest struct {
	Service  *manage.ServiceCommonRequest
	Resource *common.Resources
	Options  *CatalogCouchDBOptions
}

// CatalogConsulOptions includes the config options for Consul.
type CatalogConsulOptions struct {
	Replicas int64
	Volume   *common.ServiceVolume

	// https://www.consul.io/docs/agent/options.html#_datacenter
	// if not specified, use the current Region.
	Datacenter string

	// https://www.consul.io/docs/agent/options.html#_domain
	Domain string

	// https://www.consul.io/docs/agent/options.html#_encrypt
	// This key must be 16-bytes that are Base64-encoded.
	Encrypt string

	// TLS configs
	EnableTLS         bool
	CertFileContent   string
	KeyFileContent    string
	CACertFileContent string
	HTTPSPort         int64
}

// CatalogCreateConsulRequest creates a Consul service.
type CatalogCreateConsulRequest struct {
	Service  *manage.ServiceCommonRequest
	Resource *common.Resources
	Options  *CatalogConsulOptions
}

// CatalogCreateConsulResponse returns the consul server ips.
type CatalogCreateConsulResponse struct {
	ConsulServerIPs []string
}

// CatalogElasticSearchOptions includes the config options for ElasticSearch.
type CatalogElasticSearchOptions struct {
	Replicas int64
	Volume   *common.ServiceVolume

	// ElasticSearch JVM heap size
	HeapSizeMB int64

	DedicatedMasters       int64
	DisableDedicatedMaster bool
	DisableForceAwareness  bool
}

// CatalogCreateElasticSearchRequest creates an ElasticSearch service.
type CatalogCreateElasticSearchRequest struct {
	Service  *manage.ServiceCommonRequest
	Resource *common.Resources
	Options  *CatalogElasticSearchOptions
}

// CatalogKibanaOptions includes the config options for Kibana.
type CatalogKibanaOptions struct {
	Replicas int64
	Volume   *common.ServiceVolume

	// the ElasticSearch service that Kibana is used for
	ESServiceName string

	// specify a path to mount Kibana at if you are running behind a proxy.
	// This only affects the URLs generated by Kibana, your proxy is expected to remove the
	// basePath value before forwarding requests to Kibana. This setting cannot end in a slash (/).
	ProxyBasePath string

	EnableSSL bool
	SSLKey    string
	SSLCert   string
}

// CatalogCreateKibanaRequest creates an Kibana service.
type CatalogCreateKibanaRequest struct {
	Service  *manage.ServiceCommonRequest
	Resource *common.Resources
	Options  *CatalogKibanaOptions
}

// CatalogLogstashOptions includes the config options for Logstash.
type CatalogLogstashOptions struct {
	Replicas int64
	Volume   *common.ServiceVolume

	// Logstash JVM heap size
	HeapSizeMB int64

	// The container image for the service, such as cloudstax/firecamp-logstash:version or cloudstax/firecamp-logstash-input-couchdb:version
	ContainerImage string

	// The internal queue model: "memory" or "persisted"
	QueueType             string
	EnableDeadLetterQueue bool

	// The pipeline configs.
	// There is no need to have multiple config file, as Logstash currently has a single event pipeline.
	// All configuration files are just concatenated (in order) as if you had written a single flat file.
	PipelineConfigs string

	// https://www.elastic.co/guide/en/logstash/5.6/tuning-logstash.html
	// The pipeline settings
	PipelineWorkers       int
	PipelineOutputWorkers int
	PipelineBatchSize     int
	PipelineBatchDelay    int
}

// CatalogCreateLogstashRequest creates a Logstash service.
type CatalogCreateLogstashRequest struct {
	Service  *manage.ServiceCommonRequest
	Resource *common.Resources
	Options  *CatalogLogstashOptions
}

// CatalogTelegrafOptions defines the Telegraf creation options
type CatalogTelegrafOptions struct {
	CollectIntervalSecs int
	MonitorServiceName  string
	// The custom metrics to monitor. Leave it empty to use the default metrics for the service.
	MonitorMetrics string
}

// CatalogCreateTelegrafRequest creates a Telegraf service.
type CatalogCreateTelegrafRequest struct {
	Service  *manage.ServiceCommonRequest
	Resource *common.Resources
	Options  *CatalogTelegrafOptions
}

// CatalogCheckServiceInitRequest checks whether one catalog service is initialized.
type CatalogCheckServiceInitRequest struct {
	Service            *manage.ServiceCommonRequest
	CatalogServiceType string
}

// CatalogCheckServiceInitResponse returns the service init status
type CatalogCheckServiceInitResponse struct {
	Initialized   bool
	StatusMessage string
}

// CatalogSetServiceInitRequest sets the catalog service initialized.
// This is an internal request sent from the service's init task.
// It should not be called directly by the application.
type CatalogSetServiceInitRequest struct {
	Region      string
	Cluster     string
	ServiceName string
	ServiceType string
}

// CatalogSetRedisInitRequest sets the redis service initialized.
// This is an internal request sent from the service's init task.
// It should not be called directly by the application.
type CatalogSetRedisInitRequest struct {
	Region      string
	Cluster     string
	ServiceName string
}
