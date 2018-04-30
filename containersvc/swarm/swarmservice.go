package swarmsvc

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	mounttypes "github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"golang.org/x/net/context"

	"github.com/golang/glog"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	managecli "github.com/cloudstax/firecamp/manage/client"
)

const (
	// service task filters. see the supported filter at
	// https://docs.docker.com/engine/reference/commandline/service_ps/#filtering
	filterID               = "id"
	filterName             = "name"
	filterNode             = "node"
	filterRole             = "role"
	filterDesiredState     = "desired-state"
	taskStateRunning       = "running"
	nanoCPUs               = 1000000000
	defaultCPUPeriod       = 10000
	defaultCPUUnitsPerCore = 1024
	envKVSep               = "="

	taskSlotEnvKV = "TASK_SLOT={{.Task.Slot}}"

	zoneLabelSep       = "."
	placeConstraintFmt = "engine.labels.%s == true"

	restartWaitSeconds = 5
)

// SwarmSvc implements swarm service and task related functions.
//
// TODO task framework on Swarm.
// Swarm doesn't support the task execution. The SwarmSvc will have to manage the task
// lifecycle. The Docker daemon on the swarm worker node will have to listen on the
// host port, so docker API could be accessed remotely. The SwarmSvc will periodically
// collect the metrics, select one node, store it in db. If the node is full,
// select another node to run the task.
// At v1, the task is simply run on the swarm manager node. This is not a big issue, as
// the task would usually run some simple job, such as setup the MongoDB ReplicaSet.
type SwarmSvc struct {
	cli *SwarmClient
	// the availability zones the cluster have
	azs []string

	// One for the container running on the swarm manager nodes, such as firecamp manage server.
	// The other for the container running on the swarm worker nodes, such as firecamp volume plugin.
	// The container on the swarm worker nodes could not directly talk with the swarm manager,
	// as the swarm manager is protected with the internal tls by default. Has to talk with the manageserver
	// via the internal requests.
	// TODO better separate 2 ContainerSvc interfaces?
	workerNode bool

	region  string
	cluster string
	mgtCli  *managecli.ManageClient
}

// NewSwarmSvcOnManagerNode creates a new SwarmSvc instance on the manager node
func NewSwarmSvcOnManagerNode(azs []string) (*SwarmSvc, error) {
	cli, err := NewSwarmClient()
	if err != nil {
		return nil, err
	}

	s := &SwarmSvc{
		cli:        cli,
		azs:        azs,
		workerNode: false,
	}

	return s, nil
}

// NewSwarmSvcOnWorkerNode creates a new SwarmSvc instance on the worker node for such as volume plugin.
func NewSwarmSvcOnWorkerNode(region string, cluster string) (*SwarmSvc, error) {
	serverURL := dns.GetDefaultManageServiceURL(cluster, false)
	mgtCli := managecli.NewManageClient(serverURL, nil)

	s := &SwarmSvc{
		cli:        nil,
		workerNode: true,
		region:     region,
		cluster:    cluster,
		mgtCli:     mgtCli,
	}

	return s, nil
}

// GetContainerSvcType gets the containersvc type.
func (s *SwarmSvc) GetContainerSvcType() string {
	return common.ContainerPlatformSwarm
}

// CreateService creates a swarm service
func (s *SwarmSvc) CreateService(ctx context.Context, opts *containersvc.CreateServiceOptions) error {
	serviceSpec := s.CreateServiceSpec(opts)
	return s.CreateSwarmService(ctx, serviceSpec, opts)
}

// CreateSwarmService creates the swarm service.
func (s *SwarmSvc) CreateSwarmService(ctx context.Context, serviceSpec swarm.ServiceSpec, opts *containersvc.CreateServiceOptions) error {
	cli, err := s.cli.NewClient()
	if err != nil {
		glog.Errorln("CreateService newClient error", err, "options", opts)
		return err
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

// CreateServiceSpec creates the swarm ServiceSpec.
func (s *SwarmSvc) CreateServiceSpec(opts *containersvc.CreateServiceOptions) swarm.ServiceSpec {
	var mounts []mounttypes.Mount
	if opts.DataVolume != nil {
		// The Task.Slot in the volume name does not work on the multiple zones cluster,
		// as task slot is not aware of zone.
		// Hit one bug: swarm is possible to schedule 2 tasks to one node, even the container exposes
		// the same ports. using Task.Slot could cause the corner case:
		// 1. swarm schedules task 1 and 2 on node1 around the same time.
		// 2. task1 mounts the service volume1, task2 increases the volume refs instead of mounting volume2.
		// 3. task2 binds the port first, so task1 will fail as service could not bind the same port.
		// 4. task1 shutdown will decrease the volume refs.
		// 5. task1 is rescheduled to node2. node2 will try to mount volume1 for task1, which will fail
		//    as volume1 is still mounted on node1.
		// don't want to make the volume plugin more complex. simply disable the Task.Slot.
		source := opts.Common.ServiceUUID
		//if len(s.azs) == 1 {
		// enable Task.Slot for the single zone cluster.
		// TODO revisit it when support adding a new zone.
		//source = containersvc.GenVolumeSourceForSwarm(opts.Common.ServiceUUID)
		//}

		mount := mounttypes.Mount{
			Type:     mounttypes.TypeVolume,
			Source:   source,
			Target:   opts.DataVolume.MountPath,
			ReadOnly: false,
			VolumeOptions: &mounttypes.VolumeOptions{
				DriverConfig: &mounttypes.Driver{Name: common.VolumeDriverName},
			},
		}
		mounts = []mounttypes.Mount{mount}
	}

	if opts.JournalVolume != nil {
		// The Task.Slot in the volume name does not work on the multiple zones cluster,
		// as task slot is not aware of zone.
		source := containersvc.GetServiceJournalVolumeName(opts.Common.ServiceUUID)
		//if len(s.azs) == 1 {
		// enable Task.Slot for the single zone cluster.
		// TODO revisit it when support adding a new zone.
		//	source = containersvc.GenVolumeSourceForSwarm(source)
		//}

		mount := mounttypes.Mount{
			Type:     mounttypes.TypeVolume,
			Source:   source,
			Target:   opts.JournalVolume.MountPath,
			ReadOnly: false,
			VolumeOptions: &mounttypes.VolumeOptions{
				DriverConfig: &mounttypes.Driver{Name: common.VolumeDriverName},
			},
		}
		mounts = append(mounts, mount)
	}

	taskTemplate := swarm.TaskSpec{
		ContainerSpec: swarm.ContainerSpec{
			Image:     opts.Common.ContainerImage,
			Mounts:    mounts,
			DNSConfig: &swarm.DNSConfig{},
		},
		// docker swarm service does not allow "host" network. By default, "bridge" will be used.
		//Networks: []swarm.NetworkAttachmentConfig{
		//	swarm.NetworkAttachmentConfig{Target: "host"},
		//},
		Resources: &swarm.ResourceRequirements{
			Limits:       &swarm.Resources{},
			Reservations: &swarm.Resources{},
		},
		RestartPolicy: &swarm.RestartPolicy{
			Condition: swarm.RestartPolicyConditionAny,
		},
		Placement: &swarm.Placement{},
		LogDriver: &swarm.Driver{
			Name:    opts.Common.LogConfig.Name,
			Options: opts.Common.LogConfig.Options,
		},
	}

	// set -e TASK_SLOT={{.Task.Slot}} for service, to get the service member inside container.
	// TODO this is currently used for the stateless service. For the stateful service, could
	// also utilize it after the volume plugin is enhanced to support Task.Slot.
	env := make([]string, len(opts.Envkvs)+1)
	env[0] = taskSlotEnvKV
	for i, e := range opts.Envkvs {
		env[i+1] = e.Name + envKVSep + e.Value
	}

	taskTemplate.ContainerSpec.Env = env

	if opts.Place != nil {
		// the service aims to run on the specified availability zones.
		// docker node label could only be added on the manager node. This is not easy to automate.
		// The worker node needs to ask the manager service to add the node label.
		// So we use the engine label. During the cluster creation, the availability zone labels
		// will be created for the docker engine.
		// https://docs.docker.com/engine/swarm/services/#control-service-scale-and-placement
		// https://docs.docker.com/engine/reference/commandline/service_create/#specify-service-constraints-constraint
		// https://docs.docker.com/engine/swarm/manage-nodes/#add-or-remove-label-metadata

		sort.Slice(opts.Place.Zones, func(i, j int) bool {
			return opts.Place.Zones[i] < opts.Place.Zones[j]
		})

		zoneLabel := ""
		for i, zone := range opts.Place.Zones {
			if i == 0 {
				zoneLabel = zone
			} else {
				zoneLabel += zoneLabelSep + zone
			}
		}

		placeConstraint := fmt.Sprintf(placeConstraintFmt, zoneLabel)

		taskTemplate.Placement = &swarm.Placement{
			Constraints: []string{placeConstraint},
		}

		glog.Infoln("placement constraints", placeConstraint, "for service", opts.Common)
	}

	if opts.Common.Resource != nil {
		if opts.Common.Resource.MaxCPUUnits != common.DefaultMaxCPUUnits || opts.Common.Resource.MaxMemMB != common.DefaultMaxMemoryMB {
			if opts.Common.Resource.MaxCPUUnits != common.DefaultMaxCPUUnits {
				taskTemplate.Resources.Limits.NanoCPUs = s.cpuUnits2NanoCPUs(opts.Common.Resource.MaxCPUUnits)
			}
			if opts.Common.Resource.MaxMemMB != common.DefaultMaxMemoryMB {
				taskTemplate.Resources.Limits.MemoryBytes = s.memoryMB2Bytes(opts.Common.Resource.MaxMemMB)
			}
		}
		if opts.Common.Resource.ReserveCPUUnits != common.DefaultMaxCPUUnits || opts.Common.Resource.ReserveMemMB != common.DefaultMaxMemoryMB {
			if opts.Common.Resource.ReserveCPUUnits != common.DefaultMaxCPUUnits {
				taskTemplate.Resources.Reservations.NanoCPUs = s.cpuUnits2NanoCPUs(opts.Common.Resource.ReserveCPUUnits)
			}
			if opts.Common.Resource.ReserveMemMB != common.DefaultMaxMemoryMB {
				taskTemplate.Resources.Reservations.MemoryBytes = s.memoryMB2Bytes(opts.Common.Resource.ReserveMemMB)
			}
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
		// set the UpdateConfig. The rolling restart triggers the force update.
		// sometimes, one container may fail to start. For example, EBS detach takes long time,
		// and cause the container start failed. Swarm will pause the rolling update
		// as swarm.UpdateFailureActionPause is the default behavior.
		// Set the default behavior as continue to avoid this.
		UpdateConfig: &swarm.UpdateConfig{
			Parallelism:   1,
			FailureAction: swarm.UpdateFailureActionContinue,
			Order:         swarm.UpdateOrderStopFirst,
		},
	}

	if len(opts.PortMappings) != 0 {
		ports := make([]swarm.PortConfig, len(opts.PortMappings))
		for i, p := range opts.PortMappings {
			ports[i] = swarm.PortConfig{
				Protocol:      swarm.PortConfigProtocolTCP,
				TargetPort:    uint32(p.ContainerPort),
				PublishedPort: uint32(p.HostPort),
				PublishMode:   swarm.PortConfigPublishModeHost,
			}
		}

		epSpec := &swarm.EndpointSpec{
			Ports: ports,
		}
		serviceSpec.EndpointSpec = epSpec
	}

	return serviceSpec
}

func (s *SwarmSvc) cpuUnits2NanoCPUs(cpuUnits int64) int64 {
	return (cpuUnits * nanoCPUs) / defaultCPUUnitsPerCore
}

// ListActiveServiceTasks lists the active (running and pending) tasks of the service
func (s *SwarmSvc) ListActiveServiceTasks(ctx context.Context, cluster string, service string) (serviceTaskIDs map[string]bool, err error) {
	if s.workerNode {
		// ListActiveServiceTasks from the manage server running on the swarm manager node
		r := &manage.InternalListActiveServiceTasksRequest{
			Region:      s.region,
			Cluster:     s.cluster,
			ServiceName: service,
		}

		serviceTaskIDs, err = s.mgtCli.InternalListActiveServiceTasks(ctx, r)
		if err != nil {
			glog.Errorln("InternalListActiveServiceTasks error", err, "cluster", cluster, "service", service)
		} else {
			glog.Errorln("InternalListActiveServiceTasks ", len(serviceTaskIDs), "cluster", cluster, "service", service)
		}
		return serviceTaskIDs, err
	}

	filterArgs := filters.NewArgs()
	filterArgs.Add(filterName, service)
	filterArgs.Add(filterDesiredState, taskStateRunning)

	cli, err := s.cli.NewClient()
	if err != nil {
		glog.Errorln("ListActiveServiceTasks.cli.NewClient error", err)
		return nil, err
	}

	tasks, err := s.listTasks(cli, filterArgs)
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
	if s.workerNode {
		// GetServiceTask from the manage server running on the swarm manager node
		r := &manage.InternalGetServiceTaskRequest{
			Region:              s.region,
			Cluster:             s.cluster,
			ServiceName:         service,
			ContainerInstanceID: containerInstanceID,
		}

		serviceTaskID, err = s.mgtCli.InternalGetServiceTask(ctx, r)
		if err != nil {
			glog.Errorln("InternalGetServiceTask error", err, "cluster", cluster,
				"service", service, "container instance", containerInstanceID)
		} else {
			glog.Errorln("InternalGetServiceTask taskID", serviceTaskID, "cluster", cluster,
				"service", service, "container instance", containerInstanceID)
		}
		return serviceTaskID, err
	}

	filterArgs := filters.NewArgs()
	filterArgs.Add(filterName, service)
	filterArgs.Add(filterDesiredState, taskStateRunning)
	filterArgs.Add(filterNode, containerInstanceID)

	cli, err := s.cli.NewClient()
	if err != nil {
		glog.Errorln("GetServiceTask newClient error", err)
		return "", err
	}

	tasks, err := s.listTasks(cli, filterArgs)
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
	cli, err := s.cli.NewClient()
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

func (s *SwarmSvc) listTasks(cli *client.Client, filterArgs filters.Args) ([]swarm.Task, error) {
	listOps := types.TaskListOptions{
		Filters: filterArgs,
	}

	tasks, err := cli.TaskList(context.Background(), listOps)
	glog.Infoln("Swarm TaskList filterArgs", filterArgs, "error", err, "tasks", len(tasks))
	return tasks, err
}

// IsServiceExist checks whether the service exists
func (s *SwarmSvc) IsServiceExist(ctx context.Context, cluster string, service string) (bool, error) {
	cli, err := s.cli.NewClient()
	if err != nil {
		glog.Errorln("IsServiceExists.cli.NewClient error", err, "service", service)
		return false, err
	}

	_, err = s.inspectService(ctx, cli, service)
	if err != nil {
		if err == common.ErrNotFound {
			return false, nil
		}
		glog.Errorln("inspect service error", err, "service", service, "cluster", cluster)
		return false, err
	}

	glog.Infoln("service exist", service)
	return true, nil
}

func (s *SwarmSvc) inspectService(ctx context.Context, cli *client.Client, service string) (*swarm.Service, error) {
	svc, _, err := cli.ServiceInspectWithRaw(ctx, service, types.ServiceInspectOptions{})
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
	cli, err := s.cli.NewClient()
	if err != nil {
		glog.Errorln("GetServiceStatus.cli.NewClient error", err, "service", service)
		return nil, err
	}

	svc, err := s.inspectService(ctx, cli, service)
	if err != nil {
		glog.Errorln("inspect service error", err, "service", service, "cluster", cluster)
		return nil, err
	}

	runningTaskCount, err := s.listRunningServiceTasks(ctx, cli, cluster, service)
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

func (s *SwarmSvc) listRunningServiceTasks(ctx context.Context, cli *client.Client, cluster string, service string) (runningTaskCount int64, err error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add(filterName, service)
	filterArgs.Add(filterDesiredState, taskStateRunning)

	tasks, err := s.listTasks(cli, filterArgs)
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
	cli, err := s.cli.NewClient()
	if err != nil {
		glog.Errorln("WaitServiceRunning newClient error", err)
		return err
	}

	return s.waitServiceRunningTasks(ctx, cli, cluster, service, replicas, maxWaitSeconds)
}

func (s *SwarmSvc) waitServiceRunningTasks(ctx context.Context, cli *client.Client,
	cluster string, service string, replicas int64, maxWaitSeconds int64) error {
	filterArgs := filters.NewArgs()
	filterArgs.Add(filterName, service)
	filterArgs.Add(filterDesiredState, taskStateRunning)

	runningTasks := int64(0)
	for sec := int64(0); sec < maxWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		runningTasks = int64(0)
		tasks, err := s.listTasks(cli, filterArgs)
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

func (s *SwarmSvc) updateServiceReplicas(ctx context.Context, cli *client.Client, cluster string, service string, replicas uint64) error {
	// get service spec
	swarmservice, err := s.inspectService(ctx, cli, service)
	if err != nil {
		glog.Errorln("inspectService error", err, "service", service)
		return err
	}

	// stop service, update service replicas to 0
	swarmservice.Spec.Mode.Replicated = &swarm.ReplicatedService{Replicas: &replicas}

	resp, err := cli.ServiceUpdate(ctx, service, swarmservice.Version, swarmservice.Spec, types.ServiceUpdateOptions{})
	if err != nil {
		glog.Errorln("ServiceUpdate error", err, "resp", resp, swarmservice)
		return err
	}

	glog.Infoln("updated service replicas to", replicas, service)
	return nil
}

// UpdateService updates the service
func (s *SwarmSvc) UpdateService(ctx context.Context, opts *containersvc.UpdateServiceOptions) error {
	cli, err := s.cli.NewClient()
	if err != nil {
		glog.Errorln("UpdateService newClient error", err, "service", opts.ServiceName)
		return err
	}

	// get service spec
	svc, err := s.inspectService(ctx, cli, opts.ServiceName)
	if err != nil {
		glog.Errorln("inspectService error", err, "service", opts.ServiceName)
		return err
	}

	if opts.ReserveMemMB != nil {
		glog.Infoln("update reserve memory to", *opts.ReserveMemMB, opts.ServiceName)
		svc.Spec.TaskTemplate.Resources.Reservations.MemoryBytes = s.memoryMB2Bytes(*opts.ReserveMemMB)
	}
	if opts.MaxMemMB != nil {
		glog.Infoln("update max memory to", *opts.MaxMemMB, opts.ServiceName)
		svc.Spec.TaskTemplate.Resources.Limits.MemoryBytes = s.memoryMB2Bytes(*opts.MaxMemMB)
	}
	if opts.ReserveCPUUnits != nil {
		glog.Infoln("update reserve cpu to", *opts.ReserveCPUUnits, opts.ServiceName)
		svc.Spec.TaskTemplate.Resources.Reservations.NanoCPUs = s.cpuUnits2NanoCPUs(*opts.ReserveCPUUnits)
	}
	if opts.MaxCPUUnits != nil {
		glog.Infoln("update max cpu to", *opts.MaxCPUUnits, opts.ServiceName)
		svc.Spec.TaskTemplate.Resources.Limits.NanoCPUs = s.cpuUnits2NanoCPUs(*opts.MaxCPUUnits)
	}

	if len(opts.PortMappings) != 0 {
		glog.Infoln("update port mappings", opts.PortMappings, opts.ServiceName)

		ports := make([]swarm.PortConfig, len(opts.PortMappings))
		for i, p := range opts.PortMappings {
			ports[i] = swarm.PortConfig{
				Protocol:      swarm.PortConfigProtocolTCP,
				TargetPort:    uint32(p.ContainerPort),
				PublishedPort: uint32(p.HostPort),
				PublishMode:   swarm.PortConfigPublishModeHost,
			}
		}

		epSpec := &swarm.EndpointSpec{
			Ports: ports,
		}
		svc.Spec.EndpointSpec = epSpec
	}

	if len(opts.ReleaseVersion) != 0 {
		glog.Infoln("update volume and log driver version to", common.Version, opts.ServiceName)

		// update the volume driver version
		for _, m := range svc.Spec.TaskTemplate.ContainerSpec.Mounts {
			m.VolumeOptions.DriverConfig.Name = common.VolumeDriverName
		}

		// update the log driver version
		svc.Spec.TaskTemplate.LogDriver.Name = common.LogDriverName
	}

	// TODO control the service restart. Swarm UpdateService will automatically restart the service containers.
	resp, err := cli.ServiceUpdate(ctx, opts.ServiceName, svc.Version, svc.Spec, types.ServiceUpdateOptions{})
	if err != nil {
		glog.Errorln("ServiceUpdate error", err, "resp", resp, svc)
		return err
	}

	glog.Infoln("update service success", opts.ServiceName)
	return nil
}

// StopService stops all service containers
func (s *SwarmSvc) StopService(ctx context.Context, cluster string, service string) error {
	cli, err := s.cli.NewClient()
	if err != nil {
		glog.Errorln("StopService newClient error", err, "service", service)
		return err
	}

	// stop service, update service replicas to 0
	err = s.updateServiceReplicas(ctx, cli, cluster, service, 0)
	if err != nil {
		// this might be possible if someone else deletes the service at the same time
		if client.IsErrServiceNotFound(err) {
			return nil
		}

		glog.Errorln("updateServiceReplicas error", err, service)
		return err
	}

	glog.Infoln("stopped service", service, "cluster", cluster)
	return nil
}

// ScaleService scales the service containers up/down to the desiredCount.
func (s *SwarmSvc) ScaleService(ctx context.Context, cluster string, service string, desiredCount int64) error {
	cli, err := s.cli.NewClient()
	if err != nil {
		glog.Errorln("ScaleService newClient error", err, "service", service)
		return err
	}

	// start service, update service replicas to DesiredCount
	err = s.updateServiceReplicas(ctx, cli, cluster, service, uint64(desiredCount))
	if err != nil {
		glog.Errorln("update service error", err)
		return err
	}

	glog.Infoln("ScaleService complete", service, "desiredCount", desiredCount)
	return nil
}

// DeleteService delets a swarm service
func (s *SwarmSvc) DeleteService(ctx context.Context, cluster string, service string) error {
	cli, err := s.cli.NewClient()
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

// RollingRestartService restarts the service tasks one after the other.
func (s *SwarmSvc) RollingRestartService(ctx context.Context, cluster string, service string, opts *containersvc.RollingRestartOptions) error {
	cli, err := s.cli.NewClient()
	if err != nil {
		glog.Errorln("DeleteService newClient error", err, "service", service)
		return err
	}

	// get service spec
	svc, err := s.inspectService(ctx, cli, service)
	if err != nil {
		glog.Errorln("inspectService error", err, "service", service)
		return err
	}

	// force update service. swarm will restart one service container, wait the container running
	// and then restart the next container.
	svc.Spec.TaskTemplate.ForceUpdate++
	if svc.Spec.UpdateConfig != nil && svc.Spec.UpdateConfig.FailureAction == swarm.UpdateFailureActionPause {
		// skip the possible error during the rolling restart
		svc.Spec.UpdateConfig.FailureAction = swarm.UpdateFailureActionContinue
	}

	resp, err := cli.ServiceUpdate(ctx, service, svc.Version, svc.Spec, types.ServiceUpdateOptions{})
	if err != nil {
		glog.Errorln("ServiceUpdate error", err, "resp", resp, svc)
		return err
	}

	opts.StatusMessage = "swarm service is updated to trigger the rolling restart"

	glog.Infoln("force swarm service updated", service)

	// wait till all containers are restarted.
	for sec := int64(0); sec < common.DefaultServiceWaitSeconds*opts.Replicas; sec += restartWaitSeconds {
		svc, err = s.inspectService(ctx, cli, service)
		if err != nil {
			glog.Errorln("inspectService error", err, "service", service)
			return err
		}

		if svc.UpdateStatus != nil {
			if svc.UpdateStatus.State == swarm.UpdateStateCompleted {
				glog.Infoln("service rolling restart complete", service, svc.UpdateStatus)
				return nil
			}

			if svc.UpdateStatus.State != swarm.UpdateStateUpdating {
				errmsg := fmt.Sprintf("service %s is not at updating state, %s", service, svc.UpdateStatus)
				glog.Errorln(errmsg)
				return errors.New(errmsg)
			}

			opts.StatusMessage = svc.UpdateStatus.Message

			glog.Infoln("service update status", service, svc.UpdateStatus)
		}

		glog.Infoln("service is still at the updating state", service)

		time.Sleep(time.Duration(restartWaitSeconds) * time.Second)
	}

	glog.Errorln("service rolling restart timed out after", common.DefaultServiceWaitSeconds*opts.Replicas, "service", service)
	return errors.New("service rolling restart timed out")
}

// RunTask creates and runs the task once.
// It does 3 steps: 1) pull the image, 2) create the container, 3) start the container.
func (s *SwarmSvc) RunTask(ctx context.Context, opts *containersvc.RunTaskOptions) (taskID string, err error) {
	cli, err := s.cli.NewClient()
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

		LogConfig: container.LogConfig{
			Type:   opts.Common.LogConfig.Name,
			Config: opts.Common.LogConfig.Options,
		},
	}

	res := opts.Common.Resource
	if res != nil && (res.MaxMemMB != common.DefaultMaxMemoryMB || res.ReserveMemMB != common.DefaultMaxMemoryMB || res.MaxCPUUnits != common.DefaultMaxCPUUnits || res.ReserveCPUUnits != common.DefaultMaxCPUUnits) {
		resources := container.Resources{
			CPUPeriod: defaultCPUPeriod,
		}
		if res.MaxMemMB != common.DefaultMaxMemoryMB {
			resources.Memory = s.memoryMB2Bytes(res.MaxMemMB)
		}
		if res.ReserveMemMB != common.DefaultMaxMemoryMB {
			resources.MemoryReservation = s.memoryMB2Bytes(res.ReserveMemMB)
		}
		if res.ReserveCPUUnits != common.DefaultMaxCPUUnits {
			resources.CPUShares = res.ReserveCPUUnits
		}
		if res.MaxCPUUnits != common.DefaultMaxCPUUnits {
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
	cli, err := s.cli.NewClient()
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
	cli, err := s.cli.NewClient()
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

	// for the status string, see docker code container/state.go
	status := common.TaskStatusRunning
	if cjson.State.Status == "exited" ||
		cjson.State.Status == "dead" ||
		cjson.State.Status == "removing" ||
		cjson.State.Dead {
		glog.Infoln("task", taskID, "is stopped, state", cjson.State)
		status = common.TaskStatusStopped
	}

	taskStatus := &common.TaskStatus{
		Status:        status,
		StoppedReason: cjson.State.Error,
		StartedAt:     cjson.State.StartedAt,
		FinishedAt:    cjson.State.FinishedAt,
	}

	glog.Infoln("get task", taskID, "status", status, "State", cjson.State)
	return taskStatus, nil
}

// DeleteTask deletes the task container
func (s *SwarmSvc) DeleteTask(ctx context.Context, cluster string, service string, taskType string) error {
	containerName := s.genTaskContainerName(cluster, service, taskType)

	cli, err := s.cli.NewClient()
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

func (s *SwarmSvc) memoryMB2Bytes(size int64) int64 {
	return size * 1024 * 1024
}

// IsSwarmInitialized checks if the swarm cluster is initialized.
func (s *SwarmSvc) IsSwarmInitialized(ctx context.Context) (bool, error) {
	cli, err := s.cli.NewClient()
	if err != nil {
		glog.Errorln("IsSwarmInitialized newClient error", err)
		return false, err
	}

	swarm, err := cli.SwarmInspect(ctx)
	if err != nil {
		// if swarm is not initialized, SwarmInspect will return error.
		// didn't find the corresponding error code, simply take error as not initialized.
		glog.Errorln("SwarmInspect error", err)
		return false, nil
	}

	glog.Infoln("SwarmInspect", swarm)
	return true, nil
}

// SwarmInit initializes the swarm cluster.
func (s *SwarmSvc) SwarmInit(ctx context.Context, addr string) error {
	cli, err := s.cli.NewClient()
	if err != nil {
		glog.Errorln("SwarmInit newClient error", err)
		return err
	}

	req := swarm.InitRequest{
		ListenAddr:    addr,
		AdvertiseAddr: addr,
		Availability:  swarm.NodeAvailabilityActive,
	}

	nodeID, err := cli.SwarmInit(ctx, req)
	if err != nil {
		// if swarm is not initialized, SwarmInspect will return error.
		// didn't find the corresponding error code, simply take error as not initialized.
		glog.Errorln("SwarmInit error", err, "addr", addr)
		return err
	}

	glog.Infoln("SwarmInit done, ndoeID", nodeID, addr)
	return nil
}

// GetJoinToken gets the swarm manager and worker node join token.
func (s *SwarmSvc) GetJoinToken(ctx context.Context) (managerToken string, workerToken string, err error) {
	cli, err := s.cli.NewClient()
	if err != nil {
		glog.Errorln("GetJoinToken newClient error", err)
		return "", "", err
	}

	swarm, err := cli.SwarmInspect(ctx)
	if err != nil {
		glog.Errorln("SwarmInspect error", err)
		return "", "", err
	}

	glog.Infoln("SwarmInspect", swarm.ClusterInfo, swarm.JoinTokens)
	return swarm.JoinTokens.Manager, swarm.JoinTokens.Worker, nil
}

// SwarmJoin joins the current node to the remote manager.
func (s *SwarmSvc) SwarmJoin(ctx context.Context, addr string, joinAddr string, token string) error {
	cli, err := s.cli.NewClient()
	if err != nil {
		glog.Errorln("SwarmJoin newClient error", err)
		return err
	}

	req := swarm.JoinRequest{
		ListenAddr:    addr,
		AdvertiseAddr: addr,
		RemoteAddrs:   []string{joinAddr},
		JoinToken:     token,
		Availability:  swarm.NodeAvailabilityActive,
	}

	err = cli.SwarmJoin(ctx, req)
	if err != nil {
		glog.Errorln("SwarmJoin error", err, "join manager", joinAddr, "local addr", addr)
		return err
	}

	glog.Infoln("joined manager", joinAddr, "local addr", addr)
	return nil
}

// ListSwarmManagerNodes returns the good and down managers
func (s *SwarmSvc) ListSwarmManagerNodes(ctx context.Context) (goodManagers []string, downManagerNodes []swarm.Node, downManagers []string, err error) {
	cli, err := s.cli.NewClient()
	if err != nil {
		glog.Errorln("ListSwarmManagerNodes newClient error", err)
		return nil, nil, nil, err
	}

	filterArgs := filters.NewArgs()
	filterArgs.Add(filterRole, string(swarm.NodeRoleManager))

	opts := types.NodeListOptions{
		Filters: filterArgs,
	}

	nodes, err := cli.NodeList(ctx, opts)
	if err != nil {
		glog.Errorln("NodeList error", err)
		return nil, nil, nil, err
	}

	for _, n := range nodes {
		if n.Spec.Role != swarm.NodeRoleManager || n.ManagerStatus == nil {
			errmsg := fmt.Sprintf("internal error - swarm list manager node returns worker node %s %s, spec %s", n.ID, n.Status.Addr, n.Spec)
			glog.Errorln(errmsg)
			return nil, nil, nil, errors.New(errmsg)
		}
		if n.Status.State == swarm.NodeStateDown && n.ManagerStatus.Reachability == swarm.ReachabilityUnreachable {
			glog.Errorln("manager node is down", n.ManagerStatus, n)
			downManagers = append(downManagers, n.ManagerStatus.Addr)
			downManagerNodes = append(downManagerNodes, n)
		} else {
			glog.Infoln("manager node is good", n.ManagerStatus, n)
			goodManagers = append(goodManagers, n.ManagerStatus.Addr)
		}
	}

	return goodManagers, downManagerNodes, downManagers, nil
}

// RemoveDownManagerNode removes the down manager node.
func (s *SwarmSvc) RemoveDownManagerNode(ctx context.Context, node swarm.Node) error {
	// sanity check
	if node.Spec.Role != swarm.NodeRoleManager || node.ManagerStatus == nil {
		errmsg := fmt.Sprintf("Node %s %s is not a manager node, spec %s", node.ID, node.Status.Addr, node.Spec)
		glog.Errorln(errmsg)
		return errors.New(errmsg)
	}
	if node.ManagerStatus.Reachability != swarm.ReachabilityUnreachable {
		errmsg := fmt.Sprintf("manager node %s %s is not at unreachable status, %s", node.ID, node.Status.Addr, node.ManagerStatus)
		glog.Errorln(errmsg)
		return errors.New(errmsg)
	}

	cli, err := s.cli.NewClient()
	if err != nil {
		glog.Errorln("RemoveDownManagerNode newClient error", err)
		return err
	}

	// demote the manager node. Swarm does not allow to remove the down manager node directly.
	node.Spec.Role = swarm.NodeRoleWorker
	err = cli.NodeUpdate(ctx, node.ID, node.Version, node.Spec)
	if err != nil {
		// TODO check the update error code. it may be possible another node just demoted this node.
		// this is possible if the cluster has 5 manager nodes and 2 nodes happen to go down around the same time.
		// if this corner case happens, go ahead to remove the node from cluster. While, it does not harm
		// to simply leave a down node in the cluster.
		glog.Errorln("RemoveDownManager NodeUpdate error", err, node)
		return err
	}

	glog.Infoln("demoted down manager node", node)

	// remove the node
	err = cli.NodeRemove(ctx, node.ID, types.NodeRemoveOptions{})
	if err != nil {
		glog.Errorln("remove node error", err, node)
		return err
	}

	glog.Infoln("removed the down manager node", node.ID, node.Status.Addr)
	return nil
}

// CreateServiceVolume is a non-op for swarm.
func (s *SwarmSvc) CreateServiceVolume(ctx context.Context, service string, memberName string, volumeID string, volumeSizeGB int64, journal bool) (existingVolumeID string, err error) {
	return "", nil
}

// DeleteServiceVolume is a non-op for swarm.
func (s *SwarmSvc) DeleteServiceVolume(ctx context.Context, service string, memberName string, journal bool) error {
	return nil
}
