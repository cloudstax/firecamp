package containersvc

import (
	"flag"
	"strconv"
	"testing"

	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/pkg/log/jsonfile"
)

func TestMemContainerSvc(t *testing.T) {
	flag.Parse()

	e := NewMemContainerSvc()

	cluster := "cluster"
	servicePrefix := "service-"

	ctx := context.Background()

	// add 3 tasks for service1
	replicas1 := int64(3)
	service1 := servicePrefix + "1"
	defer e.DeleteService(ctx, cluster, service1)
	serviceTest(ctx, t, e, cluster, service1, replicas1)

	// add 4 tasks for service2
	replicas2 := int64(4)
	service2 := servicePrefix + "2"
	defer e.DeleteService(ctx, cluster, service2)
	serviceTest(ctx, t, e, cluster, service2, replicas2)

	// test task
	commonOpts := &CommonOptions{
		Cluster:        cluster,
		ServiceName:    service1,
		ServiceUUID:    service1 + "uuid",
		ContainerImage: TestBusyBoxContainerImage,
		Resource: &common.Resources{
			MaxCPUUnits:     common.DefaultMaxCPUUnits,
			ReserveCPUUnits: int64(0),
			MaxMemMB:        int64(128),
			ReserveMemMB:    int64(16),
		},
		LogConfig: jsonfilelog.CreateJSONFileLogConfig(),
	}

	opts := &RunTaskOptions{
		Common:   commonOpts,
		TaskType: common.TaskTypeInit,
	}

	taskID, err := e.RunTask(ctx, opts)
	if err != nil {
		t.Fatalf("RunTask error %s, %s", err, opts.Common)
	}

	_, err = e.GetTaskStatus(ctx, cluster, taskID)
	if err != nil {
		t.Fatalf("GetTaskStatus error %s, taskID %s, %s", err, taskID, opts.Common)
	}

	err = e.DeleteTask(ctx, opts.Common.Cluster, opts.Common.ServiceName, opts.TaskType)
	if err != nil {
		t.Fatalf("DeleteTask error %s, %s", err, opts.Common)
	}
}

func serviceTest(ctx context.Context, t *testing.T, e *MemContainerSvc, cluster string, service string, replicas int64) {
	exist, err := e.IsServiceExist(ctx, cluster, service)
	if err != nil {
		t.Fatalf("IsServiceExist error %s, cluster %s service %s", err, cluster, service)
	}
	if exist {
		t.Fatalf("service %s should not exist, cluster %s", service, cluster)
	}

	commonOpts := &CommonOptions{
		Cluster:        cluster,
		ServiceName:    service,
		ServiceUUID:    service + "uuid",
		ContainerImage: TestBusyBoxContainerImage,
		Resource: &common.Resources{
			MaxCPUUnits:     common.DefaultMaxCPUUnits,
			ReserveCPUUnits: int64(0),
			MaxMemMB:        int64(128),
			ReserveMemMB:    int64(16),
		},
		LogConfig: jsonfilelog.CreateJSONFileLogConfig(),
	}

	p := common.PortMapping{
		ContainerPort: 23011,
		HostPort:      23011,
	}

	opts := &CreateServiceOptions{
		Common:       commonOpts,
		PortMappings: []common.PortMapping{p},
		Replicas:     int64(0),
	}

	err = e.CreateService(ctx, opts)
	if err != nil {
		t.Fatalf("CreateService error %s, %s", err, opts)
	}

	exist, err = e.IsServiceExist(ctx, cluster, service)
	if err != nil {
		t.Fatalf("IsServiceExist error %s, cluster %s service %s", err, cluster, service)
	}
	if !exist {
		t.Fatalf("service %s should exist, cluster %s", service, cluster)
	}

	for i := int64(0); i < replicas; i++ {
		str := strconv.FormatInt(i, 10)
		taskArn := service + "-task-" + str
		contIns := service + "-container-instance-" + str

		err := e.AddServiceTask(ctx, cluster, service, taskArn, contIns)
		if err != nil {
			t.Fatalf("AddTask error %s, cluster %s, service %s, task %s, contIns %s",
				err, cluster, service, taskArn, contIns)
		}
	}

	// list tasks to verify
	tasks, err := e.ListActiveServiceTasks(ctx, cluster, service)
	if err != nil {
		t.Fatalf("ListTasks error %s, service %s", err, service)
	}
	for i := int64(0); i < replicas; i++ {
		str := strconv.FormatInt(i, 10)
		taskArn := service + "-task-" + str
		_, ok := tasks[taskArn]
		if !ok {
			t.Fatalf("expect task %s, but not found in list tasks %s", taskArn, tasks)
		}
	}

	// get task to verify
	expectContTask := service + "-task-" + "1"
	contIns := service + "-container-instance-" + "1"
	contInsTask, err := e.GetServiceTask(ctx, cluster, service, contIns)
	if contInsTask != expectContTask {
		t.Fatalf("expect task %s for containerInstance %s, but got task %s", expectContTask, contIns, contInsTask)
	}
	contInsTask, err = e.GetServiceTask(ctx, cluster, service, "non-exit-contIns")
	if err != ErrContainerSvcNoTask {
		t.Fatalf("expect ErrContainerSvcNoTask, error %s, service %s contIns %s contInsTask %s", err, service, contIns, contInsTask)
	}

	_, err = e.GetServiceStatus(ctx, cluster, service)
	if err != nil {
		t.Fatalf("GetServiceStatus error %s, service %s cluster %s", err, service, cluster)
	}

	// negative case, add 3 tasks on the same contIns
	contIns = service + "-container-instance-" + "xxx"
	for i := replicas; i < (replicas + 3); i++ {
		str := strconv.FormatInt(i, 10)
		taskArn := service + "-task-" + str
		err := e.AddServiceTask(ctx, cluster, service, taskArn, contIns)
		if err != nil {
			t.Fatalf("AddTask error %s, cluster %s, service %s, task %s, contIns %s",
				err, cluster, service, taskArn, contIns)
		}
	}
	contInsTask, err = e.GetServiceTask(ctx, cluster, service, contIns)
	if err != ErrContainerSvcTooManyTasks {
		t.Fatalf("expect ErrContainerSvcTooManyTasks, error %s, service %s contIns %s contInsTask %s", err, service, contIns, contInsTask)
	}
	contInsTask, err = e.GetServiceTask(ctx, cluster, service, "non-exit-contIns")
	if err != ErrContainerSvcNoTask {
		t.Fatalf("expect ErrContainerSvcNoTask, error %s, service %s contIns %s contInsTask %s", err, service, contIns, contInsTask)
	}
}
