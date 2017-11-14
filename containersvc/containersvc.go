package containersvc

import (
	"errors"

	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/log"
)

const (
	// The busybox image for unit test.
	TestBusyBoxContainerImage = common.OrgName + "/" + common.SystemName + "-busybox"
)

var (
	ErrContainerSvcTooManyTasks   = errors.New("The service has more than one tasks on the same ContainerInstance")
	ErrContainerSvcNoTask         = errors.New("The service has no task on the ContainerInstance")
	ErrInvalidContainerInstanceID = errors.New("InvalidContainerInstanceID")
	ErrInvalidCluster             = errors.New("InvalidCluster")
)

type CommonOptions struct {
	Cluster        string
	ServiceName    string
	ServiceUUID    string
	ContainerImage string
	Resource       *common.Resources
	LogConfig      *cloudlog.LogConfig
}

type Placement struct {
	Zones []string
}

type CreateServiceOptions struct {
	Common           *CommonOptions
	ContainerPath    string // The mount path inside container for the service data
	JournalContainerPath string // The mount path inside container for the service journal
	PortMappings     []common.PortMapping
	Replicas         int64
	// the placement constraints. If not specified, spread to all zones.
	Place  *Placement
	Envkvs []*common.EnvKeyValuePair
}

type RunTaskOptions struct {
	Common   *CommonOptions
	TaskType string
	Envkvs   []*common.EnvKeyValuePair
}

// ContainerSvc defines the cluster, service and task related functions
type ContainerSvc interface {
	// IsServiceExist checks if service exists. If not exist, return false & nil. If exists, return true & nil.
	// If meets any error, error will be returned.
	IsServiceExist(ctx context.Context, cluster string, service string) (bool, error)
	CreateService(ctx context.Context, opts *CreateServiceOptions) error
	GetServiceStatus(ctx context.Context, cluster string, service string) (*common.ServiceStatus, error)
	// StopService stops the service on the container platform, and waits till all containers are stopped.
	// Expect no error (nil) if service is already stopped or does not exist.
	StopService(ctx context.Context, cluster string, service string) error
	RestartService(ctx context.Context, cluster string, service string, desiredCount int64) error
	// DeleteService deletes the service on the container platform.
	// Expect no error (nil) if service does not exist.
	DeleteService(ctx context.Context, cluster string, service string) error
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
}

// Info defines the operations for the container related info
type Info interface {
	GetLocalContainerInstanceID() string
	GetContainerClusterID() string
}
