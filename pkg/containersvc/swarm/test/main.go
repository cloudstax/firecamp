package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/jazzl0ver/firecamp/pkg/containersvc"
	"github.com/jazzl0ver/firecamp/pkg/containersvc/swarm"
	"github.com/jazzl0ver/firecamp/pkg/log/jsonfile"
	"github.com/jazzl0ver/firecamp/pkg/utils"
)

var (
	host = flag.String("host", "tcp://127.0.0.1", "the swarm manager host, example: tcp://127.0.0.1")
	port = flag.Int("port", 2376, "the swarm manager port, default: 2376")
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
	//
	// Amazon Linux AMI: see /etc/init.d/docker, OPTIONS is configed in /etc/sysconfig/docker
	// OPTIONS="--default-ulimit nofile=1024:4096 -H tcp://0.0.0.0:2375 -H unix:///var/run/docker.sock"

	flag.Parse()

	ctx := context.Background()
	swarmInitTest(ctx)
	swarmServiceTest(ctx)
}

func swarmInitTest(ctx context.Context) {
	azs := []string{"local"}
	svc, err := swarmsvc.NewSwarmSvcOnManagerNode(azs)
	if err != nil {
		fmt.Println("NewSwarmSvcOnManagerNode error", err)
		os.Exit(-1)
	}

	init, err := svc.IsSwarmInitialized(ctx)
	if err != nil {
		fmt.Println("IsSwarmInitialized error", err, init)
		os.Exit(-1)
	}

	if !init {
		addr := "192.168.1.59"
		err = svc.SwarmInit(ctx, addr)
		fmt.Println("InitSwarm addr", addr, "error", err)
	}

	mtoken, wtoken, err := svc.GetJoinToken(ctx)
	fmt.Println("GetJoinToken manager", mtoken, "worker", wtoken, "error", err)

	goodManagers, _, downManagers, err := svc.ListSwarmManagerNodes(ctx)
	fmt.Println("goodManagers", goodManagers, "downManagers", downManagers)
}

func swarmServiceTest(ctx context.Context) {
	testTalkToLocalManager(ctx)
	testLocalClient(ctx)

	clusterName := "c1"
	info, err := swarmsvc.NewSwarmInfo(clusterName)
	if err != nil {
		fmt.Println("NewSwarmInfo error", err)
		os.Exit(-1)
	}

	fmt.Println(info.GetContainerClusterID(), info.GetLocalContainerInstanceID())
	azs := []string{"local"}
	e, err := swarmsvc.NewSwarmSvcOnManagerNode(azs)
	if err != nil {
		fmt.Println("NewSwarmSvcOnManagerNode error", err)
		os.Exit(-1)
	}

	err = testService(ctx, e)
	if err != nil {
		fmt.Println("\n== testService error", err)
		os.Exit(-1)
	}

	err = testTask(ctx, e)
	if err != nil {
		fmt.Println("\n== testTask error", err)
		os.Exit(-1)
	}

	fmt.Println("\n== pass ==")
}

func testTalkToLocalManager(ctx context.Context) {
	addr := *host + ":" + strconv.Itoa(*port)
	cli, err := client.NewClient(addr, "1.27", nil, nil)
	if err != nil {
		fmt.Println("ERROR: NewClient to", addr, "error", err)
		return
	}

	service := "service-manager"
	svc, _, err := cli.ServiceInspectWithRaw(ctx, service, types.ServiceInspectOptions{})
	fmt.Println("inspect service", service, "error", err, "svc", svc)
}

func testLocalClient(ctx context.Context) {
	// this is to test docker client could talk with swarm manager via unix:///var/run/docker.sock.
	cli, err := client.NewEnvClient()
	if err != nil {
		fmt.Println("ERROR: NewEnvClient to unix sock error", err)
		os.Exit(-1)
	}

	ver, err := cli.ServerVersion(ctx)
	fmt.Println("docker client version", cli.ClientVersion(), "server api version", ver.APIVersion, ver, err)

	service := "service-aaa"
	svc, _, err := cli.ServiceInspectWithRaw(ctx, service, types.ServiceInspectOptions{})
	fmt.Println("inspect service", service, "error", err, "svc", svc)
}

func testService(ctx context.Context, e *swarmsvc.SwarmSvc) error {
	cluster := "cluster"
	service := "service-" + utils.GenUUID()

	// check service not exist
	exist, err := e.IsServiceExist(ctx, cluster, service)
	if err != nil {
		glog.Errorln("IsServiceExist expect success, got error", err, service, cluster)
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
			MaxCPUUnits:     common.DefaultMaxCPUUnits,
			ReserveCPUUnits: int64(16),
			MaxMemMB:        common.DefaultMaxMemoryMB,
			ReserveMemMB:    int64(16),
		},
		LogConfig: jsonfilelog.CreateJSONFileLogConfig(),
	}
	portmapping := common.PortMapping{
		ContainerPort: 23011,
		HostPort:      23011,
	}
	createOpts := &containersvc.CreateServiceOptions{
		Common:       commonOpts,
		PortMappings: []common.PortMapping{portmapping},
		Replicas:     replicas,
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

	// restart service
	err = e.StopService(ctx, cluster, service)
	if err != nil {
		glog.Errorln("StopService error", err, service)
		return err
	}
	err = e.ScaleService(ctx, cluster, service, replicas)
	if err != nil {
		glog.Errorln("ScaleService error", err, service)
		return err
	}

	fmt.Println("\nRestartService", service)

	// wait till all tasks are running
	err = e.WaitServiceRunning(ctx, cluster, service, replicas, maxWaitSeconds)
	if err != nil {
		glog.Errorln("WaitServiceRunning error", err, service)
		return err
	}

	fmt.Println("\nservice container is running", service)

	// list tasks of service again
	tasks, err = e.ListActiveServiceTasks(ctx, cluster, service)
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

	// test service update
	portmapping = common.PortMapping{
		ContainerPort: 23012,
		HostPort:      23012,
	}
	updateOpts := &containersvc.UpdateServiceOptions{
		Cluster:         cluster,
		ServiceName:     service,
		MaxCPUUnits:     utils.Int64Ptr(100),
		ReserveCPUUnits: utils.Int64Ptr(10),
		MaxMemMB:        utils.Int64Ptr(256),
		ReserveMemMB:    utils.Int64Ptr(10),
		PortMappings:    []common.PortMapping{portmapping},
		ExternalDNS:     true,
	}

	err = e.UpdateService(ctx, updateOpts)
	if err != nil {
		glog.Errorln("UpdateService error", err, service)
		return err
	}
	fmt.Println("\nupdated service", service)

	// wait till all tasks are running
	err = e.WaitServiceRunning(ctx, cluster, service, replicas, maxWaitSeconds)
	if err != nil {
		glog.Errorln("WaitServiceRunning error", err, service)
		return err
	}
	fmt.Println("\nall tasks are running after service update", service)

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
			MaxCPUUnits:     common.DefaultMaxCPUUnits,
			ReserveCPUUnits: int64(16),
			MaxMemMB:        common.DefaultMaxMemoryMB,
			ReserveMemMB:    int64(16),
		},
		LogConfig: jsonfilelog.CreateJSONFileLogConfig(),
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
