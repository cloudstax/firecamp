package cloudlog

import (
	"golang.org/x/net/context"
)

// On AWS, every service will have its own log group: firecamp-clustername-servicename-serviceUUID.
// Every service member will get the stream name: membername/hostname/containerID.
// AWS ECS and Docker Swarm will have the same log group and stream.
// K8s will be slightly different, as K8s has namespace and init container concepts.
// K8s log group: firecamp-clustername-namespace-servicename-serviceUUID.
// K8s log stream: membername/containername/hostname/containerID. FireCamp will set the init/stop
// container name with the init and stop prefix in the pod.

// LogConfig contains the configs for the log driver.
type LogConfig struct {
	// the log driver name, such as awslogs.
	Name string
	// the log driver options, such as awslogs-region, awslogs-group, etc.
	Options map[string]string
}

// CloudLog defines the common log interface.
type CloudLog interface {
	// CreateServiceLogConfig creates the LogConfig for the service to send logs to AWS CloudWatch.
	CreateServiceLogConfig(ctx context.Context, cluster string, service string, serviceUUID string) *LogConfig
	// CreateTaskLogConfig creates the LogConfig for the task to send logs to AWS CloudWatch.
	CreateTaskLogConfig(ctx context.Context, cluster string, service string, serviceUUID string, taskType string) *LogConfig
	// InitializeServiceLogConfig creates the log group on AWS CloudWatch.
	InitializeServiceLogConfig(ctx context.Context, cluster string, service string, serviceUUID string) error
	// DeleteServiceLogConfig deletes the log group from AWS CloudWatch.
	DeleteServiceLogConfig(ctx context.Context, cluster string, service string, serviceUUID string) error
}
