package managesvc

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/utils"
)

type manageTask struct {
	serviceUUID string
	serviceName string
	complete    bool
	restartOpts *containersvc.RollingRestartOptions
}

type manageTaskService struct {
	cluster         string
	containersvcIns containersvc.ContainerSvc
	// when task completes, wait some time for cli to get the status
	taskDoneWaitSeconds int64

	// The task map to track whether the service has task running. key: serviceUUID, value: not used.
	tasks map[string]*manageTask
	tlock *sync.Mutex
}

func newManageTaskService(cluster string, containersvcIns containersvc.ContainerSvc) *manageTaskService {
	s := &manageTaskService{
		cluster:             cluster,
		containersvcIns:     containersvcIns,
		taskDoneWaitSeconds: 2 * common.CliRetryWaitSeconds,
		tasks:               make(map[string]*manageTask),
		tlock:               &sync.Mutex{},
	}
	return s
}

func (s *manageTaskService) hasTask(serviceUUID string) (has bool, statusMsg string, complete bool) {
	s.tlock.Lock()
	defer s.tlock.Unlock()

	task, ok := s.tasks[serviceUUID]
	if ok {
		statusMsg = task.restartOpts.StatusMessage
		complete = task.complete
	}
	return ok, statusMsg, complete
}

// task will run in the background.
func (s *manageTaskService) addTask(serviceUUID string, task *manageTask, requuid string) error {
	ctx := utils.NewRequestContext(context.Background(), requuid)

	s.tlock.Lock()
	defer s.tlock.Unlock()

	// check if service has the same task running
	_, ok := s.tasks[task.serviceUUID]
	if ok {
		glog.Infoln("service task is already running", task, "requuid", requuid)
		return nil
	}

	if len(s.tasks) > maxTaskCounts {
		glog.Errorln("exceed maxTaskCounts", maxTaskCounts, "could not run task", task, "requuid", requuid)
		return errors.New("reach max task counts")
	}

	task.restartOpts.StatusMessage = "start rolling restart service"

	// service does not have running task, run it
	s.tasks[task.serviceUUID] = task
	go s.runTask(ctx, task, requuid)

	glog.Infoln("add task", task, "requuid", requuid)
	return nil
}

func (s *manageTaskService) runTask(ctx context.Context, task *manageTask, requuid string) {
	// remove the task from map when task is done
	defer s.taskDone(ctx, task, requuid)

	// support rolling restart task only. refine when more tasks are added.
	err := s.containersvcIns.RollingRestartService(ctx, s.cluster, task.serviceName, task.restartOpts)
	if err != nil {
		task.restartOpts.StatusMessage = fmt.Sprintf("RollingRestartService error %s", err.Error())
		glog.Errorln("RollingRestartService error", err, "requuid", requuid, task)
	} else {
		task.restartOpts.StatusMessage = "RollingRestartService complete"
	}
	task.complete = true
}

func (s *manageTaskService) taskDone(ctx context.Context, task *manageTask, requuid string) {
	// wait some time for cli to get the results
	time.Sleep(time.Duration(s.taskDoneWaitSeconds) * time.Second)

	glog.Infoln("deleted service rolling restart task", task, "requuid", requuid)

	// task done, remove it from map
	s.tlock.Lock()
	defer s.tlock.Unlock()

	delete(s.tasks, task.serviceUUID)
}
