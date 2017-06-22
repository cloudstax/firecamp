package swarmsvc

import (
	"io"
	"net/http"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	mounttypes "github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/tlsconfig"
	"golang.org/x/net/context"

	"github.com/golang/glog"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/containersvc"
)

const (
	// service task filters. see the supported filter at
	// https://docs.docker.com/engine/reference/commandline/service_ps/#filtering
	filterID               = "id"
	filterName             = "name"
	filterNode             = "node"
	filterDesiredState     = "desired-state"
	taskStateRunning       = "running"
	nanoCPUs               = 1000000000
	defaultCPUPeriod       = 10000
	defaultCPUUnitsPerCore = 1024
	envKVSep               = "="
)

// SwarmSvc implements swarm service and task related functions.
//
// TODO task framework on Swarm.
// Swarm doesn't support the task execution. The SwarmSvc will have to manage the task
// lifecycle. The Docker daemon on the swarm worker node will have to listen on the
// host port, so docker API could be accessed remotely. The SwarmSvc will periodically
// collect the metrics, select one node, store it in the controldb. If the node is full,
// select another node to run the task.
// At v1, the task is simply run on the swarm manager node. This is not a big issue, as
// the task would usually run some simple job, such as setup the MongoDB ReplicaSet.
//
// TODO only talk with the first manager. Support retrying other managers on failure.
type SwarmSvc struct {
	// the swarm manager listening address, such as tcp://127.0.0.1:2376
	managerAddrs []string
	tlsVerify    bool
	caFile       string
	certFile     string
	keyFile      string
}

// NewSwarmSvc creates a new SwarmSvc instance
func NewSwarmSvc(managerAddrs []string, tlsVerify bool, caFile, certFile, keyFile string) *SwarmSvc {
	s := &SwarmSvc{
		managerAddrs: managerAddrs,
		tlsVerify:    tlsVerify,
		caFile:       caFile,
		certFile:     certFile,
		keyFile:      keyFile,
	}
	return s
}

// CreateService creates a swarm service
func (s *SwarmSvc) CreateService(ctx context.Context, opts *containersvc.CreateServiceOptions) error {
	cli, err := s.newClient()
	if err != nil {
		glog.Errorln("CreateService newClient error", err, "options", opts)
		return err
	}

	var mounts []mounttypes.Mount
	if len(opts.ContainerPath) != 0 {
		mount := mounttypes.Mount{
			Type:     mounttypes.TypeVolume,
			Source:   opts.Common.ServiceUUID,
			Target:   opts.ContainerPath,
			ReadOnly: false,
			VolumeOptions: &mounttypes.VolumeOptions{
				DriverConfig: &mounttypes.Driver{Name: common.VolumeDriverName},
			},
		}
		mounts = []mounttypes.Mount{mount}
	}

	taskTemplate := swarm.TaskSpec{
		ContainerSpec: swarm.ContainerSpec{
			Image:  opts.Common.ContainerImage,
			Mounts: mounts,
		},
		//LogDriver: opts.logDriver.toLogDriver(),
	}
	if opts.Common.Resource != nil {
		res := &swarm.ResourceRequirements{}
		setRes := false
		if opts.Common.Resource.MaxCPUUnits != -1 || opts.Common.Resource.MaxMemMB != -1 {
			limits := &swarm.Resources{}
			if opts.Common.Resource.MaxCPUUnits != -1 {
				limits.NanoCPUs = s.cpuUnits2NanoCPUs(opts.Common.Resource.MaxCPUUnits)
			}
			if opts.Common.Resource.MaxMemMB != -1 {
				limits.MemoryBytes = s.memoryMB2Bytes(opts.Common.Resource.MaxMemMB)
			}
			res.Limits = limits
			setRes = true
		}
		if opts.Common.Resource.ReserveCPUUnits != -1 || opts.Common.Resource.ReserveMemMB != -1 {
			reserve := &swarm.Resources{}
			if opts.Common.Resource.ReserveCPUUnits != -1 {
				reserve.NanoCPUs = s.cpuUnits2NanoCPUs(opts.Common.Resource.ReserveCPUUnits)
			}
			if opts.Common.Resource.ReserveMemMB != -1 {
				reserve.MemoryBytes = s.memoryMB2Bytes(opts.Common.Resource.ReserveMemMB)
			}
			res.Reservations = reserve
			setRes = true
		}
		if setRes {
			taskTemplate.Resources = res
		}
	}

	replicas := uint64(opts.Replicas)
	serviceSpec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name: opts.Common.ServiceName,
		},
		TaskTemplate: taskTemplate,
		//Networks: convertNetworks(opts.networks),
		Mode: swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{Replicas: &replicas},
		},
	}

	serviceOpts := types.ServiceCreateOptions{}

	resp, err := cli.ServiceCreate(ctx, serviceSpec, serviceOpts)
	if err != nil {
		glog.Errorln("CreateService error", err, "service spec", serviceSpec, "options", opts)
		return err
	}

	glog.Infoln("CreateService complete, resp", resp, "options", opts)
	return nil
}

func (s *SwarmSvc) cpuUnits2NanoCPUs(cpuUnits int64) int64 {
	return (cpuUnits * nanoCPUs) / defaultCPUUnitsPerCore
}

// ListActiveServiceTasks lists the active (running and pending) tasks of the service
func (s *SwarmSvc) ListActiveServiceTasks(ctx context.Context, cluster string, service string) (serviceTaskIDs map[string]bool, err error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add(filterName, service)
	filterArgs.Add(filterDesiredState, taskStateRunning)

	tasks, err := s.listTasks(filterArgs)
	if err != nil {
		glog.Errorln("ListTasks list error", err, "filterArgs", filterArgs)
		return nil, err
	}

	serviceTaskIDs = make(map[string]bool)
	for _, task := range tasks {
		glog.V(1).Infoln("list task", task)
		serviceTaskIDs[task.ID] = true
	}

	glog.Infoln("list", len(serviceTaskIDs), "tasks, service", service, "cluster", cluster)
	return serviceTaskIDs, nil
}

// GetServiceTask gets the task running on the containerInstanceID
func (s *SwarmSvc) GetServiceTask(ctx context.Context, cluster string, service string, containerInstanceID string) (serviceTaskID string, err error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add(filterName, service)
	filterArgs.Add(filterDesiredState, taskStateRunning)
	filterArgs.Add(filterNode, containerInstanceID)

	tasks, err := s.listTasks(filterArgs)
	if err != nil {
		glog.Errorln("GetTask list error", err, "filterArgs", filterArgs)
		return "", err
	}
	if len(tasks) == 0 {
		glog.Errorln("service", service, "has no task on ContainerInstance",
			containerInstanceID, "cluster", cluster)
		return "", containersvc.ErrContainerSvcNoTask
	}
	if len(tasks) != 1 {
		glog.Infoln("ContainerInstance", containerInstanceID, "has", len(tasks),
			"tasks, service", service, "cluster", cluster)
		return "", containersvc.ErrContainerSvcTooManyTasks
	}

	serviceTaskID = tasks[0].ID

	glog.Infoln("get task", serviceTaskID, "on ContainerInstance", containerInstanceID,
		"service", service, "cluster", cluster)
	return serviceTaskID, nil
}

// GetTaskContainerInstance returns the ContainerInstanceID the task runs on
func (s *SwarmSvc) GetTaskContainerInstance(ctx context.Context, cluster string,
	serviceTaskID string) (containerInstanceID string, err error) {
	cli, err := s.newClient()
	if err != nil {
		glog.Errorln("GetTaskContainerInstance newClient error", err, "service taskID", serviceTaskID)
		return "", err
	}

	task, _, err := cli.TaskInspectWithRaw(ctx, serviceTaskID)
	if err != nil {
		glog.Errorln("inspect task", serviceTaskID, "error", err)
		return "", err
	}

	glog.Infoln("task", serviceTaskID, "ContainerInstanceID", task.NodeID, task)
	return task.NodeID, nil
}

func (s *SwarmSvc) listTasks(filterArgs filters.Args) ([]swarm.Task, error) {
	cli, err := s.newClient()
	if err != nil {
		glog.Errorln("listTasks newClient error", err, "filterArgs", filterArgs)
		return nil, err
	}

	listOps := types.TaskListOptions{
		Filters: filterArgs,
	}

	tasks, err := cli.TaskList(context.Background(), listOps)
	glog.Infoln("Swarm TaskList filterArgs", filterArgs, "error", err, "tasks", len(tasks))
	return tasks, err
}

// IsServiceExist checks whether the service exists
func (s *SwarmSvc) IsServiceExist(ctx context.Context, cluster string, service string) (bool, error) {
	cli, err := s.newClient()
	if err != nil {
		glog.Errorln("IsServiceExists newClient error", err, "service", service)
		return false, err
	}

	_, err = s.inspectService(ctx, cli, service)
	if err != nil {
		glog.Errorln("inspect service error", err, "service", service, "cluster", cluster)
		return false, err
	}

	glog.Infoln("service exist", service)
	return true, nil
}

func (s *SwarmSvc) inspectService(ctx context.Context, cli *client.Client, service string) (*swarm.Service, error) {
	svc, _, err := cli.ServiceInspectWithRaw(ctx, service)
	if err != nil {
		if client.IsErrServiceNotFound(err) {
			glog.Infoln("service not exist", service)
			return nil, common.ErrNotFound
		}
		glog.Errorln("inspect service error", err, "service", service)
		return nil, err
	}
	glog.Infoln("inspect service", service, svc)
	return &svc, nil
}

// GetServiceStatus gets the service's status.
func (s *SwarmSvc) GetServiceStatus(ctx context.Context, cluster string, service string) (*common.ServiceStatus, error) {
	cli, err := s.newClient()
	if err != nil {
		glog.Errorln("GetServiceStatus newClient error", err, "service", service)
		return nil, err
	}

	svc, err := s.inspectService(ctx, cli, service)
	if err != nil {
		glog.Errorln("inspect service error", err, "service", service, "cluster", cluster)
		return nil, err
	}

	runningTaskCount, err := s.listRunningServiceTasks(ctx, cluster, service)
	if err != nil {
		glog.Errorln("listRunningServiceTasks error", err, "service", service)
		return nil, err
	}

	status := &common.ServiceStatus{
		RunningCount: runningTaskCount,
		DesiredCount: int64(*svc.Spec.Mode.Replicated.Replicas),
	}

	glog.Infoln("get service status", status, service)
	return status, nil
}

func (s *SwarmSvc) listRunningServiceTasks(ctx context.Context, cluster string, service string) (runningTaskCount int64, err error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add(filterName, service)
	filterArgs.Add(filterDesiredState, taskStateRunning)

	tasks, err := s.listTasks(filterArgs)
	if err != nil {
		glog.Errorln("listTasks error", err, "service", service)
		return 0, err
	}

	// swarm service task has 2 fields: Task.Status.State and Task.DesiredState.
	// The DesiredState is the desired state to distinguish the shutdown and running
	// tasks. When one task is scheduled to run, its DesiredState will be running.
	// When the task completes, the DesiredState will be shutdown.
	// The Status.State is the task's current state, such as pending, running, complete, etc.

	// check every task's Status.State.
	for _, task := range tasks {
		glog.V(1).Infoln("service", service, "task", task)
		if task.Status.State == swarm.TaskStateRunning {
			runningTaskCount++
		}
	}

	glog.Infoln("service", service, "has", runningTaskCount, "running tasks")
	return runningTaskCount, nil
}

// WaitServiceRunning waits till all tasks are running or time out.
func (s *SwarmSvc) WaitServiceRunning(ctx context.Context, cluster string, service string, replicas int64, maxWaitSeconds int64) error {
	return s.waitServiceRunningTasks(ctx, cluster, service, replicas, maxWaitSeconds)
}

func (s *SwarmSvc) waitServiceRunningTasks(ctx context.Context, cluster string,
	service string, replicas int64, maxWaitSeconds int64) error {
	filterArgs := filters.NewArgs()
	filterArgs.Add(filterName, service)
	filterArgs.Add(filterDesiredState, taskStateRunning)

	runningTasks := int64(0)
	for sec := int64(0); sec < maxWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		runningTasks = int64(0)
		tasks, err := s.listTasks(filterArgs)
		if err == nil {
			// swarm service task has 2 fields: Task.Status.State and Task.DesiredState.
			// The DesiredState is the desired state to distinguish the shutdown and running
			// tasks. When one task is scheduled to run, its DesiredState will be running.
			// When the task completes, the DesiredState will be shutdown.
			// The Status.State is the task's current state, such as pending, running, complete, etc.

			// check every task's Status.State.
			for _, task := range tasks {
				if task.Status.State == swarm.TaskStateRunning {
					runningTasks++
				}
			}

			if runningTasks == replicas {
				glog.Infoln("service", service, "has target running tasks", replicas)
				return nil
			}
			glog.Infoln("service has", runningTasks, "tasks running, expect", replicas, len(tasks))
		} else {
			glog.Errorln("listTasks error", err, "filterArgs", filterArgs)
		}

		time.Sleep(time.Duration(common.DefaultRetryWaitSeconds) * time.Second)
	}

	glog.Infoln("service", service, "has", runningTasks, "running tasks, expect",
		replicas, "after", maxWaitSeconds)
	return common.ErrTimeout
}

// StopService stops all service containers
func (s *SwarmSvc) StopService(ctx context.Context, cluster string, service string) error {
	cli, err := s.newClient()
	if err != nil {
		glog.Errorln("StopService newClient error", err, "service", service)
		return err
	}

	// get service spec
	swarmservice, err := s.inspectService(ctx, cli, service)
	if err != nil {
		glog.Errorln("inspectService error", err, "service", service)
		if client.IsErrServiceNotFound(err) {
			return nil
		}
		return err
	}

	// update service replicas to 0
	replicas := uint64(0)
	swarmservice.Spec.Mode.Replicated = &swarm.ReplicatedService{Replicas: &replicas}

	resp, err := cli.ServiceUpdate(ctx, service, swarmservice.Version, swarmservice.Spec, types.ServiceUpdateOptions{})
	if err != nil {
		glog.Errorln("ServiceUpdate error", err, "resp", resp, swarmservice)
		if client.IsErrServiceNotFound(err) {
			// this might be possible if someone else stops the service at the same time
			return nil
		}
		return err
	}

	glog.Infoln("updated service replicas to 0", service, "cluster", cluster)
	return nil
}

// DeleteService delets a swarm service
func (s *SwarmSvc) DeleteService(ctx context.Context, cluster string, service string) error {
	cli, err := s.newClient()
	if err != nil {
		glog.Errorln("DeleteService newClient error", err, "service", service)
		return err
	}

	err = cli.ServiceRemove(ctx, service)
	if err != nil {
		glog.Errorln("ServiceRemove error", err, "service", service)
		if client.IsErrServiceNotFound(err) {
			return nil
		}
		return err
	}

	glog.Infoln("deleted service", service, "cluster", cluster)
	return nil
}

// RunTask creates and runs the task once.
// It does 3 steps: 1) pull the image, 2) create the container, 3) start the container.
func (s *SwarmSvc) RunTask(ctx context.Context, opts *containersvc.RunTaskOptions) (taskID string, err error) {
	cli, err := s.newClient()
	if err != nil {
		glog.Errorln("DeleteService newClient error", err, "task", taskID)
		return "", err
	}

	// pull the container image
	respBody, err := cli.ImagePull(ctx, opts.Common.ContainerImage, types.ImagePullOptions{})
	if err != nil {
		glog.Errorln("ImagePull error", err, opts.Common)
		return "", err
	}
	// wait till image pull completes
	p := make([]byte, 1024)
	for true {
		_, err := respBody.Read(p)
		if err != nil {
			if err == io.EOF {
				glog.Infoln("ImagePull complete", opts.Common)
				respBody.Close()
				break
			}
			glog.Errorln("ImagePull read response error", err, opts.Common)
			respBody.Close()
			return "", err
		}
	}

	// create the container
	containerName := s.genTaskContainerName(opts.Common.Cluster, opts.Common.ServiceName, opts.TaskType)

	env := make([]string, len(opts.Envkvs))
	for i, e := range opts.Envkvs {
		env[i] = e.Name + envKVSep + e.Value
	}

	config := &container.Config{
		Hostname: containerName,
		Env:      env,
		Image:    opts.Common.ContainerImage,
	}

	hostConfig := &container.HostConfig{
		// Make sure the dns fields are never nil.
		// New containers don't ever have those fields nil,
		// but pre created containers can still have those nil values.
		// See https://github.com/docker/docker/pull/17779
		// for a more detailed explanation on why we don't want that.
		DNS:        make([]string, 0),
		DNSSearch:  make([]string, 0),
		DNSOptions: make([]string, 0),
		// LogConfig:      container.LogConfig{Type: copts.loggingDriver, Config: loggingOpts},
	}

	res := opts.Common.Resource
	if res != nil && (res.MaxMemMB != -1 || res.ReserveMemMB != -1 || res.MaxCPUUnits != -1 || res.ReserveCPUUnits != -1) {
		resources := container.Resources{
			CPUPeriod: defaultCPUPeriod,
		}
		if res.MaxMemMB != -1 {
			resources.Memory = s.memoryMB2Bytes(res.MaxMemMB)
		}
		if res.ReserveMemMB != -1 {
			resources.MemoryReservation = s.memoryMB2Bytes(res.ReserveMemMB)
		}
		if res.ReserveCPUUnits != -1 {
			resources.CPUShares = res.ReserveCPUUnits
		}
		if res.MaxCPUUnits != -1 {
			resources.CPUQuota = (defaultCPUPeriod * opts.Common.Resource.MaxCPUUnits) / defaultCPUUnitsPerCore
		}
		hostConfig.Resources = resources
	}

	netConfig := &network.NetworkingConfig{}

	// TODO the container may exist, if the previous task is not deleted.
	body, err := cli.ContainerCreate(ctx, config, hostConfig, netConfig, containerName)
	if err != nil {
		glog.Errorln("ContainerCreate error", err, config, hostConfig, opts.Common)
		return "", err
	}

	glog.Infoln("created container", body, opts.Common)

	// run the container
	options := types.ContainerStartOptions{}
	err = cli.ContainerStart(ctx, body.ID, options)
	if err != nil {
		glog.Errorln("ContainerStart error", err, "container id", body.ID, opts.Common)
		return body.ID, err
	}

	glog.Infoln("started container", body.ID, opts.Common)
	return body.ID, nil
}

// WaitTaskComplete waits till the task container completes
func (s *SwarmSvc) WaitTaskComplete(ctx context.Context, cluster string, taskID string, maxWaitSeconds int64) error {
	cli, err := s.newClient()
	if err != nil {
		glog.Errorln("WaitTaskComplete newClient error", err, "taskID", taskID)
		return err
	}

	var cjson types.ContainerJSON
	for sec := int64(0); sec < maxWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		cjson, err = cli.ContainerInspect(ctx, taskID)
		if err != nil {
			glog.Errorln("inspect container error", err, "containerID", taskID)
			if client.IsErrContainerNotFound(err) {
				return common.ErrNotFound
			}
			return err
		}

		if cjson.State.Status == taskStateRunning && cjson.State.Running {
			glog.Infoln("container", taskID, "is still running", cjson.State)
			time.Sleep(time.Duration(common.DefaultRetryWaitSeconds) * time.Second)
			continue
		}

		glog.Infoln("container", taskID, "is not running", cjson.State)
		return nil
	}

	glog.Errorln("container", taskID, "is still running after", maxWaitSeconds, cjson.State)
	return common.ErrTimeout
}

// GetTaskStatus returns the task's status.
func (s *SwarmSvc) GetTaskStatus(ctx context.Context, cluster string, taskID string) (*common.TaskStatus, error) {
	cli, err := s.newClient()
	if err != nil {
		glog.Errorln("newClient error", err, "taskID", taskID)
		return nil, err
	}

	cjson, err := cli.ContainerInspect(ctx, taskID)
	if err != nil {
		glog.Errorln("inspect container error", err, "containerID", taskID)
		if client.IsErrContainerNotFound(err) {
			return nil, common.ErrNotFound
		}
		return nil, err
	}

	taskStatus := &common.TaskStatus{
		Status:        cjson.State.Status,
		StoppedReason: cjson.State.Error,
		StartedAt:     cjson.State.StartedAt,
		FinishedAt:    cjson.State.FinishedAt,
	}

	glog.Infoln("get task", taskID, "status", taskStatus)
	return taskStatus, nil
}

// DeleteTask deletes the task container
func (s *SwarmSvc) DeleteTask(ctx context.Context, cluster string, service string, taskType string) error {
	containerName := s.genTaskContainerName(cluster, service, taskType)

	cli, err := s.newClient()
	if err != nil {
		glog.Errorln("DeleteTask newClient error", err, containerName)
		return err
	}

	err = cli.ContainerRemove(ctx, containerName, types.ContainerRemoveOptions{})
	if err != nil {
		glog.Errorln("remove container", containerName, "error", err, containerName)
		if client.IsErrContainerNotFound(err) {
			return nil
		}
		return err
	}

	glog.Infoln("removed container", containerName)
	return nil
}

func (s *SwarmSvc) genTaskContainerName(cluster string, service string, taskType string) string {
	return cluster + common.NameSeparator + service + common.NameSeparator + taskType
}

func (s *SwarmSvc) newClient() (*client.Client, error) {
	var httpclient *http.Client
	if s.tlsVerify {
		options := tlsconfig.Options{
			CAFile:   s.caFile,
			CertFile: s.certFile,
			KeyFile:  s.keyFile,
			//InsecureSkipVerify: s.tlsVerify,
		}
		tlsc, err := tlsconfig.Client(options)
		if err != nil {
			glog.Errorln("tlsconfig.Client error", err)
			return nil, err
		}

		httpclient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsc,
			},
		}
	}

	return client.NewClient(s.managerAddrs[0], client.DefaultVersion, httpclient, nil)
}

func (s *SwarmSvc) memoryMB2Bytes(size int64) int64 {
	return size * 1024 * 1024
}
