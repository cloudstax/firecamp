package containersvc

import (
	"strings"
	"sync"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
)

const keySep = ":"

type taskItem struct {
	service             string
	containerInstanceID string
}

type MemContainerSvc struct {
	mlock *sync.Mutex
	// key: cluster+service
	services map[string]bool
	// key: cluster+taskID
	servicetasks map[string]taskItem
	// key: taskID
	tasks map[string]bool
}

func NewMemContainerSvc() *MemContainerSvc {
	return &MemContainerSvc{
		mlock:        &sync.Mutex{},
		services:     map[string]bool{},
		servicetasks: map[string]taskItem{},
		tasks:        map[string]bool{},
	}
}

func (m *MemContainerSvc) GetContainerSvcType() string {
	return "MemContainerSvc"
}

func (m *MemContainerSvc) CreateService(ctx context.Context, opts *CreateServiceOptions) error {
	key := opts.Common.Cluster + opts.Common.ServiceName

	m.mlock.Lock()
	defer m.mlock.Unlock()

	_, ok := m.services[key]
	if ok {
		glog.Errorln("service exist", key)
		return common.ErrServiceExist
	}

	m.services[key] = true
	return nil
}

func (m *MemContainerSvc) IsServiceExist(ctx context.Context, cluster string, service string) (bool, error) {
	key := cluster + service

	m.mlock.Lock()
	defer m.mlock.Unlock()

	_, ok := m.services[key]
	return ok, nil
}

func (m *MemContainerSvc) ListActiveServiceTasks(ctx context.Context, cluster string, service string) (taskIDs map[string]bool, err error) {
	if strings.Contains(cluster, keySep) {
		glog.Errorln("invalid cluster name", cluster, "should not have", keySep)
		return nil, common.ErrInternal
	}

	m.mlock.Lock()
	defer m.mlock.Unlock()

	taskIDs = map[string]bool{}

	for key, t := range m.servicetasks {
		s := strings.SplitN(key, keySep, 2)
		if s[0] == cluster && t.service == service {
			glog.Infoln("taskID", s[1], "belongs to service", service, "cluster", cluster)
			taskIDs[s[1]] = true
		}
	}

	glog.Infoln("taskIDs", taskIDs, "belongs to service", service, "cluster", cluster)
	return taskIDs, nil
}

func (m *MemContainerSvc) GetServiceStatus(ctx context.Context, cluster string, service string) (*common.ServiceStatus, error) {
	taskIDs, err := m.ListActiveServiceTasks(ctx, cluster, service)
	if err != nil {
		return nil, err
	}

	count := int64(len(taskIDs))
	status := &common.ServiceStatus{
		RunningCount: count,
		DesiredCount: count,
	}
	return status, nil
}

func (m *MemContainerSvc) GetServiceTask(ctx context.Context, cluster string, service string, containerInstanceID string) (taskID string, err error) {
	if strings.Contains(cluster, keySep) {
		glog.Errorln("invalid cluster name", cluster, "should not have", keySep)
		return "", common.ErrInternal
	}

	m.mlock.Lock()
	defer m.mlock.Unlock()

	for key, t := range m.servicetasks {
		s := strings.SplitN(key, keySep, 2)
		if s[0] == cluster && t.service == service && t.containerInstanceID == containerInstanceID {
			if len(taskID) != 0 {
				glog.Errorln("more than 1 tasks", taskID, s[1], "for container instance",
					containerInstanceID, "service", service, "cluster", cluster)
				return "", ErrContainerSvcTooManyTasks
			}

			taskID = s[1]
			glog.Infoln("get task", taskID, "for container instance", containerInstanceID,
				"service", service, "cluster", cluster)
		}
	}

	if len(taskID) == 0 {
		glog.Errorln("no task for container instance", containerInstanceID,
			"service", service, "cluster", cluster)
		return "", ErrContainerSvcNoTask
	}

	return taskID, nil
}

func (m *MemContainerSvc) AddServiceTask(ctx context.Context, cluster string, service string, taskID string, containerInstanceID string) error {
	if strings.Contains(cluster, keySep) {
		glog.Errorln("invalid cluster name", cluster, "should not have", keySep)
		return common.ErrInternal
	}

	key := cluster + keySep + taskID

	m.mlock.Lock()
	defer m.mlock.Unlock()

	_, ok := m.servicetasks[key]
	if ok {
		glog.Errorln("task exists", taskID)
		return common.ErrInternal
	}

	taskItem := taskItem{
		service:             service,
		containerInstanceID: containerInstanceID,
	}

	m.servicetasks[key] = taskItem

	glog.Infoln("added task", taskItem)
	return nil
}

func (m *MemContainerSvc) UpdateService(ctx context.Context, opts *UpdateServiceOptions) error {
	return nil
}

func (m *MemContainerSvc) StopService(ctx context.Context, cluster string, service string) error {
	return nil
}

func (m *MemContainerSvc) ScaleService(ctx context.Context, cluster string, service string, desiredCount int64) error {
	return nil
}

func (m *MemContainerSvc) RollingRestartService(ctx context.Context, cluster string, service string, opts *RollingRestartOptions) error {
	return nil
}

func (m *MemContainerSvc) DeleteService(ctx context.Context, cluster string, service string) error {
	key := cluster + service

	m.mlock.Lock()
	defer m.mlock.Unlock()

	delete(m.services, key)
	return nil
}

func (m *MemContainerSvc) RunTask(ctx context.Context, opts *RunTaskOptions) (taskID string, err error) {
	taskID = opts.Common.Cluster + opts.Common.ServiceName + opts.TaskType

	m.mlock.Lock()
	defer m.mlock.Unlock()

	_, ok := m.tasks[taskID]
	if ok {
		glog.Infoln("task is already running")
		return taskID, nil
	}

	m.tasks[taskID] = true
	return taskID, nil
}

func (m *MemContainerSvc) GetTaskStatus(ctx context.Context, cluster string, taskID string) (*common.TaskStatus, error) {
	m.mlock.Lock()
	defer m.mlock.Unlock()

	_, ok := m.tasks[taskID]
	if !ok {
		glog.Infoln("task not exist")
		return nil, common.ErrNotFound
	}

	status := &common.TaskStatus{
		Status:        "running",
		StoppedReason: "",
		StartedAt:     "",
		FinishedAt:    "",
	}
	return status, nil
}

func (m *MemContainerSvc) DeleteTask(ctx context.Context, cluster string, service string, taskType string) error {
	taskID := cluster + service + taskType

	m.mlock.Lock()
	defer m.mlock.Unlock()

	delete(m.tasks, taskID)
	return nil
}

func (m *MemContainerSvc) CreateServiceVolume(ctx context.Context, service string, memberIndex int64, volumeID string, volumeSizeGB int64, journal bool) (existingVolumeID string, err error) {
	return "", nil
}

func (m *MemContainerSvc) DeleteServiceVolume(ctx context.Context, service string, memberIndex int64, journal bool) error {
	return nil
}
