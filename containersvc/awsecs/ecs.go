package awsecs

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/containersvc"
)

const (
	// ecs does not allow 100 MinimumHealthyPercent and 100 MaximumPercent,
	// which will block the deployment.
	// for a typical 3 replicas db deployments, 65% allows 1 replica is down.
	minHealthyPercent = 65
	// not allow more replicas
	maxHealthyPercent = 100

	taskFamilyRevisionSep = ":"
	tcpProtocol           = "tcp"
	taskDefStatusActive   = "ACTIVE"

	serviceStatusActive   = "ACTIVE"
	serviceStatusDraining = "DRAINING"
	serviceStatusInactive = "INACTIVE"

	taskStatusPending = "PENDING"
	taskStatusRunning = "RUNNING"
	taskStatusStopped = "STOPPED"

	serviceNotFoundException  = "ServiceNotFoundException"
	serviceNotActiveException = "ServiceNotActiveException"
)

// AWSEcs serves the ECS related functions
type AWSEcs struct {
	sess *session.Session
}

// NewAWSEcs creates a new AWSEcs instance
func NewAWSEcs(sess *session.Session) *AWSEcs {
	s := new(AWSEcs)
	s.sess = sess
	return s
}

// ListActiveServiceTasks lists all running and pending tasks of the service
func (s *AWSEcs) ListActiveServiceTasks(ctx context.Context, cluster string, service string) (taskIDs map[string]bool, err error) {
	return s.ListActiveServiceTasksWithLimit(ctx, cluster, service, 0)
}

// ListActiveServiceTasksWithLimit lists all tasks of the service by pagination, called by unit test only.
func (s *AWSEcs) ListActiveServiceTasksWithLimit(ctx context.Context, cluster string, service string, limit int64) (taskIDs map[string]bool, err error) {
	// ECS documents:
	// "Recently-stopped tasks might appear in the returned results. Currently,
	//  stopped tasks appear in the returned results for at least one hour."
	// "ECS never sets the desired status of a task to that value (only a task's
	//  lastStatus may have a value of PENDING )."
	params := &ecs.ListTasksInput{
		Cluster:       aws.String(cluster),
		ServiceName:   aws.String(service),
		DesiredStatus: aws.String(taskStatusRunning),
	}

	var nextToken *string
	taskIDs = make(map[string]bool)
	svc := ecs.New(s.sess)
	for true {
		params.NextToken = nextToken
		if limit > 0 {
			params.MaxResults = aws.Int64(limit)
		}

		resp, err := svc.ListTasks(params)

		if err != nil {
			glog.Errorln("failed to list task, cluster", cluster, "service", service, "error", err)
			return nil, err
		}

		glog.Infoln("list service", service, "cluster", cluster, "resp", resp)

		nextToken = resp.NextToken

		// check if there is any TaskIDs in resp
		if len(resp.TaskArns) == 0 {
			if resp.NextToken != nil {
				glog.Errorln("no TaskArns in resp but NextToken is not nil, resp", resp)
				continue
			}
			glog.Infoln("no more tasks for service", service,
				"cluster", cluster, "task count", len(taskIDs))
			return taskIDs, nil
		}

		// add the TaskIDs in resp
		for _, task := range resp.TaskArns {
			taskIDs[*task] = true
			glog.Infoln("list task", *task)
		}

		glog.Infoln("list", len(taskIDs), "tasks, service", service, "cluster", cluster)

		if resp.NextToken == nil {
			// no more tasks to list
			return taskIDs, nil
		}
	}

	return nil, common.ErrInternal
}

// GetServiceTask gets the task running on the containerInstanceID
func (s *AWSEcs) GetServiceTask(ctx context.Context, cluster string, service string, containerInstanceID string) (taskID string, err error) {
	// The ecs ListTasks cannot specify serviceName with other arguments.
	// Also ecs DescribeTasks does not return the ServiceName.
	// So list tasks separately by ServiceName and ContainerInstance.

	svc := ecs.New(s.sess)

	// list tasks of the service
	params := &ecs.ListTasksInput{
		Cluster:     aws.String(cluster),
		ServiceName: aws.String(service),
	}
	resp, err := svc.ListTasks(params)
	if err != nil {
		glog.Errorln("ListTasks by service", service, "error", err,
			"ContainerInstance", containerInstanceID, "cluster", cluster)
		return "", err
	}
	if len(resp.TaskArns) == 0 {
		glog.Errorln("service", service, "has no task, cluster", cluster, "resp", resp)
		return "", containersvc.ErrContainerSvcNoTask
	}

	glog.V(0).Infoln("ListTasks by service", service, "resp", resp)

	serviceTasks := make(map[string]bool)
	for _, taskArn := range resp.TaskArns {
		serviceTasks[*taskArn] = true
	}

	// list tasks on the ContainerInstance
	params = &ecs.ListTasksInput{
		Cluster:           aws.String(cluster),
		ContainerInstance: aws.String(containerInstanceID),
	}
	resp, err = svc.ListTasks(params)
	if err != nil {
		glog.Errorln("ListTasks by ContainerInstanc", containerInstanceID, "error", err,
			"service", service, "cluster", cluster)
		return "", err
	}
	if len(resp.TaskArns) == 0 {
		glog.Errorln("ContainerInstance", containerInstanceID, "has no task, cluster", cluster, "resp", resp)
		return "", containersvc.ErrContainerSvcNoTask
	}

	glog.V(0).Infoln("ListTasks by ContainerInstance", containerInstanceID, "resp", resp)

	for _, taskArn := range resp.TaskArns {
		_, ok := serviceTasks[*taskArn]
		if ok {
			// task belongs to service
			glog.Infoln("find task", *taskArn, "on ContainerInstance", containerInstanceID,
				"service", service, "cluster", cluster)

			if len(taskID) != 0 {
				glog.Infoln("more than 1 task on ContainerInstance", containerInstanceID,
					"service", service, "cluster", cluster, "resp", resp)
				return "", containersvc.ErrContainerSvcTooManyTasks
			}

			taskID = *taskArn
		}
	}

	if len(taskID) == 0 {
		glog.Errorln("service", service, "has no task on ContainerInstance",
			containerInstanceID, "cluster", cluster)
		return "", containersvc.ErrContainerSvcNoTask
	}

	glog.Infoln("get task", taskID, "on ContainerInstance", containerInstanceID,
		"service", service, "cluster", cluster)
	return taskID, nil
}

// GetTaskContainerInstance returns the ContainerInstanceArn the task runs on
func (s *AWSEcs) GetTaskContainerInstance(ctx context.Context, cluster string, taskArn string) (containerInstanceArn string, err error) {
	// describe task to get the ContainerInstance
	params := &ecs.DescribeTasksInput{
		Tasks: []*string{
			aws.String(taskArn),
		},
		Cluster: aws.String(cluster),
	}
	svc := ecs.New(s.sess)
	resp, err := svc.DescribeTasks(params)
	if err != nil {
		glog.Errorln("failed to describe task", taskArn, "cluster", cluster, "error", err)
		return "", err
	}
	if len(resp.Tasks) != 1 {
		glog.Errorln("describe taskArn", taskArn,
			"cluster", cluster, "get more than 1 tasks, resp", resp)
		return "", common.ErrInternal
	}

	containerInstanceArn = *(resp.Tasks[0].ContainerInstanceArn)

	glog.Infoln("get ContainerInstanceArn", containerInstanceArn,
		"for task", taskArn, "cluster", cluster)

	return containerInstanceArn, nil
}

// The functions for automating the ECS task definition and service creation

// IsClusterExist checks whether the cluster exists.
func (s *AWSEcs) IsClusterExist(ctx context.Context, cluster string) (bool, error) {
	params := &ecs.DescribeClustersInput{
		Clusters: []*string{
			aws.String(cluster),
		},
	}

	svc := ecs.New(s.sess)
	resp, err := svc.DescribeClusters(params)
	if err != nil {
		glog.Errorln("describe cluster error", err, cluster)
		return false, err
	}
	if len(resp.Clusters) == 0 {
		glog.Infoln("cluster not exist", cluster, resp)
		return false, nil
	}
	if len(resp.Clusters) == 1 && *(resp.Clusters[0].ClusterName) == cluster {
		glog.Infoln("cluster exist", cluster, resp)
		return true, nil
	}

	glog.Errorln("internal error, cluster", cluster, "resp", resp)
	return false, common.ErrInternal
}

// IsServiceExist checks whether the service exists.
func (s *AWSEcs) IsServiceExist(ctx context.Context, cluster string, service string) (bool, error) {
	params := &ecs.DescribeServicesInput{
		Services: []*string{
			aws.String(service),
		},
		Cluster: aws.String(cluster),
	}

	svc := ecs.New(s.sess)
	resp, err := svc.DescribeServices(params)
	if err != nil {
		glog.Errorln("describe service error", err, service, "cluster", cluster)
		return false, err
	}
	if len(resp.Services) == 0 {
		glog.Infoln("service not exists", service, "cluster", cluster, resp)
		return false, nil
	}
	if len(resp.Services) == 1 && *(resp.Services[0].ServiceName) == service {
		status := *(resp.Services[0].Status)
		if status == serviceStatusActive || status == serviceStatusDraining {
			glog.Infoln("service exists", service, "cluster", cluster, "resp", resp)
			return true, nil
		}
		if status == serviceStatusInactive {
			glog.Infoln("service is inactive", service, "cluster", cluster, "resp", resp)
			return false, nil
		}
	}

	glog.Errorln("internal error, service", service, "cluster", cluster, "resp", resp)
	return false, common.ErrInternal
}

func (s *AWSEcs) isTaskDefFamilyExist(ctx context.Context, taskDefFamily string) (bool, error) {
	// TODO could we use describe-task-definition? looks not? If a taskDefFamily does not
	// have the active task definition, ECS returns ClientException. Not be able to know
	// whether it is a real exception or not.
	families, err := s.listTaskDefFamilies(ctx, taskDefFamily)
	if err != nil {
		glog.Errorln("listTaskDefFamilies error", err, taskDefFamily)
		return false, err
	}

	for _, fam := range families {
		if fam == taskDefFamily {
			glog.Infoln("taskDefFamily exists", taskDefFamily)
			return true, nil
		}
	}

	glog.Infoln("taskDefFamily not exists", taskDefFamily)
	return false, nil
}

func (s *AWSEcs) listTaskDefFamilies(ctx context.Context, prefix string) (families []string, err error) {
	params := &ecs.ListTaskDefinitionFamiliesInput{
		FamilyPrefix: aws.String(prefix),
		Status:       aws.String(taskDefStatusActive),
	}

	svc := ecs.New(s.sess)
	var nextToken *string
	for true {
		params.NextToken = nextToken

		resp, err := svc.ListTaskDefinitionFamilies(params)
		if err != nil {
			glog.Errorln("ListTaskDefinitionFamilies error", err, "prefix", prefix, "token", nextToken)
			return families, err
		}

		glog.Infoln("ListTaskDefinitionFamilies prefix", prefix, "token", nextToken, "resp", resp)

		for _, fam := range resp.Families {
			families = append(families, *fam)
		}

		if resp.NextToken == nil {
			break
		}

		nextToken = resp.NextToken
	}

	glog.Infoln("list", len(families), "task definition families for prefix", prefix)
	return families, nil
}

func (s *AWSEcs) getLatestTaskDefinition(ctx context.Context, taskDefFamily string) (string, error) {
	params := &ecs.ListTaskDefinitionsInput{
		FamilyPrefix: aws.String(taskDefFamily),
		Status:       aws.String(taskDefStatusActive),
	}

	var token *string
	taskDef := ""
	svc := ecs.New(s.sess)
	for true {
		params.NextToken = token

		resp, err := svc.ListTaskDefinitions(params)
		if err != nil {
			glog.Errorln("ListTaskDefinitions error", err, "family", taskDefFamily)
			return "", err
		}

		taskDefCount := len(resp.TaskDefinitionArns)
		if taskDefCount != 0 {
			lastTaskDef := resp.TaskDefinitionArns[taskDefCount-1]
			revision, err := s.getTaskDefRevision(ctx, *lastTaskDef)
			if err != nil {
				glog.Errorln("getTaskDefRevision error", err, resp)
				return "", err
			}
			taskDef = taskDefFamily + taskFamilyRevisionSep + revision
		}

		if resp.NextToken == nil {
			break
		}

		token = params.NextToken
		glog.V(5).Infoln("pagination list next batch task definitions, taskDefFamily",
			taskDefFamily, "token", token, "taskDef", taskDef)
	}

	if taskDef == "" {
		glog.Errorln("not found the task definition for", taskDefFamily)
		return "", common.ErrNotFound
	}

	glog.Infoln("find the latest task definition", taskDef, "for", taskDefFamily)
	return taskDef, nil
}

func (s *AWSEcs) getTaskDefRevision(ctx context.Context, taskDef string) (string, error) {
	idx := strings.LastIndex(taskDef, taskFamilyRevisionSep)
	if idx == -1 {
		glog.Errorln("taskDef doesn't have revision", taskDef)
		return "", common.ErrInvalidArgs
	}

	return taskDef[idx+1 : len(taskDef)], nil
}

// CreateCluster creates an ECS cluster
func (s *AWSEcs) CreateCluster(ctx context.Context, cluster string) error {
	params := &ecs.CreateClusterInput{
		ClusterName: aws.String(cluster),
	}

	svc := ecs.New(s.sess)
	resp, err := svc.CreateCluster(params)
	if err != nil {
		glog.Errorln("CreateCluster error", err, cluster)
		return err
	}

	glog.Infoln("create cluster done", cluster, "resp", resp)
	return nil
}

// CreateService creates a ECS service
func (s *AWSEcs) CreateService(ctx context.Context, opts *containersvc.CreateServiceOptions) error {
	// check and create the ECS task definition for the service
	taskDefFamily := s.genTaskDefFamilyForService(opts.Common.Cluster, opts.Common.ServiceName)

	taskDef, err := s.createEcsTaskDefinitionForService(ctx, taskDefFamily, opts)
	if err != nil {
		glog.Errorln("createEcsTaskDefinitionForService error", err, "taskDefFamily",
			taskDefFamily, "options", opts.Common, opts.Replicas)
		return err
	}

	glog.Infoln("created ECS task definition", taskDef, "options", opts.Common, opts.Replicas)

	params := &ecs.CreateServiceInput{
		TaskDefinition: aws.String(taskDef),
		Cluster:        aws.String(opts.Common.Cluster),
		ServiceName:    aws.String(opts.Common.ServiceName),
		DesiredCount:   aws.Int64(int64(opts.Replicas)),
		DeploymentConfiguration: &ecs.DeploymentConfiguration{
			MaximumPercent:        aws.Int64(maxHealthyPercent),
			MinimumHealthyPercent: aws.Int64(minHealthyPercent),
		},
		// http://docs.aws.amazon.com/AmazonECS/latest/developerguide/task-placement-strategies.html
		PlacementStrategy: []*ecs.PlacementStrategy{
			{
				Field: aws.String("attribute:ecs.availability-zone"),
				Type:  aws.String("spread"),
			},
		},
	}

	svc := ecs.New(s.sess)
	resp, err := svc.CreateService(params)
	if err != nil {
		glog.Errorln("CreateService error", err, "taskDef", taskDef, "options", opts.Common, opts.Replicas)
		return err
	}
	if resp == nil || resp.Service == nil || resp.Service.ServiceArn == nil {
		glog.Errorln("CreateService internal error - bad resp", resp, "taskDef", taskDef, "options", opts.Common, opts.Replicas)
		return common.ErrInternal
	}

	glog.Infoln("create service done, taskDef", taskDef, "resp", resp, "options", opts.Common, opts.Replicas)
	return nil
}

func (s *AWSEcs) checkTaskDefExist(ctx context.Context, taskDefFamily string) (taskDef string, exist bool, err error) {
	exist, err = s.isTaskDefFamilyExist(ctx, taskDefFamily)
	if err != nil {
		glog.Errorln("check taskDefFamily exist error", err, "taskDefFamily", taskDefFamily)
		return "", exist, err
	}

	// if taskDefFamily exists, return the latest TaskDefinition
	if exist {
		taskDef, err = s.getLatestTaskDefinition(ctx, taskDefFamily)
		if err != nil {
			glog.Errorln("get the latest task definition error", err, "taskDefFamily", taskDefFamily)
		}
		return taskDef, exist, err
	}

	glog.Infoln("taskDefFamily not exist", taskDefFamily)
	return "", false, nil
}

func (s *AWSEcs) createRegisterTaskDefinitionInput(taskDefFamily string,
	commonOpts *containersvc.CommonOptions) *ecs.RegisterTaskDefinitionInput {

	containerName := taskDefFamily + common.NameSeparator + common.ContainerNameSuffix

	containerDef := &ecs.ContainerDefinition{
		Name:       aws.String(containerName),
		Image:      aws.String(commonOpts.ContainerImage),
		Essential:  aws.Bool(true),
		Privileged: aws.Bool(false),
		//Environment: []*ecs.KeyValuePair{
		//	{ // Required
		//		Name:  aws.String("String"),
		//		Value: aws.String("String"),
		//	},
		// More values...
		//},
		// LogConfiguration: &ecs.LogConfiguration{
		//  LogDriver: aws.String("LogDriver"),
		//  Options: map[string]*string{
		//    "Key": aws.String("String"),
		//  },
		//},
	}
	if commonOpts.Resource != nil {
		if commonOpts.Resource.ReserveCPUUnits != -1 {
			containerDef.Cpu = aws.Int64(commonOpts.Resource.ReserveCPUUnits)
		}
		if commonOpts.Resource.ReserveMemMB != -1 {
			// soft limit
			containerDef.MemoryReservation = aws.Int64(commonOpts.Resource.ReserveMemMB)
		}
		if commonOpts.Resource.MaxMemMB != -1 {
			// hard limit
			containerDef.Memory = aws.Int64(commonOpts.Resource.MaxMemMB)
		}
	}

	params := &ecs.RegisterTaskDefinitionInput{
		Family: aws.String(taskDefFamily),
		//TaskRoleArn: aws.String("String"),

		ContainerDefinitions: []*ecs.ContainerDefinition{
			containerDef,
		},
	}
	return params
}

// createEcsTaskDefinitionForService registers the ECS task definition for the service.
// set "port" to -1, if no need to expose container port to host port.
// set "containerPath" to "", if no need to mount volume.
func (s *AWSEcs) createEcsTaskDefinitionForService(ctx context.Context, taskDefFamily string,
	opts *containersvc.CreateServiceOptions) (taskDef string, err error) {

	taskDef, exist, err := s.checkTaskDefExist(ctx, taskDefFamily)
	if err != nil {
		glog.Errorln("checkTaskDefExist error", err, taskDefFamily)
		return taskDef, err
	}
	if exist {
		glog.Infoln("taskDefFamily exist", taskDefFamily)
		return taskDef, nil
	}

	params := s.createRegisterTaskDefinitionInput(taskDefFamily, opts.Common)

	if len(opts.ContainerPath) != 0 {
		params.Volumes = []*ecs.Volume{
			{
				Host: &ecs.HostVolumeProperties{
					SourcePath: aws.String(opts.Common.ServiceUUID),
				},
				Name: aws.String(opts.Common.ServiceUUID),
			},
		}

		params.ContainerDefinitions[0].MountPoints = []*ecs.MountPoint{
			{
				ContainerPath: aws.String(opts.ContainerPath),
				SourceVolume:  aws.String(opts.Common.ServiceUUID),
				//ReadOnly:      aws.Bool(true),
			},
		}
	}

	if opts.Port != -1 {
		portMappings := []*ecs.PortMapping{
			{
				ContainerPort: aws.Int64(opts.Port),
				HostPort:      aws.Int64(opts.Port),
				Protocol:      aws.String(tcpProtocol),
			},
		}
		params.ContainerDefinitions[0].PortMappings = portMappings
	}

	if len(opts.Envkvs) != 0 {
		// pass the target environment keyvalue pairs to task
		envkvpairs := make([]*ecs.KeyValuePair, len(opts.Envkvs))
		for i, kv := range opts.Envkvs {
			kvpair := &ecs.KeyValuePair{
				Name:  aws.String(kv.Name),
				Value: aws.String(kv.Value),
			}
			envkvpairs[i] = kvpair
		}
		params.ContainerDefinitions[0].Environment = envkvpairs
	}

	svc := ecs.New(s.sess)
	resp, err := svc.RegisterTaskDefinition(params)
	if err != nil {
		glog.Errorln("registerTaskDefinition error", err, params)
		return "", err
	}

	revision := *(resp.TaskDefinition.Revision)
	taskDef = taskDefFamily + taskFamilyRevisionSep + strconv.FormatInt(revision, 10)
	glog.Infoln("create TaskDefinition done", taskDef, "resp", resp)
	return taskDef, nil
}

// createEcsTaskDefinitionForTask registers the ECS task definition for the task.
func (s *AWSEcs) createEcsTaskDefinitionForTask(ctx context.Context, taskDefFamily string,
	opts *containersvc.RunTaskOptions) (taskDef string, exist bool, err error) {

	taskDef, exist, err = s.checkTaskDefExist(ctx, taskDefFamily)
	if err != nil {
		glog.Errorln("checkTaskDefExist error", err, taskDefFamily)
		return taskDef, exist, err
	}
	if exist {
		glog.Infoln("taskDefFamily exist", taskDefFamily)
		return taskDef, exist, nil
	}

	params := s.createRegisterTaskDefinitionInput(taskDefFamily, opts.Common)

	if len(opts.Envkvs) != 0 {
		// pass the target environment keyvalue pairs to task
		envkvpairs := make([]*ecs.KeyValuePair, len(opts.Envkvs))
		for i, kv := range opts.Envkvs {
			kvpair := &ecs.KeyValuePair{
				Name:  aws.String(kv.Name),
				Value: aws.String(kv.Value),
			}
			envkvpairs[i] = kvpair
		}
		params.ContainerDefinitions[0].Environment = envkvpairs
	}

	svc := ecs.New(s.sess)
	resp, err := svc.RegisterTaskDefinition(params)
	if err != nil {
		glog.Errorln("registerTaskDefinition error", err, params)
		return "", false, err
	}

	revision := *(resp.TaskDefinition.Revision)
	taskDef = taskDefFamily + taskFamilyRevisionSep + strconv.FormatInt(revision, 10)
	glog.Infoln("create TaskDefinition done", taskDef, "resp", resp)
	return taskDef, false, nil
}

// WaitServiceRunning waits till all service containers are running or maxWaitSeconds reaches.
func (s *AWSEcs) WaitServiceRunning(ctx context.Context, cluster string, service string, replicas int64, maxWaitSeconds int64) error {
	ecscli := ecs.New(s.sess)
	var pending int64
	for sec := int64(0); sec < maxWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		svc, err := s.describeService(ctx, ecscli, cluster, service)
		if err == nil {
			if *(svc.DesiredCount) != replicas {
				glog.Errorln("DesiredCount != replicas", replicas, svc)
				return common.ErrInvalidArgs
			}
			pending = *(svc.PendingCount)
			if pending == 0 && *(svc.RunningCount) == *(svc.DesiredCount) {
				glog.Infoln("all tasks are running", svc)
				return nil
			}
			glog.Infoln("not all tasks are running yet", svc)
		} else {
			glog.Errorln("describeService error", err, "service", service, "cluster", cluster)
		}

		time.Sleep(time.Duration(common.DefaultRetryWaitSeconds) * time.Second)
	}

	glog.Errorln("service", service, "still has", pending, "pending tasks after", maxWaitSeconds)
	return common.ErrTimeout
}

// GetServiceStatus returns the ServiceStatus.
func (s *AWSEcs) GetServiceStatus(ctx context.Context, cluster string, service string) (*common.ServiceStatus, error) {
	ecscli := ecs.New(s.sess)
	svc, err := s.describeService(ctx, ecscli, cluster, service)
	if err != nil {
		glog.Errorln("describeService error", err, service, "cluster", cluster)
		return nil, err
	}

	status := &common.ServiceStatus{
		RunningCount: *(svc.RunningCount),
		DesiredCount: *(svc.DesiredCount),
	}
	glog.Infoln("service", service, "has", status.RunningCount,
		"running containers, desired", status.DesiredCount)
	return status, nil
}

// waitServiceStop waits till all service containers are stopped or maxWaitSeconds reaches.
func (s *AWSEcs) waitServiceStop(ctx context.Context, ecscli *ecs.ECS, cluster string, service string, maxWaitSeconds int64) error {
	var running int64
	for sec := int64(0); sec < maxWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		svc, err := s.describeService(ctx, ecscli, cluster, service)
		if err == nil {
			running = *(svc.RunningCount)
			if running == 0 {
				glog.Infoln("service", service, "has no running task")
				return nil
			}
			glog.Infoln("service", service, "still has", running, "running tasks")
		} else {
			glog.Errorln("describeService error", err, "service", service, "cluster", cluster)
		}

		time.Sleep(time.Duration(common.DefaultRetryWaitSeconds) * time.Second)
	}

	glog.Errorln("service", service, "still has", running, "running tasks after", maxWaitSeconds)
	return common.ErrTimeout
}

func (s *AWSEcs) describeService(ctx context.Context, ecscli *ecs.ECS, cluster string, service string) (*ecs.Service, error) {
	params := &ecs.DescribeServicesInput{
		Services: []*string{
			aws.String(service),
		},
		Cluster: aws.String(cluster),
	}

	resp, err := ecscli.DescribeServices(params)
	if err != nil {
		glog.Errorln("DescribeServices error", err, "service", service, "cluster", cluster)
		return nil, err
	}
	if len(resp.Services) != 1 {
		glog.Errorln("expect 1 service", resp)
		return nil, common.ErrInternal
	}

	return resp.Services[0], nil
}

func (s *AWSEcs) updateService(ctx context.Context, ecscli *ecs.ECS, cluster string, service string, desiredCount int64) error {
	params := &ecs.UpdateServiceInput{
		Cluster:      aws.String(cluster),
		Service:      aws.String(service),
		DesiredCount: aws.Int64(desiredCount),
	}

	resp, err := ecscli.UpdateService(params)
	if err != nil {
		glog.Errorln("UpdateService error", err, "service", service, "cluster", cluster)
		return err
	}

	glog.Infoln("updated service desiredCount", service, desiredCount, "resp", resp)
	return nil
}

// StopService stops all service containers
func (s *AWSEcs) StopService(ctx context.Context, cluster string, service string) error {
	ecscli := ecs.New(s.sess)
	err := s.updateService(ctx, ecscli, cluster, service, 0)
	if err != nil {
		glog.Errorln("updateService error", err, "service", service, "cluster", cluster)
		if s.isServiceNotFoundError(err) || err.(awserr.Error).Code() == serviceNotActiveException {
			return nil
		}
		return err
	}

	glog.Infoln("stop service complete", service, "cluster", cluster)
	return nil
}

// RestartService waits all service containers stopped and then starts the containers again.
func (s *AWSEcs) RestartService(ctx context.Context, cluster string, service string, desiredCount int64) error {
	ecscli := ecs.New(s.sess)

	// stop service
	err := s.updateService(ctx, ecscli, cluster, service, 0)
	if err != nil {
		glog.Errorln("stopService error", err, "service", service)
		return err
	}

	err = s.waitServiceStop(ctx, ecscli, cluster, service, common.DefaultServiceWaitSeconds)
	if err != nil {
		glog.Errorln("WaitServiceStop error", err, "service", service)
		return err
	}

	// start service again
	err = s.updateService(ctx, ecscli, cluster, service, desiredCount)
	if err != nil {
		glog.Errorln("start service error", err, "service", service, "desiredCount", desiredCount)
		return err
	}

	glog.Infoln("RestartService complete", service, "desiredCount", desiredCount)
	return nil
}

// DeleteCluster deletes the ECS cluster
func (s *AWSEcs) DeleteCluster(ctx context.Context, cluster string) error {
	params := &ecs.DeleteClusterInput{
		Cluster: aws.String(cluster),
	}

	svc := ecs.New(s.sess)
	resp, err := svc.DeleteCluster(params)
	if err != nil {
		glog.Errorln("DeleteCluster error", err, cluster)
		return err
	}

	glog.Infoln("delete cluster done", cluster, "resp", resp)
	return nil
}

// DeleteService deletes the ECS service and TaskDefinition
func (s *AWSEcs) DeleteService(ctx context.Context, cluster string, service string) error {
	ecscli := ecs.New(s.sess)
	taskDef := ""
	svc, err := s.describeService(ctx, ecscli, cluster, service)
	if err == nil {
		params := &ecs.DeleteServiceInput{
			Service: aws.String(service),
			Cluster: aws.String(cluster),
		}

		resp, err := ecscli.DeleteService(params)
		if err != nil {
			glog.Errorln("DeleteService error", err, "service", service, "cluster", cluster)
			return err
		}

		glog.Infoln("deleted service", service, "cluster", cluster, "resp", resp)

		taskDef = *svc.TaskDefinition

	} else {
		glog.Errorln("describeService error", err, "cluster", cluster, "service", service)

		if !s.isServiceNotFoundError(err) {
			return err
		}

		// service not found, maybe a retry request, continue to delete TaskDefinition
		taskDefFamily := s.genTaskDefFamilyForService(cluster, service)
		taskDef, err = s.getLatestTaskDefinition(ctx, taskDefFamily)
		if err != nil {
			glog.Errorln("getLatestTaskDefinition error", err, taskDefFamily)
			if err == common.ErrNotFound {
				return nil
			}
			return err
		}
	}

	err = s.deregisterTaskDefinition(ctx, taskDef)
	if err != nil {
		glog.Errorln("deregisterTaskDefinition error", err, *svc.TaskDefinition)
		return err
	}

	glog.Infoln("delete service done", service, "cluster", cluster)
	return nil
}

func (s *AWSEcs) deregisterTaskDefinition(ctx context.Context, taskDef string) error {
	// taskDef is family:revision
	params := &ecs.DeregisterTaskDefinitionInput{
		TaskDefinition: aws.String(taskDef),
	}

	svc := ecs.New(s.sess)
	resp, err := svc.DeregisterTaskDefinition(params)
	if err != nil {
		glog.Errorln("DeregisterTaskDefinition error", err, taskDef)
		return err
	}

	glog.Infoln("deregister task definition done", taskDef, "resp", resp)
	return nil
}

// DeregisterContainerInstance deregisters the container instance from ECS cluster
func (s *AWSEcs) DeregisterContainerInstance(ctx context.Context, cluster string, instance string) error {
	params := &ecs.DeregisterContainerInstanceInput{
		ContainerInstance: aws.String(instance),
		Cluster:           aws.String(cluster),
	}

	svc := ecs.New(s.sess)
	resp, err := svc.DeregisterContainerInstance(params)
	if err != nil {
		glog.Errorln("DeregisterContainerInstance error", err, instance, "cluster", cluster)
		return err
	}

	glog.Infoln("deregister container instance done", instance, "cluster", cluster, "resp", resp)
	return nil
}

// listRunningTasks lists the tasks with DesiredStatus == RUNNING.
func (s *AWSEcs) listRunningTasks(ctx context.Context, opts *containersvc.RunTaskOptions) (tasks []string, err error) {
	taskDefFamily := s.genTaskDefFamilyForTask(opts.Common.Cluster, opts.Common.ServiceName, opts.TaskType)

	params := &ecs.ListTasksInput{
		Cluster:       aws.String(opts.Common.Cluster),
		DesiredStatus: aws.String(taskStatusRunning),
		Family:        aws.String(taskDefFamily),
	}

	var nextToken *string
	svc := ecs.New(s.sess)
	for true {
		params.NextToken = nextToken

		resp, err := svc.ListTasks(params)
		if err != nil {
			glog.Errorln("failed to list task", err, opts.Common, opts.TaskType)
			return nil, err
		}

		glog.Infoln("response", len(resp.TaskArns), "tasks", opts.Common, opts.TaskType)

		nextToken = resp.NextToken

		// check if there is any task in resp
		if len(resp.TaskArns) == 0 {
			if nextToken != nil {
				glog.Errorln("no TaskArns in resp but NextToken is not nil, params", params, "resp", resp)
				continue
			}
			glog.Infoln("list", len(tasks), "tasks", opts.Common, opts.TaskType)
			return tasks, nil
		}

		// add the tasks in resp
		for _, task := range resp.TaskArns {
			glog.V(1).Infoln("list task", *task)
			if tasks == nil {
				tasks = []string{*task}
			} else {
				tasks = append(tasks, *task)
			}
		}

		glog.Infoln("list", len(tasks), "tasks", opts.Common, opts.TaskType)

		if nextToken == nil {
			// no more tasks to list
			return tasks, nil
		}
	}

	glog.Errorln("listRunningTasks - internal error", opts.Common, opts.TaskType)
	return nil, common.ErrInternal
}

// RunTask starts a new task. ECS limits "startedBy" up to 36 letters.
func (s *AWSEcs) RunTask(ctx context.Context, opts *containersvc.RunTaskOptions) (taskID string, err error) {
	// create task definition
	taskDefFamily := s.genTaskDefFamilyForTask(opts.Common.Cluster, opts.Common.ServiceName, opts.TaskType)

	taskDef, taskDefExist, err := s.createEcsTaskDefinitionForTask(ctx, taskDefFamily, opts)
	if taskDefExist {
		// task definition exists, check if there is task running
		tasks, err := s.listRunningTasks(ctx, opts)
		if err != nil {
			glog.Errorln("listRunningTasks error", err, opts.Common, opts.TaskType)
			return "", err
		}

		if len(tasks) == 1 {
			glog.Infoln("task is already running", tasks[0], opts.Common, opts.TaskType)
			return tasks[0], nil
		}

		if len(tasks) != 0 {
			glog.Errorln("internal error - multiple running tasks", len(tasks), opts.Common, opts.TaskType)
			return "", common.ErrInternal
		}

		// no task running, continue to run it
	}

	// run the task
	params := &ecs.RunTaskInput{
		TaskDefinition: aws.String(taskDef),
		Cluster:        aws.String(opts.Common.Cluster),
		Count:          aws.Int64(1),
	}
	if len(opts.Envkvs) != 0 {
		// pass the target environment keyvalue pairs to task
		envkvpairs := make([]*ecs.KeyValuePair, len(opts.Envkvs))
		for i, kv := range opts.Envkvs {
			kvpair := &ecs.KeyValuePair{
				Name:  aws.String(kv.Name),
				Value: aws.String(kv.Value),
			}
			envkvpairs[i] = kvpair
		}

		containerName := taskDefFamily + common.NameSeparator + common.ContainerNameSuffix

		overrides := &ecs.TaskOverride{
			ContainerOverrides: []*ecs.ContainerOverride{
				{
					Name:        aws.String(containerName),
					Environment: envkvpairs,
				},
			},
		}
		params.Overrides = overrides
	}

	svc := ecs.New(s.sess)
	resp, err := svc.RunTask(params)

	if err != nil {
		glog.Errorln("RunTask error", err, "taskDef", taskDef, opts.Common, opts.TaskType)
		return "", err
	}
	if len(resp.Tasks) != 1 {
		glog.Errorln("expect 1 task", resp)
		return "", common.ErrInternal
	}

	taskID = *(resp.Tasks[0].TaskArn)
	glog.Infoln("RunTask succeeds, taskID", taskID, opts.Common, opts.TaskType)
	return taskID, nil
}

// GetTaskStatus returns the task's status.
func (s *AWSEcs) GetTaskStatus(ctx context.Context, cluster string, taskArn string) (*common.TaskStatus, error) {
	task, err := s.describeTasks(ctx, cluster, taskArn)
	if err != nil {
		glog.Errorln("describeTasks error", err, "taskID", taskArn, "cluster", cluster)
		return nil, err
	}

	taskStatus := &common.TaskStatus{
		Status: *task.LastStatus,
	}
	if task.StoppedReason != nil {
		taskStatus.StoppedReason = *task.StoppedReason
	}
	if task.StartedAt != nil {
		taskStatus.StartedAt = s.formatTimeString(task.StartedAt)
	}
	if task.StoppedAt != nil {
		taskStatus.FinishedAt = s.formatTimeString(task.StoppedAt)
	}
	return taskStatus, nil
}

// format time to 2017-05-18T14:16:24.343219648Z
func (s *AWSEcs) formatTimeString(t *time.Time) string {
	return fmt.Sprintf("%d-%02d-%02dT%02d:%02d:%02d.%9dZ\n",
		t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second(), t.Nanosecond())
}

// WaitTaskComplete waits till the task successfully completes.
func (s *AWSEcs) WaitTaskComplete(ctx context.Context, cluster string, taskArn string, maxWaitSeconds int64) error {
	for sec := int64(0); sec < maxWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		// wait some time for the task to run
		time.Sleep(time.Duration(common.DefaultRetryWaitSeconds) * time.Second)

		task, err := s.describeTasks(ctx, cluster, taskArn)
		if err == nil {
			// http://docs.aws.amazon.com/AmazonECS/latest/developerguide/task_life_cycle.html
			// When both LastStatus and DesiredStatus are "stopped", there are 3 possiblility:
			// 1) task succeeds, 2) stop task API is called, 3) container instance terminated or stopped.
			// Q: how about container instance restart? Restart == stop + start, so expect #3 will happen.
			// There is no way to exactl know whether #2 or #3 happens.
			// So as long as both are "stopped", return success. The caller should still check whether
			// the task indeed succeeds or not.
			if *(task.LastStatus) == taskStatusStopped && *(task.DesiredStatus) == taskStatusStopped {
				glog.Infoln("task completes", task)
				return nil
			}

			glog.Infoln("task not done yet", task)
		} else {
			glog.Errorln("describeTasks error", err, "cluster", cluster, "taskArn", taskArn)
		}
	}

	glog.Errorln("cluster", cluster, "task", taskArn, "didn't complete after", maxWaitSeconds)
	return common.ErrTimeout
}

// DeleteTask deletes the task definition
func (s *AWSEcs) DeleteTask(ctx context.Context, cluster string, service string, taskType string) error {
	taskDefFamily := s.genTaskDefFamilyForTask(cluster, service, taskType)

	taskDef, err := s.getLatestTaskDefinition(ctx, taskDefFamily)
	if err != nil {
		glog.Errorln("getLatestTaskDefinition error", err, "taskDefFamily", taskDefFamily)
		return err
	}

	glog.Infoln("deleted task definition", taskDef, taskDefFamily)

	return s.deregisterTaskDefinition(ctx, taskDef)
}

func (s *AWSEcs) describeTasks(ctx context.Context, cluster string, taskArn string) (task *ecs.Task, err error) {
	params := &ecs.DescribeTasksInput{
		Tasks: []*string{
			aws.String(taskArn),
		},
		Cluster: aws.String(cluster),
	}

	svc := ecs.New(s.sess)
	resp, err := svc.DescribeTasks(params)
	if err != nil {
		glog.Errorln("DescribeTasks error", err, "cluster", cluster, "taskArn", taskArn)
		return nil, err
	}
	if len(resp.Failures) != 0 || len(resp.Tasks) != 1 {
		glog.Errorln("DescribeTasks failure or get more than 1 tasks, cluster", cluster,
			"taskArn", taskArn, "resp", resp)
		return nil, common.ErrInternal
	}

	task = resp.Tasks[0]
	glog.Infoln("DescribeTasks succeeds, cluster", cluster, "task", task)
	return task, nil
}

func (s *AWSEcs) genTaskDefFamilyForService(cluster string, service string) string {
	return cluster + common.NameSeparator + service
}

func (s *AWSEcs) genTaskDefFamilyForTask(cluster string, service string, taskType string) string {
	return cluster + common.NameSeparator + service + common.NameSeparator + taskType
}

func (s *AWSEcs) isServiceNotFoundError(err error) bool {
	return err.(awserr.Error).Code() == serviceNotFoundException
}
