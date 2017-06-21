package manageserver

import (
	"sync"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/containersvc"
	"github.com/cloudstax/openmanage/db"
	"github.com/cloudstax/openmanage/utils"
)

const (
	// The max concurrent service tasks. 100 would be enough.
	maxTaskCounts = 100
	// Wait some time for service to stabilize before running the init task.
	waitSecondsBeforeInit = time.Duration(10) * time.Second
)

type serviceTask struct {
	// The uuid of the service
	serviceUUID string
	serviceName string
	// The catalog service type, defined in catalog/types.go
	serviceType string
	// The task detail
	opts *containersvc.RunTaskOptions
}

// catalogServiceInit serves and tracks the service init tasks.
// Every task will have its own goroutine to run.
// Currently only the init task is supported. Revisit if different/multiple
// tasks of one service are allowed in the future.
type catalogServiceInit struct {
	cluster         string
	containersvcIns containersvc.ContainerSvc
	dbIns           db.DB

	// The task map to track whether the service has task running. key: serviceUUID, value: not used.
	tasks map[string]*serviceTask
	tlock *sync.Mutex
}

func newCatalogServiceInit(cluster string, containersvcIns containersvc.ContainerSvc, dbIns db.DB) *catalogServiceInit {
	c := &catalogServiceInit{
		cluster:         cluster,
		containersvcIns: containersvcIns,
		dbIns:           dbIns,
		tasks:           make(map[string]*serviceTask),
		tlock:           &sync.Mutex{},
	}

	return c
}

func (c *catalogServiceInit) hasInitTask(ctx context.Context, serviceUUID string) bool {
	c.tlock.Lock()
	defer c.tlock.Unlock()

	_, ok := c.tasks[serviceUUID]
	return ok
}

func (c *catalogServiceInit) addInitTask(ctx context.Context, task *serviceTask) error {
	requuid := utils.GetReqIDFromContext(ctx)

	c.tlock.Lock()
	defer c.tlock.Unlock()

	// check if service has the same task running
	_, ok := c.tasks[task.serviceUUID]
	if ok {
		glog.Infoln("service task is already running", task, "requuid", requuid)
		return nil
	}

	// service does not have running task, run it
	if len(c.tasks) > maxTaskCounts {
		glog.Errorln("exceed maxTaskCounts", maxTaskCounts, "could not run task", task, "requuid", requuid)
		return common.ErrInternal
	}

	c.tasks[task.serviceUUID] = task
	go c.runInitTask(ctx, task, requuid)

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

	for i := 0; i < common.DefaultTaskRetryCounts; i++ {
		// run task
		taskID, err := c.containersvcIns.RunTask(ctx, task.opts)
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
	err := c.containersvcIns.DeleteTask(ctx, c.cluster, task.opts.Common.ServiceName, task.opts.TaskType)
	if err != nil {
		glog.Errorln("DeleteTask error", err, "requuid", requuid, task)
	}

	// task done, remove it from map
	c.tlock.Lock()
	defer c.tlock.Unlock()
	delete(c.tasks, task.serviceUUID)
}

func (c *catalogServiceInit) isServiceInitialized(ctx context.Context, task *serviceTask, requuid string) (bool, error) {
	attr, err := c.dbIns.GetServiceAttr(ctx, task.serviceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, task)
		return false, err
	}

	return attr.ServiceStatus == common.ServiceStatusActive, nil
}

func (c *catalogServiceInit) waitServiceRunning(ctx context.Context, task *serviceTask, requuid string) error {
	sleepTime := time.Duration(common.DefaultRetryWaitSeconds) * time.Second
	for sec := int64(0); sec < common.DefaultServiceWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		status, err := c.containersvcIns.GetServiceStatus(ctx, c.cluster, task.serviceName)
		if err != nil {
			// The service is successfully created. It may be possible there are some
			// temporary error, such as network error. For example, ECS may return MISSING
			// for the GET right after the service creation.
			// Here just log the GetServiceStatus error and retry.
			glog.Errorln("GetServiceStatus error", err, "requuid", requuid, task)
		} else {
			if status.RunningCount == status.DesiredCount {
				glog.Infoln("The service containers are all running, requuid", requuid, task)
				return nil
			}
		}

		time.Sleep(sleepTime)
	}
	glog.Errorln("service is not running after", common.DefaultTaskWaitSeconds, "seconds, requuid", requuid, task)
	return common.ErrTimeout
}

func (c *catalogServiceInit) waitTask(ctx context.Context, taskID string, task *serviceTask, requuid string) error {
	sleepTime := time.Duration(common.DefaultRetryWaitSeconds) * time.Second
	for sec := int64(0); sec < common.DefaultTaskWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		// wait some time for the task to run
		time.Sleep(sleepTime)

		taskStatus, err := c.containersvcIns.GetTaskStatus(ctx, c.cluster, taskID)
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
