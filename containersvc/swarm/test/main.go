package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/containersvc"
	"github.com/cloudstax/openmanage/containersvc/swarm"
	"github.com/cloudstax/openmanage/utils"
)

var (
	host      = flag.String("host", "tcp://127.0.0.1", "the swarm manager host, example: tcp://127.0.0.1")
	port      = flag.Int("port", 2376, "the swarm manager port, default: 2376")
	tlsVerify = flag.Bool("tlsVerify", false, "enable/disable tlsVerify to talk with swarm manager")
	caFile    = flag.String("tlscacert", "", "The CA file")
	certFile  = flag.String("tlscert", "", "The TLS server certificate file")
	keyFile   = flag.String("tlskey", "", "The TLS server key file")
)

func main() {
	// please pre-create the swarm cluster.
	//
	// Inside file /lib/systemd/system/docker.service change:
	//   ExecStart=/usr/bin/dockerd fd://
	// with
	//   ExecStart=/usr/bin/dockerd -H tcp://0.0.0.0:2376
	// or
	//   ExecStart=/usr/bin/dockerd -H tcp://0.0.0.0:2376 -H unix:///var/run/docker.sock
	//
	// Inside file /etc/default/docker change:
	//   DOCKER_OPTS=
	// with
	//   DOCKER_OPTS="-H tcp://0.0.0.0:2376"
	// or
	//   DOCKER_OPTS="-H tcp://127.0.0.1:2376 -H unix:///var/run/docker.sock"
	//
	// to enable tls, modify /etc/default/docker
	// #DOCKER_OPTS="-H tcp://0.0.0.0:2376 --tlsverify --tlscacert=/home/junius/.certs/ca.pem --tlscert=/home/junius/.certs/cert.pem --tlskey=/home/junius/.certs/key.pem"

	flag.Parse()

	if *tlsVerify && (*caFile == "" || *certFile == "" || *keyFile == "") {
		fmt.Println("please input swarm manager cluster, service, tlsVerify and caFile/certFile/keyFile")
		os.Exit(-1)
	}

	addr := *host + ":" + strconv.Itoa(*port)
	addrs := []string{addr}
	e := swarmsvc.NewSwarmSvc(addrs, *tlsVerify, *caFile, *certFile, *keyFile)

	ctx := context.Background()

	err := testService(ctx, e)
	if err != nil {
		fmt.Println("\n== testService error", err)
		return
	}

	err = testTask(ctx, e)
	if err != nil {
		fmt.Println("\n== testTask error", err)
		return
	}

	fmt.Println("\n== pass ==")
}

func testService(ctx context.Context, e *swarmsvc.SwarmSvc) error {
	cluster := "cluster"
	service := "service-" + utils.GenUUID()

	// check service not exist
	exist, err := e.IsServiceExist(ctx, cluster, service)
	if err != common.ErrNotFound {
		glog.Errorln("IsServiceExist expect NotFound, got error", err, service, cluster)
		return err
	}
	if exist {
		glog.Errorln("service", service, "exists")
		return common.ErrInternal
	}

	// create the service
	replicas := int64(1)
	commonOpts := &containersvc.CommonOptions{
		Cluster:        cluster,
		ServiceName:    service,
		ServiceUUID:    service + "-serviceUUID",
		ContainerImage: containersvc.TestBusyBoxContainerImage,
		Resource: &common.Resources{
			MaxCPUUnits:     -1,
			ReserveCPUUnits: int64(16),
			MaxMemMB:        -1,
			ReserveMemMB:    int64(16),
		},
	}
	createOpts := &containersvc.CreateServiceOptions{
		Common:        commonOpts,
		ContainerPath: "",
		Port:          int64(23011),
		Replicas:      replicas,
	}

	err = e.CreateService(ctx, createOpts)
	if err != nil {
		glog.Errorln("CreateService error", err, commonOpts)
		return err
	}
	defer deleteService(ctx, e, cluster, service)

	fmt.Println("\ncreated service", service)

	// check service exist
	exist, err = e.IsServiceExist(ctx, cluster, service)
	if err != nil {
		glog.Errorln("IsServiceExist error", err, service, cluster)
		return err
	}
	if !exist {
		glog.Errorln("service", service, "not exists")
		return common.ErrInternal
	}

	// wait till all tasks are running
	maxWaitSeconds := int64(60)
	err = e.WaitServiceRunning(ctx, cluster, service, replicas, maxWaitSeconds)
	if err != nil {
		glog.Errorln("WaitServiceRunning error", err, service)
		return err
	}

	fmt.Println("\nservice container is running", service)

	// list tasks of service
	tasks, err := e.ListActiveServiceTasks(ctx, cluster, service)
	if err != nil {
		glog.Errorln("ListTasks failed, cluster", cluster, "service", service, "error", err)
		return err
	}

	fmt.Println("\nListTasks, cluster", cluster, "service", service, "taskID", tasks)

	// get task
	for taskID := range tasks {
		contInsID, err := e.GetTaskContainerInstance(ctx, cluster, taskID)
		if err != nil {
			glog.Errorln("GetTaskContainerInstance error", err, "cluster", cluster, "taskID", taskID)
			return err
		}

		taskID1, err := e.GetServiceTask(ctx, cluster, service, contInsID)
		if err != nil {
			glog.Errorln("GetTask error", err, "cluster", cluster, "service", service, "contInsID", contInsID)
			return err
		}
		if taskID1 != taskID {
			glog.Errorln("expect task", taskID, "got task", taskID1, "cluster", cluster,
				"service", service, "contInsID", contInsID)
			return common.ErrInternal
		}
		fmt.Println("\ntask", taskID, "ContainerInstance", contInsID)

		// negative case: get task of non-exist service
		taskID1, err = e.GetServiceTask(ctx, cluster, "service-xxx", contInsID)
		if err == nil {
			glog.Errorln("GetServiceTask succeeds on non-exist service-xxx")
			return common.ErrInternal
		}
		// negative case: get service task on non-exist container instance
		taskID1, err = e.GetServiceTask(ctx, cluster, service, "container-instance-xxx")
		if err == nil {
			glog.Errorln("GetServiceTask succeeds on non-exist container-instance-xxx", service)
			return common.ErrInternal
		}
	}

	// negative case: list tasks of non-exist service
	tasks, err = e.ListActiveServiceTasks(ctx, cluster, "service-xxx")
	if err == nil && len(tasks) != 0 {
		glog.Errorln("list non-exist task of service-xxx succeeded")
		return common.ErrInternal
	}

	fmt.Println("\n== service test pass ==")
	return nil
}

func deleteService(ctx context.Context, e *swarmsvc.SwarmSvc, cluster string, service string) {
	err := e.StopService(ctx, cluster, service)
	if err != nil {
		glog.Errorln("StopService error", err, service, cluster)
	}

	err = e.DeleteService(ctx, cluster, service)
	if err != nil {
		glog.Errorln("DeleteService error", err, service, cluster)
	}
}

func testTask(ctx context.Context, e *swarmsvc.SwarmSvc) error {
	cluster := "cluster"
	taskName := "task" + utils.GenUUID()

	commonOpts := &containersvc.CommonOptions{
		Cluster:        cluster,
		ServiceName:    taskName,
		ServiceUUID:    taskName + "-UUID",
		ContainerImage: containersvc.TestBusyBoxContainerImage,
		Resource: &common.Resources{
			MaxCPUUnits:     -1,
			ReserveCPUUnits: int64(16),
			MaxMemMB:        -1,
			ReserveMemMB:    int64(16),
		},
	}

	taskOpts := &containersvc.RunTaskOptions{
		Common:   commonOpts,
		TaskType: common.TaskTypeInit,
	}

	// run task
	taskID, err := e.RunTask(ctx, taskOpts)
	if err != nil {
		glog.Errorln("RunTask error", err, commonOpts)
		return err
	}
	defer e.DeleteTask(ctx, cluster, taskName, taskOpts.TaskType)

	fmt.Println("\ncreated task", taskID, taskName)

	// wait task complete
	maxWaitSeconds := int64(60)
	err = e.WaitTaskComplete(ctx, cluster, taskID, maxWaitSeconds)
	if err != nil {
		glog.Errorln("WaitTaskComplete error", err, "taskID", taskID, commonOpts)
		return err
	}

	fmt.Println("\n== task test pass ==")
	return nil
}
