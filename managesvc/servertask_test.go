package managesvc

import (
	"testing"
	"time"

	"github.com/cloudstax/firecamp/containersvc"
)

func TestServerTask(t *testing.T) {
	containersvcIns := containersvc.NewMemContainerSvc()
	cluster := "c1"
	tasksvc := newManageTaskService(cluster, containersvcIns)
	tasksvc.taskDoneWaitSeconds = 2

	serviceUUID := "s1-uuid"
	has, msg, complete := tasksvc.hasTask(serviceUUID)
	if has {
		t.Fatalf("expect no task for service %s", serviceUUID)
	}

	serviceName := "s1"
	task := &manageTask{
		serviceUUID: serviceUUID,
		serviceName: serviceName,
		restartOpts: &containersvc.RollingRestartOptions{
			Replicas:     1,
			ServiceTasks: []string{"task-1"},
		},
	}
	err := tasksvc.addTask(serviceUUID, task, "requuid")
	if err != nil {
		t.Fatalf("addTask expect success, get error %s", err)
	}

	has, msg, complete = tasksvc.hasTask(serviceUUID)
	if !has {
		t.Fatalf("expect task for service %s", serviceUUID)
	}

	time.Sleep(time.Duration(1) * time.Second)

	has, msg, complete = tasksvc.hasTask(serviceUUID)
	if !has {
		t.Fatalf("expect task for service %s", serviceUUID)
	}
	if len(msg) == 0 || !complete {
		t.Fatalf("expect non-zero status message and task complete, get message %s, complete %d", msg, complete)
	}
}
