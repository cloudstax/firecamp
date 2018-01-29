package jsonfilelog

import (
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/log"
)

type Log struct {
	cfg *cloudlog.LogConfig
}

func NewLog() *Log {
	return &Log{
		cfg: CreateJSONFileLogConfig(),
	}
}

func CreateJSONFileLogConfig() *cloudlog.LogConfig {
	return &cloudlog.LogConfig{
		Name:    "json-file",
		Options: make(map[string]string),
	}
}

func (l *Log) CreateServiceLogConfig(ctx context.Context, cluster string, service string, serviceUUID string) *cloudlog.LogConfig {
	return l.cfg
}

func (l *Log) CreateTaskLogConfig(ctx context.Context, cluster string, service string, serviceUUID string, taskType string) *cloudlog.LogConfig {
	return l.cfg
}

func (l *Log) InitializeServiceLogConfig(ctx context.Context, cluster string, service string, serviceUUID string) error {
	return nil
}

func (l *Log) DeleteServiceLogConfig(ctx context.Context, cluster string, service string, serviceUUID string) error {
	return nil
}
