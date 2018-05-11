package cloudlog

import (
	"golang.org/x/net/context"
)

// See [Catalog README](https://github.com/cloudstax/firecamp/pkg/tree/master/catalog/README.md) for the detail log group and stream formats.

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
	// CreateStreamLogConfig creates the LogConfig for the stateless service, such as the managemen service,
	// or the task of the stateful service, such as init task.
	CreateStreamLogConfig(ctx context.Context, cluster string, service string, serviceUUID string, stream string) *LogConfig
	// InitializeServiceLogConfig creates the log group on AWS CloudWatch.
	InitializeServiceLogConfig(ctx context.Context, cluster string, service string, serviceUUID string) error
	// DeleteServiceLogConfig deletes the log group from AWS CloudWatch.
	DeleteServiceLogConfig(ctx context.Context, cluster string, service string, serviceUUID string) error
}
