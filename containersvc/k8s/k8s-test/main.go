// Note: the example only works with the code within the same release/branch.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/containersvc/k8s"
)

var kubeconfig = flag.String("kubeconfig", "/home/ubuntu/.kube/config", "absolute path to the kubeconfig file")
var runService = flag.Bool("run-service", true, "run test service")
var runTask = flag.Bool("run-task", true, "run test task")

func main() {
	flag.Parse()

	glog.Infoln("kubeconfig path", *kubeconfig)

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	svc, err := k8ssvc.NewTestK8sSvc(common.CloudPlatformAWS, "default", config)
	if err != nil {
		panic(err.Error())
	}

	if *runService {
		testService(svc)
	}
	if *runTask {
		testTask(svc)
	}
}

func testService(svc *k8ssvc.K8sSvc) {
	cluster := "c1"
	service := "s1"
	serviceuuid := "s1-uuid"
	opts := &containersvc.CreateServiceOptions{
		Common: &containersvc.CommonOptions{
			Cluster:        cluster,
			ServiceName:    service,
			ServiceUUID:    serviceuuid,
			ContainerImage: "cloudstax/firecamp-busybox",
			Resource: &common.Resources{
				MaxCPUUnits:     common.DefaultMaxCPUUnits,
				ReserveCPUUnits: common.DefaultReserveCPUUnits,
				MaxMemMB:        common.DefaultMaxMemoryMB,
				ReserveMemMB:    common.DefaultReserveMemoryMB,
			},
		},
		DataVolume: &containersvc.VolumeOptions{
			MountPath:  "/data",
			VolumeType: common.VolumeTypeGPSSD,
			SizeGB:     1,
		},
		JournalVolume: &containersvc.VolumeOptions{
			MountPath:  "/journal",
			VolumeType: common.VolumeTypeGPSSD,
			SizeGB:     1,
		},
		PortMappings: []common.PortMapping{
			{ContainerPort: 5000, HostPort: 5000},
		},
		Replicas: 1,
		Envkvs: []*common.EnvKeyValuePair{
			{Name: "env1", Value: "val1"},
		},
		KubeOptions: &containersvc.K8sOptions{
			InitContainerImage: "cloudstax/firecamp-initcontainer",
			ServicePort:        5001,
			ExternalDNS:        false,
		},
	}

	ctx := context.Background()

	exist, err := svc.IsServiceExist(ctx, cluster, service)
	if err != nil {
		panic(err.Error())
	}
	if exist {
		panic(fmt.Sprintf("service %s already exists", service))
	}

	err = svc.CreateService(ctx, opts)
	if err != nil {
		panic(err.Error())
	}

	exist, err = svc.IsServiceExist(ctx, cluster, service)
	if err != nil {
		panic(err.Error())
	}
	if !exist {
		panic(fmt.Sprintf("service %s exists", service))
	}

	maxRetry := 20
	for i := 0; i < maxRetry; i++ {
		status, err := svc.GetServiceStatus(ctx, cluster, service)
		if err != nil {
			panic(err.Error())
		}

		fmt.Println("get service status", status, service, cluster)

		if status.RunningCount == status.DesiredCount && status.RunningCount == opts.Replicas {
			fmt.Println("all replicas are running")
			break
		}

		if i == (maxRetry - 1) {
			panic(fmt.Sprintf("not all replicas are running after %d seconds", maxRetry*common.DefaultRetryWaitSeconds))
		}

		time.Sleep(time.Duration(common.DefaultRetryWaitSeconds) * time.Second)
	}

	err = svc.StopService(ctx, cluster, service)
	if err != nil {
		panic(err.Error())
	}

	err = svc.ScaleService(ctx, cluster, service, opts.Replicas)
	if err != nil {
		panic(err.Error())
	}

	err = svc.DeleteService(ctx, cluster, service)
	if err != nil {
		panic(err.Error())
	}
}

func testTask(svc *k8ssvc.K8sSvc) {
	cluster := "taskcluster"
	service := "tasksvc"
	serviceuuid := "tasksvc-uuid"
	opts := &containersvc.RunTaskOptions{
		Common: &containersvc.CommonOptions{
			Cluster:        cluster,
			ServiceName:    service,
			ServiceUUID:    serviceuuid,
			ContainerImage: "cloudstax/firecamp-busybox",
			Resource: &common.Resources{
				MaxCPUUnits:     common.DefaultMaxCPUUnits,
				ReserveCPUUnits: common.DefaultReserveCPUUnits,
				MaxMemMB:        common.DefaultMaxMemoryMB,
				ReserveMemMB:    common.DefaultReserveMemoryMB,
			},
		},
		TaskType: common.TaskTypeInit,
		Envkvs: []*common.EnvKeyValuePair{
			{Name: "env1", Value: "val1"},
		},
	}

	ctx := context.Background()

	taskID, err := svc.RunTask(ctx, opts)
	if err != nil {
		fmt.Println("run task error", err)
		os.Exit(-1)
	}

	fmt.Println("created task", taskID)

	maxRetry := 20
	for i := 0; i < maxRetry; i++ {
		status, err := svc.GetTaskStatus(ctx, cluster, taskID)
		if err != nil {
			fmt.Println("GetTaskStatus error", err)
			os.Exit(-1)
		}

		fmt.Println("get task status", status)

		if status.Status == common.TaskStatusStopped {
			break
		}

		if i == (maxRetry - 1) {
			fmt.Println("task does not complete after", maxRetry*common.DefaultRetryWaitSeconds, "seconds")
			os.Exit(-1)
		}

		time.Sleep(time.Duration(common.DefaultRetryWaitSeconds) * time.Second)
	}

	err = svc.DeleteTask(ctx, cluster, service, opts.TaskType)
	if err != nil {
		fmt.Println("DeleteTask error", err)
		os.Exit(-1)
	}

	fmt.Println("deleted task", taskID)
}
