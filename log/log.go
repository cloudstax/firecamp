package cloudlog

import (
	"golang.org/x/net/context"
)

// LogConfig contains the configs for the log driver
type LogConfig struct {
	// the log driver name, such as awslogs
	Name string
	// the log driver options, such as awslogs-region, awslogs-group, etc.
	Options map[string]string
}

// CloudLog defines the common log interface.
type CloudLog interface {
	// CreateServiceLogConfig creates the LogConfig for the service to send logs to AWS CloudWatch.
	CreateServiceLogConfig(ctx context.Context, cluster string, service string, serviceUUID string) *LogConfig
	// CreateLogConfigForStream creates the LogConfig for the stream to send logs to AWS CloudWatch.
	CreateLogConfigForStream(ctx context.Context, cluster string, service string, serviceUUID string, stream string) *LogConfig
	InitializeServiceLogConfig(ctx context.Context, cluster string, service string, serviceUUID string) error
	DeleteServiceLogConfig(ctx context.Context, cluster string, service string, serviceUUID string) error
}
