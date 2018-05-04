package catalogsvc

import (
	"fmt"
	"sync"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/manage"
	manageclient "github.com/cloudstax/firecamp/api/manage/client"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/utils"
)

const (
	// Wait some time for service to stabilize before running the init task.
	waitSecondsBeforeInit = time.Duration(10) * time.Second

	// Task status messages
	waitServiceRunningInitialMsg = "wait the service containers running"
	waitServiceRunningMsg        = "wait the service containers running, RunningCount %d"
	serviceRunningMsg            = "all service containers are running, wait to run the init task"
	startInitTaskMsg             = "the init task is running"
)

type serviceTask struct {
	serviceName string
	// The task detail
	opts *containersvc.RunTaskOptions
	// The task status message
	statusMessage string
}

// catalogServiceInit serves and tracks the service init tasks.
// Every task will have its own goroutine to run.
// Currently only the init task is supported. Revisit if different/multiple
// tasks of one service are allowed in the future.
type catalogServiceInit struct {
	region    string
	cluster   string
	managecli *manageclient.ManageClient

	// The task map to track whether the service has task running. key: serviceName
	tasks map[string]*serviceTask
	tlock *sync.Mutex
}

func newCatalogServiceInit(region string, cluster string, managecli *manageclient.ManageClient) *catalogServiceInit {
	c := &catalogServiceInit{
		region:    region,
		cluster:   cluster,
		managecli: managecli,
		tasks:     make(map[string]*serviceTask),
		tlock:     &sync.Mutex{},
	}

	return c
}

func (c *catalogServiceInit) hasInitTask(ctx context.Context, serviceName string) (hasTask bool, statusMsg string) {
	c.tlock.Lock()
	defer c.tlock.Unlock()

	task, ok := c.tasks[serviceName]
	if ok {
		statusMsg = task.statusMessage
	}
	return ok, statusMsg
}

func (c *catalogServiceInit) addInitTask(ctx context.Context, task *serviceTask) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// the init task will be handled in the background. ctx cancel will be called
	// when the request is returned. create a new context for the init task.
	initCtx := utils.NewRequestContext(context.Background(), requuid)

	c.tlock.Lock()
	defer c.tlock.Unlock()

	// check if service has the same task running
	_, ok := c.tasks[task.serviceName]
	if ok {
		glog.Infoln("service task is already running", task, "requuid", requuid)
		return nil
	}

	if len(c.tasks) > maxTaskCounts {
		glog.Errorln("exceed maxTaskCounts", maxTaskCounts, "could not run task", task, "requuid", requuid)
		return common.ErrInternal
	}

	// service does not have running task, run it
	task.statusMessage = waitServiceRunningInitialMsg
	c.tasks[task.serviceName] = task
	go c.runInitTask(initCtx, task, requuid)

	glog.Infoln("add init task", task, "requuid", requuid)
	return nil
}

func (c *catalogServiceInit) runInitTask(ctx context.Context, task *serviceTask, requuid string) {
	// remove the task from map when task is done
	defer c.taskDone(ctx, task, requuid)

	// wait till all service containers are running
	err := c.waitServiceRunning(ctx, task, requuid)
	if err != nil {
		return
	}

	// wait some time for service to stabilize
	time.Sleep(waitSecondsBeforeInit)

	// check if service is already initialized
	initialized, err := c.isServiceInitialized(ctx, task, requuid)
	if err != nil {
		return
	}
	if initialized {
		glog.Infoln("service is already initialized, requuid", requuid, task)
		return
	}

	task.statusMessage = startInitTaskMsg

	req := &manage.RunTaskRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      c.region,
			Cluster:     c.cluster,
			ServiceName: task.serviceName,
		},
		Resource:       task.opts.Common.Resource,
		ContainerImage: task.opts.Common.ContainerImage,
		TaskType:       task.opts.TaskType,
		Envkvs:         task.opts.Envkvs,
	}

	for i := 0; i < common.DefaultTaskRetryCounts; i++ {
		// run task
		taskID, err := c.managecli.RunTask(ctx, req)
		if err != nil {
			glog.Errorln("RunTask error", err, "requuid", requuid, task)
			return
		}

		// wait task done
		c.waitTask(ctx, taskID, task, requuid)

		// check if service is initialized
		initialized, err = c.isServiceInitialized(ctx, task, requuid)
		if err == nil && initialized {
			glog.Infoln("service is initialized, requuid", requuid, task)
			return
		}
		glog.Infoln("service is not initialized yet, requuid", requuid, task)
	}
}

func (c *catalogServiceInit) taskDone(ctx context.Context, task *serviceTask, requuid string) {
	// delete the task from the container platform
	req := &manage.DeleteTaskRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      c.region,
			Cluster:     c.cluster,
			ServiceName: task.serviceName,
		},
		TaskType: task.opts.TaskType,
	}
	err := c.managecli.DeleteTask(ctx, req)
	if err != nil {
		glog.Errorln("DeleteTask error", err, "requuid", requuid, task)
	}

	// task done, remove it from map
	c.tlock.Lock()
	defer c.tlock.Unlock()
	delete(c.tasks, task.serviceName)
}

func (c *catalogServiceInit) isServiceInitialized(ctx context.Context, task *serviceTask, requuid string) (bool, error) {
	req := &manage.ServiceCommonRequest{
		Region:      c.region,
		Cluster:     c.cluster,
		ServiceName: task.serviceName,
	}
	attr, err := c.managecli.GetServiceAttr(ctx, req)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, task)
		return false, err
	}

	return attr.Meta.ServiceStatus == common.ServiceStatusActive, nil
}

func (c *catalogServiceInit) waitServiceRunning(ctx context.Context, task *serviceTask, requuid string) error {
	req := &manage.ServiceCommonRequest{
		Region:      c.region,
		Cluster:     c.cluster,
		ServiceName: task.serviceName,
	}

	sleepTime := time.Duration(common.DefaultRetryWaitSeconds) * time.Second
	for sec := int64(0); sec < common.DefaultServiceWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		status, err := c.managecli.GetServiceStatus(ctx, req)
		if err != nil {
			// The service is successfully created. It may be possible there are some
			// temporary error, such as network error. For example, ECS may return MISSING
			// for the GET right after the service creation.
			// Here just log the GetServiceStatus error and retry.
			glog.Errorln("GetServiceStatus error", err, "requuid", requuid, task)
		} else {
			if status.RunningCount == status.DesiredCount && status.DesiredCount != 0 {
				glog.Infoln("The service containers are all running, requuid", requuid, status, task)
				task.statusMessage = serviceRunningMsg
				return nil
			}

			glog.Infoln("service running containers", status.RunningCount, "desired", status.DesiredCount, "requuid", requuid, task)
			task.statusMessage = fmt.Sprintf(waitServiceRunningMsg, status.RunningCount)
		}

		time.Sleep(sleepTime)
	}

	glog.Errorln("service is not running after", common.DefaultTaskWaitSeconds, "seconds, requuid", requuid, task)
	return common.ErrTimeout
}

func (c *catalogServiceInit) waitTask(ctx context.Context, taskID string, task *serviceTask, requuid string) error {
	req := &manage.GetTaskStatusRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      c.region,
			Cluster:     c.cluster,
			ServiceName: task.serviceName,
		},
		TaskID: taskID,
	}

	sleepTime := time.Duration(common.DefaultRetryWaitSeconds) * time.Second
	for sec := int64(0); sec < common.DefaultTaskWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		// wait some time for the task to run
		time.Sleep(sleepTime)

		taskStatus, err := c.managecli.GetTaskStatus(ctx, req)
		if err != nil {
			glog.Errorln("GetTaskStatus error", err, "requuid", requuid, "taskID", taskID, task)
		} else {
			glog.Infoln("get task", taskID, "status", taskStatus, "requuid", requuid, task)
			if taskStatus.Status == common.TaskStatusStopped {
				return nil
			}
		}
	}

	glog.Errorln("task", taskID, "is not done after", common.DefaultTaskWaitSeconds, "seconds, requuid", requuid, task)
	return common.ErrTimeout
}

func (c *catalogServiceInit) UpdateTaskStatusMsg(serviceName string, statusMsg string) {
	c.tlock.Lock()
	defer c.tlock.Unlock()

	// check if service has the task running
	task, ok := c.tasks[serviceName]
	if ok {
		glog.Infoln("service task is running", task, "update status message", statusMsg)
		task.statusMessage = statusMsg
	} else {
		glog.Infoln("service does not have running task", serviceName, "for status message", statusMsg)
	}
}
