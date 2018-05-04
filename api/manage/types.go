package manage

import (
	"github.com/cloudstax/firecamp/common"
)

const (
	// special service operations
	SpecialOpPrefix        = "?"
	ListServiceOp          = SpecialOpPrefix + "List-Service"
	ListServiceMemberOp    = SpecialOpPrefix + "List-ServiceMember"
	GetServiceConfigFileOp = SpecialOpPrefix + "Get-Service-ConfigFile"
	GetMemberConfigFileOp  = SpecialOpPrefix + "Get-Member-ConfigFile"
	GetServiceStatusOp     = SpecialOpPrefix + "Get-Service-Status"
	ServiceInitializedOp   = SpecialOpPrefix + "Set-Service-Initialized"

	CreateServiceOp          = SpecialOpPrefix + "Create-Service"
	CreateManageServiceOp    = SpecialOpPrefix + "Create-Manage-Service"
	CreateContainerServiceOp = SpecialOpPrefix + "Create-Container-Service"
	UpdateServiceConfigOp    = SpecialOpPrefix + "Update-Service-Config"
	UpdateServiceResourceOp  = SpecialOpPrefix + "Update-Service-Resource"
	UpdateMemberConfigOp     = SpecialOpPrefix + "Update-Member-Config"
	StopServiceOp            = SpecialOpPrefix + "Stop-Service"
	StartServiceOp           = SpecialOpPrefix + "Start-Service"
	DeleteServiceOp          = SpecialOpPrefix + "Delete-Service"
	ScaleServiceOp           = SpecialOpPrefix + "Scale-Service"
	UpgradeServiceOp         = SpecialOpPrefix + "Upgrade-Service"
	RunTaskOp                = SpecialOpPrefix + "Run-Task"
	GetTaskStatusOp          = SpecialOpPrefix + "Get-Task-Status"
	DeleteTaskOp             = SpecialOpPrefix + "Delete-Task"
	// The service related management task
	RollingRestartServiceOp = SpecialOpPrefix + "RollingRestart-Service"
	GetServiceTaskStatusOp  = SpecialOpPrefix + "Get-ServiceTask-Status"

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
	Configs []*ConfigFileContent
}

// ConfigFileContent contains the detail config file name and content.
type ConfigFileContent struct {
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

	// ServiceType: stateful or stateless. default: stateful.
	ServiceType string

	// Catalog Service, such as Cassandra, Kafka, etc.
	CatalogServiceType string

	ContainerImage string
	Replicas       int64
	PortMappings   []common.PortMapping
	Envkvs         []*common.EnvKeyValuePair

	// whether need to register DNS
	RegisterDNS bool

	// Whether the service requires static ip
	RequireStaticIP bool

	ServiceConfigs []*ConfigFileContent

	// Below fields are used by the stateful service only.

	// The primary volume for the service data
	Volume *common.ServiceVolume
	// The journal volume for the service journal
	JournalVolume *common.ServiceVolume
	// TODO remove ContainerPath, as the docker entrypoint script simply uses the default path
	ContainerPath        string // The mount path inside container for the primary volume
	JournalContainerPath string // The mount path inside container for the journal volume

	// The detail configs for each replica
	ReplicaConfigs []*ReplicaConfig
}

// CreateServiceResponse returns the service uuid
type CreateServiceResponse struct {
	ServiceUUID string
}

// UpdateServiceConfigRequest updates the config file of the service
type UpdateServiceConfigRequest struct {
	Service           *ServiceCommonRequest
	ConfigFileName    string
	ConfigFileContent string
}

// ScaleServiceRequest scales the service.
// TODO only support scale out now.
type ScaleServiceRequest struct {
	Service *ServiceCommonRequest
	// The detail configs for each service member. Note: currently ReplicaConfigs
	// have the configs of all service members, including the existing members.
	ReplicaConfigs []*ReplicaConfig
}

// UpdateServiceResourceRequest updates the service resource.
type UpdateServiceResourceRequest struct {
	Service         *ServiceCommonRequest
	MaxCPUUnits     *int64
	ReserveCPUUnits *int64
	MaxMemMB        *int64
	ReserveMemMB    *int64
}

// UpdateMemberConfigRequest updates the config file of the service member
type UpdateMemberConfigRequest struct {
	Service           *ServiceCommonRequest
	MemberName        string
	ConfigFileName    string
	ConfigFileContent string
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

// GetServiceConfigFileRequest gets one service config file.
type GetServiceConfigFileRequest struct {
	Service        *ServiceCommonRequest
	ConfigFileName string
}

// GetMemberConfigFileRequest gets the config file of one service member.
type GetMemberConfigFileRequest struct {
	Service        *ServiceCommonRequest
	MemberName     string
	ConfigFileName string
}

// GetConfigFileResponse returns the config file.
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
