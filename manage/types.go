package manage

import (
	"github.com/cloudstax/firecamp/common"
)

const (
	// special service operations
	SpecialOpPrefix      = "?"
	ListServiceOp        = SpecialOpPrefix + "List-Service"
	ListServiceMemberOp  = SpecialOpPrefix + "List-ServiceMember"
	GetConfigFileOp      = SpecialOpPrefix + "Get-Config-File"
	GetServiceStatusOp   = SpecialOpPrefix + "Get-Service-Status"
	ServiceInitializedOp = SpecialOpPrefix + "Set-Service-Initialized"
	StopServiceOp        = SpecialOpPrefix + "Stop-Service"
	StartServiceOp       = SpecialOpPrefix + "Start-Service"
	DeleteServiceOp      = SpecialOpPrefix + "Delete-Service"
	RunTaskOp            = SpecialOpPrefix + "Run-Task"
	GetTaskStatusOp      = SpecialOpPrefix + "Get-Task-Status"
	DeleteTaskOp         = SpecialOpPrefix + "Delete-Task"
	// The service related management task
	RollingRestartServiceOp = SpecialOpPrefix + "RollingRestart-Service"
	GetServiceTaskStatusOp  = SpecialOpPrefix + "Get-ServiceTask-Status"

	CatalogOpPrefix              = SpecialOpPrefix + "Catalog-"
	CatalogCreateMongoDBOp       = CatalogOpPrefix + "Create-MongoDB"
	CatalogCreatePostgreSQLOp    = CatalogOpPrefix + "Create-PostgreSQL"
	CatalogCreateCassandraOp     = CatalogOpPrefix + "Create-Cassandra"
	CatalogCreateZooKeeperOp     = CatalogOpPrefix + "Create-ZooKeeper"
	CatalogCreateKafkaOp         = CatalogOpPrefix + "Create-Kafka"
	CatalogCreateKafkaManagerOp  = CatalogOpPrefix + "Create-Kafka-Manager"
	CatalogCreateRedisOp         = CatalogOpPrefix + "Create-Redis"
	CatalogCreateCouchDBOp       = CatalogOpPrefix + "Create-CouchDB"
	CatalogCreateConsulOp        = CatalogOpPrefix + "Create-Consul"
	CatalogCreateElasticSearchOp = CatalogOpPrefix + "Create-ElasticSearch"
	CatalogCreateKibanaOp        = CatalogOpPrefix + "Create-Kibana"
	CatalogCreateLogstashOp      = CatalogOpPrefix + "Create-Logstash"
	CatalogCheckServiceInitOp    = CatalogOpPrefix + "Check-Service-Init"
	CatalogSetServiceInitOp      = CatalogOpPrefix + "Set-Service-Init"
	CatalogSetRedisInitOp        = CatalogOpPrefix + "Set-Redis-Init"
	CatalogUpdateCassandraOp     = CatalogOpPrefix + "Update-Cassandra"
	CatalogScaleCassandraOp      = CatalogOpPrefix + "Scale-Cassandra"
	CatalogUpdateRedisOp         = CatalogOpPrefix + "Update-Redis"
	CatalogUpdateKafkaOp         = CatalogOpPrefix + "Update-Kafka"

	InternalOpPrefix                 = SpecialOpPrefix + "Internal-"
	InternalGetServiceTaskOp         = InternalOpPrefix + "GetServiceTask"
	InternalListActiveServiceTasksOp = InternalOpPrefix + "ListActiveServiceTasks"

	// response headers
	RequestID       = "x-RequestId"
	Server          = "Server"
	ContentType     = "Content-Type"
	JsonContentType = "application/json"
)

// ReplicaConfig contains the required config files for one replica (member).
type ReplicaConfig struct {
	// The availability zone this replica should run.
	Zone string
	// The replica's member name.
	MemberName string
	// The detail config files for this replica.
	Configs []*ReplicaConfigFile
}

// ReplicaConfigFile contains the detail config file name and content.
type ReplicaConfigFile struct {
	FileName string
	FileMode uint32
	Content  string
}

// ServiceCommonRequest contains the common service parameters.
// region and cluster are for the management service to verify
// the request is sent to the correct server.
type ServiceCommonRequest struct {
	Region      string
	Cluster     string
	ServiceName string
	// ServiceType: stateful or stateless. default: stateful.
	// The empty string means stateful as this field is added after 0.9.3.
	ServiceType string
}

// CreateServiceRequest contains the parameters for creating a service.
// Currently every replica should have its own ReplicaConfig. This aims to
// provide the flexibility for different services.
// CreateService simply returns success or not.
type CreateServiceRequest struct {
	Service  *ServiceCommonRequest
	Resource *common.Resources

	ContainerImage string
	Replicas       int64
	PortMappings   []common.PortMapping
	Envkvs         []*common.EnvKeyValuePair

	// whether need to register DNS
	RegisterDNS bool

	// The service's custom attributes
	UserAttr *common.ServiceUserAttr

	// Below fields are used by the stateful service only.

	// The primary volume for the service data
	Volume *common.ServiceVolume
	// The journal volume for the service journal
	JournalVolume *common.ServiceVolume
	// TODO remove ContainerPath, as the docker entrypoint script simply uses the default path
	ContainerPath        string // The mount path inside container for the primary volume
	JournalContainerPath string // The mount path inside container for the journal volume

	// Whether the service requires static ip
	RequireStaticIP bool

	// The detail configs for each replica
	ReplicaConfigs []*ReplicaConfig
}

// GetServiceAttributesResponse returns the service's attributes.
// GetServiceAttributesRequest just sends a "GET" with ServiceCommonRequest.
type GetServiceAttributesResponse struct {
	Service *common.ServiceAttr
}

// GetServiceStatusResponse returns the service's status.
// GetServiceStatusRequest just sends a "GET" with ServiceCommonRequest.
type GetServiceStatusResponse struct {
	Status *common.ServiceStatus
}

// ListServiceMemberRequest lists the serviceMembers of one service
type ListServiceMemberRequest struct {
	Service *ServiceCommonRequest
}

// ListServiceMemberResponse returns the serviceMembers of one service
type ListServiceMemberResponse struct {
	ServiceMembers []*common.ServiceMember
}

// ListServiceRequest lists the services according to the filter.
// TODO change "Prefix" to the common filter.
type ListServiceRequest struct {
	// region, az and cluster are for the management service to verify
	// the request is sent to the correct server
	Region  string
	Cluster string

	// The prefix of the service name
	Prefix string
}

// ListServiceResponse returns all listed services' attributes.
type ListServiceResponse struct {
	Services []*common.ServiceAttr
}

// DeleteServiceRequest deletes the service.
type DeleteServiceRequest struct {
	Service *ServiceCommonRequest
}

// DeleteServiceResponse returns the volumes of the service.
type DeleteServiceResponse struct {
	VolumeIDs []string
}

// GetConfigFileRequest gets one config file.
type GetConfigFileRequest struct {
	Region      string
	Cluster     string
	ServiceUUID string
	FileID      string
}

// GetConfigFileResponse rturns the config file.
type GetConfigFileResponse struct {
	ConfigFile *common.ConfigFile
}

// RunTaskRequest contains the parameters to run a task.
type RunTaskRequest struct {
	Service        *ServiceCommonRequest
	Resource       *common.Resources
	ContainerImage string
	TaskType       string
	Envkvs         []*common.EnvKeyValuePair
}

// RunTaskResponse returns the TaskID for the task.
type RunTaskResponse struct {
	TaskID string
}

// GetTaskStatusRequest gets the task status for the task of one service.
type GetTaskStatusRequest struct {
	Service *ServiceCommonRequest
	TaskID  string
}

// GetTaskStatusResponse returns the task status.
type GetTaskStatusResponse struct {
	Status *common.TaskStatus
}

// DeleteTaskRequest deletes the service task.
type DeleteTaskRequest struct {
	Service  *ServiceCommonRequest
	TaskType string
}

// Service management task related requests

// GetServiceTaskStatusResponse returns the service management task status.
type GetServiceTaskStatusResponse struct {
	Complete      bool
	StatusMessage string
}

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
	Service  *ServiceCommonRequest
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
	Service  *ServiceCommonRequest
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
	Service  *ServiceCommonRequest
	Resource *common.Resources
	Options  *CatalogCassandraOptions
}

// CatalogCreateCassandraResponse returns the Cassandra JMX user and password.
type CatalogCreateCassandraResponse struct {
	JmxRemoteUser   string
	JmxRemotePasswd string
}

// CatalogUpdateCassandraRequest updates the configs of the Cassandra service.
type CatalogUpdateCassandraRequest struct {
	Service    *ServiceCommonRequest
	HeapSizeMB int64 // 0 means no change
	// empty JmxRemoteUser means no change, jmx user could not be disabled.
	// JmxRemotePasswd could not be empty if JmxRemoteUser is set.
	JmxRemoteUser   string
	JmxRemotePasswd string
}

// CatalogScaleCassandraRequest scales the Cassandra service.
type CatalogScaleCassandraRequest struct {
	Service  *ServiceCommonRequest
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
	Service  *ServiceCommonRequest
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
	Service  *ServiceCommonRequest
	Resource *common.Resources
	Options  *CatalogKafkaOptions
}

// CatalogCreateKafkaResponse returns the Kafka JMX user and password.
type CatalogCreateKafkaResponse struct {
	JmxRemoteUser   string
	JmxRemotePasswd string
}

// CatalogUpdateKafkaRequest updates the configs of the Kafka service.
type CatalogUpdateKafkaRequest struct {
	Service        *ServiceCommonRequest
	HeapSizeMB     int64 // 0 == no change
	AllowTopicDel  *bool // nil == no change
	RetentionHours int64 // 0 == no change
	// empty JmxRemoteUser means no change, jmx user could not be disabled.
	// JmxRemotePasswd could not be empty if JmxRemoteUser is set.
	JmxRemoteUser   string
	JmxRemotePasswd string
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
	Service  *ServiceCommonRequest
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
	Service *ServiceCommonRequest

	// Resources.MaxMemoryMB should always be set.
	// if cluster mode is enabled (Shards >= 3), the actual redis memory will be
	// Resources.MaxMemoryMB - default output buffer for the slaves (512MB).
	Resource *common.Resources

	Options *CatalogRedisOptions
}

// CatalogUpdateRedisRequest updates the configs of the Redis service.
// If the field is not changed, please leave it 0 or empty.
type CatalogUpdateRedisRequest struct {
	Service           *ServiceCommonRequest
	MemoryCacheSizeMB int64 // 0 == no change
	// empty == no change. if auth is enabled, it could not be disabled.
	AuthPass        string
	ReplTimeoutSecs int64  // 0 == no change
	MaxMemPolicy    string // empty == no change
	// nil == no change. to disable config cmd, set empty string
	ConfigCmdName *string
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
	Service  *ServiceCommonRequest
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
	Service  *ServiceCommonRequest
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
	Service  *ServiceCommonRequest
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
	Service  *ServiceCommonRequest
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
	Service  *ServiceCommonRequest
	Resource *common.Resources
	Options  *CatalogLogstashOptions
}

// CatalogCheckServiceInitRequest checks whether one catalog service is initialized.
type CatalogCheckServiceInitRequest struct {
	ServiceType string

	Service *ServiceCommonRequest

	Admin       string
	AdminPasswd string
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

// The internal requests from the plugin to the manage server.

// InternalGetServiceTaskRequest gets the service task from the container framework.
type InternalGetServiceTaskRequest struct {
	Region              string
	Cluster             string
	ServiceName         string
	ContainerInstanceID string
}

// InternalGetServiceTaskResponse returns the service task ID.
type InternalGetServiceTaskResponse struct {
	ServiceTaskID string
}

// InternalListActiveServiceTasksRequest gets the service active tasks from the container framework.
type InternalListActiveServiceTasksRequest struct {
	Region      string
	Cluster     string
	ServiceName string
}

// InternalListActiveServiceTasksResponse returns the active task IDs of the service.
type InternalListActiveServiceTasksResponse struct {
	ServiceTaskIDs map[string]bool
}
