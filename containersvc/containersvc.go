package containersvc

import (
	"errors"

	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/log"
)

const (
	// The journal volume name prefix, the journal volume name will be journal-serviceuuid,
	// the mount path will be /mnt/journal-serviceuuid
	JournalVolumeNamePrefix = "journal"

	// TestBusyBoxContainerImage is the busybox image for unit test.
	TestBusyBoxContainerImage = common.OrgName + common.SystemName + "-busybox"
)

var (
	ErrContainerSvcTooManyTasks   = errors.New("The service has more than one tasks on the same ContainerInstance")
	ErrContainerSvcNoTask         = errors.New("The service has no task on the ContainerInstance")
	ErrInvalidContainerInstanceID = errors.New("InvalidContainerInstanceID")
	ErrInvalidCluster             = errors.New("InvalidCluster")
	ErrVolumeExist                = errors.New("Volume Exists")
)

type CommonOptions struct {
	Cluster        string
	ServiceName    string
	ServiceUUID    string
	ServiceType    string // stateful or stateless
	ContainerImage string
	Resource       *common.Resources
	LogConfig      *cloudlog.LogConfig
}

type VolumeOptions struct {
	MountPath  string
	VolumeType string
	SizeGB     int64
	Iops       int64
	Encrypted  bool
}

type Placement struct {
	Zones []string
}

type CreateServiceOptions struct {
	Replicas int64
	Common   *CommonOptions

	// PortMappings include the ports exposed by the services. It includes a "ServicePort" field
	// for whether the port is a service port, used by k8s headless service for statefulset.
	// https://kubernetes.io/docs/tutorials/stateful-application/basic-stateful-set/#creating-a-statefulset
	// https://kubernetes.io/docs/concepts/services-networking/service/#headless-services
	// https://kubernetes.io/docs/concepts/services-networking/service/#multi-port-services
	PortMappings []common.PortMapping
	// the placement constraints. If not specified, spread to all zones.
	Place  *Placement
	Envkvs []*common.EnvKeyValuePair

	// The volume mount for the service data, must exist.
	DataVolume *VolumeOptions
	// The volume mount for the service journal, optional.
	JournalVolume *VolumeOptions

	// Whether uses external DNS. For example, set to true if connect with AWS Route53.
	// If only use within k8s, set to false.
	ExternalDNS bool
	// Whether uses the external static IP. This is only for the services that require static ip, such as Redis, Consul.
	ExternalStaticIP bool
}

type UpdateServiceOptions struct {
	Cluster     string
	ServiceName string
	// update cpu and memory limits
	MaxCPUUnits     *int64
	ReserveCPUUnits *int64
	MaxMemMB        *int64
	ReserveMemMB    *int64
	// update port mappings
	PortMappings []common.PortMapping
	// Whether uses external DNS. For example, set to true if connect with AWS Route53.
	// If only use within k8s, set to false.
	ExternalDNS bool
	// update the release version, such as 0.9.5. empty means no change.
	ReleaseVersion string
}

type RollingRestartOptions struct {
	Replicas int64
	// serviceTasks is a list of tasks for ECS, a list of pods for K8s.
	// swarm currently does not need it.
	ServiceTasks  []string
	StatusMessage string
}

type RunTaskOptions struct {
	Common   *CommonOptions
	TaskType string
	Envkvs   []*common.EnvKeyValuePair
}

// ContainerSvc defines the cluster, service and task related functions
type ContainerSvc interface {
	// GetContainerSvcType gets the containersvc type, such as ecs, swarm, k8s.
	GetContainerSvcType() string

	// IsServiceExist checks if service exists. If not exist, return false & nil. If exists, return true & nil.
	// If meets any error, error will be returned.
	IsServiceExist(ctx context.Context, cluster string, service string) (bool, error)
	CreateService(ctx context.Context, opts *CreateServiceOptions) error
	UpdateService(ctx context.Context, opts *UpdateServiceOptions) error
	GetServiceStatus(ctx context.Context, cluster string, service string) (*common.ServiceStatus, error)
	// StopService stops the service on the container platform, and waits till all containers are stopped.
	// Expect no error (nil) if service is already stopped or does not exist.
	StopService(ctx context.Context, cluster string, service string) error
	// ScaleService scales the service containers up/down to the desiredCount. Note: it does not wait till all containers are started or stopped.
	ScaleService(ctx context.Context, cluster string, service string, desiredCount int64) error
	// DeleteService deletes the service on the container platform.
	// Expect no error (nil) if service does not exist.
	DeleteService(ctx context.Context, cluster string, service string) error
	// RollingRestartService restarts the tasks one after the other.
	RollingRestartService(ctx context.Context, cluster string, service string, opts *RollingRestartOptions) error
	// List the active (pending and running) tasks of the service.
	ListActiveServiceTasks(ctx context.Context, cluster string, service string) (serviceTaskIDs map[string]bool, err error)
	GetServiceTask(ctx context.Context, cluster string, service string, containerInstanceID string) (serviceTaskID string, err error)

	// One node (EC2) could crash at any time. So the task needs to be reentrant.
	// The container orchestration framework, such as ECS, usually does not retry the
	// task automatically, when a taks fails. The caller needs to check the task status
	// and decide whether to retry.
	// It may not be easy to know the task failure reason clearly.
	// For example, ECS task will reach the stopped status at many conditions,
	// http://docs.aws.amazon.com/AmazonECS/latest/developerguide/task_life_cycle.html.
	// The Task.StoppedReason records the detail reason,
	// http://docs.aws.amazon.com/AmazonECS/latest/developerguide/troubleshooting.html#stopped-task-errors.
	//
	// The caller could check whether a task succeeds or not by checking
	// both TaskStatus.Status and TaskStatus.StoppedReason. This does not sounds the best
	// option. It would be better that ECS could add the explicit task failure status.
	//
	// The other option is the task updates the status somewhere at the end,
	// and the caller could check that status to decide whether the task needs to be retried.
	// For example, the MongoDB init task will set the service initialized in the control plane.
	RunTask(ctx context.Context, opts *RunTaskOptions) (taskID string, err error)
	GetTaskStatus(ctx context.Context, cluster string, taskID string) (*common.TaskStatus, error)
	DeleteTask(ctx context.Context, cluster string, service string, taskType string) error

	// Create the service volume. This is only useful for k8s to create PV and PVC. And simply non-op for ECS and Swarm.
	CreateServiceVolume(ctx context.Context, service string, memberIndex int64, volumeID string, volumeSizeGB int64, journal bool) (existingVolumeID string, err error)
	DeleteServiceVolume(ctx context.Context, service string, memberIndex int64, journal bool) error
}

// Info defines the operations for the container related info
type Info interface {
	GetLocalContainerInstanceID() string
	GetContainerClusterID() string
}
