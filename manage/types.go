package manage

import (
	"github.com/openconnectio/openmanage/common"
)

const (
	// special service operations
	SpecialOpPrefix      = "?"
	ListServiceOp        = SpecialOpPrefix + "List-Service"
	GetServiceStatusOp   = SpecialOpPrefix + "Get-Service-Status"
	ServiceInitializedOp = SpecialOpPrefix + "Set-Service-Initialized"
	RunTaskOp            = SpecialOpPrefix + "Run-Task"
	GetTaskStatusOp      = SpecialOpPrefix + "Get-Task-Status"
	DeleteTaskOp         = SpecialOpPrefix + "Delete-Task"

	// response headers
	RequestID       = "x-RequestId"
	Server          = "Server"
	ContentType     = "Content-Type"
	JsonContentType = "application/json"
)

// ReplicaConfig contains the required config files for one replica
type ReplicaConfig struct {
	Configs []*ReplicaConfigFile
}

// ReplicaConfigFile contains the detail config file name and content
type ReplicaConfigFile struct {
	FileName string
	Content  string
}

// ServiceCommonRequest contains the common service parameters.
// region, az and cluster are for the management service to verify
// the request is sent to the correct server
type ServiceCommonRequest struct {
	Region      string
	Zone        string
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
	Port           int64
	Envkvs         []*common.EnvKeyValuePair

	HasMembership  bool
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

// ListServiceRequest lists the services according to the filter.
// TODO change "Prefix" to the common filter.
type ListServiceRequest struct {
	// region, az and cluster are for the management service to verify
	// the request is sent to the correct server
	Region  string
	Zone    string
	Cluster string

	// The prefix of the service name
	Prefix string
}

// ListServiceResponse returns all listed services' attributes.
type ListServiceResponse struct {
	Services []*common.ServiceAttr
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
