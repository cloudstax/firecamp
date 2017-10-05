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
	RunTaskOp            = SpecialOpPrefix + "Run-Task"
	GetTaskStatusOp      = SpecialOpPrefix + "Get-Task-Status"
	DeleteTaskOp         = SpecialOpPrefix + "Delete-Task"

	CatalogOpPrefix           = SpecialOpPrefix + "Catalog-"
	CatalogCreateMongoDBOp    = CatalogOpPrefix + "Create-MongoDB"
	CatalogCreatePostgreSQLOp = CatalogOpPrefix + "Create-PostgreSQL"
	CatalogCreateCassandraOp  = CatalogOpPrefix + "Create-Cassandra"
	CatalogCreateZooKeeperOp  = CatalogOpPrefix + "Create-ZooKeeper"
	CatalogCreateKafkaOp      = CatalogOpPrefix + "Create-Kafka"
	CatalogCreateRedisOp      = CatalogOpPrefix + "Create-Redis"
	CatalogCreateCouchDBOp    = CatalogOpPrefix + "Create-CouchDB"
	CatalogCheckServiceInitOp = CatalogOpPrefix + "Check-Service-Init"
	CatalogSetServiceInitOp   = CatalogOpPrefix + "Set-Service-Init"
	CatalogSetRedisInitOp     = CatalogOpPrefix + "Set-Redis-Init"

	InternalOpPrefix                 = SpecialOpPrefix + "Internal-"
	InternalGetServiceTaskOp         = InternalOpPrefix + "GetServiceTask"
	InternalListActiveServiceTasksOp = InternalOpPrefix + "ListActiveServiceTasks"

	// response headers
	RequestID       = "x-RequestId"
	Server          = "Server"
	ContentType     = "Content-Type"
	JsonContentType = "application/json"
)

// ReplicaConfig contains the required config files for one replica.
type ReplicaConfig struct {
	// The availability zone this replica should run.
	Zone string
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
	VolumeSizeGB   int64
	ContainerPath  string // The mount path inside container
	PortMappings   []common.PortMapping
	Envkvs         []*common.EnvKeyValuePair

	RegisterDNS     bool
	RequireStaticIP bool
	ReplicaConfigs  []*ReplicaConfig
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

// The integrated catalog services supported by the system.

// CatalogCreateMongoDBRequest creates a MongoDB ReplicaSet service.
type CatalogCreateMongoDBRequest struct {
	Service  *ServiceCommonRequest
	Resource *common.Resources

	Replicas     int64
	VolumeSizeGB int64

	Admin       string
	AdminPasswd string
}

// CatalogCreatePostgreSQLRequest creates a PostgreSQL service.
type CatalogCreatePostgreSQLRequest struct {
	Service  *ServiceCommonRequest
	Resource *common.Resources

	Replicas     int64
	VolumeSizeGB int64

	Admin          string
	AdminPasswd    string
	ReplUser       string
	ReplUserPasswd string
}

// CatalogCreateCassandraRequest creates a Cassandra service.
type CatalogCreateCassandraRequest struct {
	Service  *ServiceCommonRequest
	Resource *common.Resources

	Replicas     int64
	VolumeSizeGB int64
}

// CatalogCreateZooKeeperRequest creates a ZooKeeper service.
type CatalogCreateZooKeeperRequest struct {
	Service  *ServiceCommonRequest
	Resource *common.Resources

	Replicas     int64
	VolumeSizeGB int64
}

// CatalogCreateKafkaRequest creates a Kafka service.
type CatalogCreateKafkaRequest struct {
	Service  *ServiceCommonRequest
	Resource *common.Resources

	Replicas     int64
	VolumeSizeGB int64

	AllowTopicDel  bool
	RetentionHours int64
	// the existing ZooKeeper service that Kafka will use.
	ZkServiceName string
}

// CatalogRedisOptions includes the config options for Redis.
type CatalogRedisOptions struct {
	// The number of shards for Redis cluster, minimal 3.
	// Setting to 1 will disable cluster mode. 2 is invalid.
	Shards           int64
	ReplicasPerShard int64

	VolumeSizeGB int64

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

// CatalogCouchDBOptions includes the config options for CouchDB.
type CatalogCouchDBOptions struct {
	Replicas     int64
	VolumeSizeGB int64

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

// CatalogCheckServiceInitRequest checks whether one catalog service is initialized.
type CatalogCheckServiceInitRequest struct {
	ServiceType string

	Service *ServiceCommonRequest

	Admin       string
	AdminPasswd string

	// Redis specific fields.
	// TODO they should be moved to ServiceAttr.UserData.
	Shards           int64
	ReplicasPerShard int64
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
	// The Redis node ids, format: "MemberName RedisNodeID Role(master/slave)"
	NodeIds []string
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
