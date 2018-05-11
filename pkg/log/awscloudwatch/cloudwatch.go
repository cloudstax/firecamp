package awscloudwatch

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/pkg/log"
	"github.com/cloudstax/firecamp/pkg/utils"
)

const (
	driverName = "awslogs"
	logRegion  = "awslogs-region"
	logGroup   = "awslogs-group"
	// AWS ECS does not support awslogs-stream, instead it introduces stream-prefix.
	// see http://docs.aws.amazon.com/AmazonECS/latest/developerguide/using_awslogs.html
	logStream       = "awslogs-stream"
	logStreamPrefix = "awslogs-stream-prefix"

	// Currently cloudwatch logs takes every log line as one event.
	// Docker awslogs driver supports multiline pattern from 17.05.
	// https://docs.docker.com/engine/admin/logging/awslogs/
	// TODO enable it later.
	// It is not urgent to enable it now. AWS CloudWatch console and cli support "text" view mode.
	// aws logs get-log-events --log-group-name A --log-stream-name a --output text > a.log

	defaultLogRetentionDays = 120
)

// Log implements the cloudlog interface for AWS CloudWatchLogs.
type Log struct {
	platform     string
	k8snamespace string
	region       string
	cli          *cloudwatchlogs.CloudWatchLogs
}

// NewLog returns a new AWS CloudWatchLogs instance.
func NewLog(sess *session.Session, region string, containerPlatform string, k8snamespace string) *Log {
	return &Log{
		platform:     containerPlatform,
		k8snamespace: k8snamespace,
		region:       region,
		cli:          cloudwatchlogs.New(sess),
	}
}

// CreateServiceLogConfig creates the LogConfig for the service to send logs to AWS CloudWatch.
func (l *Log) CreateServiceLogConfig(ctx context.Context, cluster string, service string, serviceUUID string) *cloudlog.LogConfig {
	opts := make(map[string]string)
	opts[logRegion] = l.region
	opts[logGroup] = cloudlog.GenServiceLogGroupName(cluster, service, serviceUUID, l.k8snamespace)

	// not set the log stream name. By default, awslogs uses container id as the log stream name.
	// FireCamp has a log driver cloudstax/firecamp-log, which creates the log stream with the
	// service member name as prefix. So we could easily track the logs of one service member.
	// While, AWS ECS does not support custom log driver. FireCamp amazon-ecs-agent will replace
	// the log driver with firecamp-log plugin. We still set awslogs as the default driver.
	// Just in case if firecamp-amazon-ecs-agent is replaced by aws amazon-ecs-agent, the logs will
	// still be sent to CloudWatch, and we could manually fix the log stream name later.
	switch l.platform {
	case common.ContainerPlatformECS:
		// not set LogServiceUUIDKey, which is not supported by awslog driver.
		// The firecamp amazon-ecs-agent will replace the log driver to firecamp-log-driver and set the LogServiceUUIDKey.
		return &cloudlog.LogConfig{
			Name:    driverName,
			Options: opts,
		}
	case common.ContainerPlatformSwarm:
		// set the LogServiceUUIDKey for firecamp-log-driver to get the serviceUUID.
		opts[common.LogServiceUUIDKey] = serviceUUID
		return &cloudlog.LogConfig{
			Name:    common.LogDriverName,
			Options: opts,
		}
	case common.ContainerPlatformK8s:
		// k8s uses the Fluentd daemonset, which will set the LogGroup and LogStream names.
		glog.Infoln("no log config set for k8s, cluster", cluster, "service", service,
			"serviceUUID", serviceUUID, "namespace", l.k8snamespace)
		return nil
	}
	panic(fmt.Sprintf("unsupported container platform %s, cluster %s, service %s", l.platform, cluster, service))
	return nil
}

// CreateStreamLogConfig creates the LogConfig for the stateless service, such as the management service,
// Kafka Manager service, or the task of the stateful service, such as init task.
// The task log directly uses the awslogs driver. There is no need to use firecamp-log-driver.
func (l *Log) CreateStreamLogConfig(ctx context.Context, cluster string, service string, serviceUUID string, stream string) *cloudlog.LogConfig {
	opts := make(map[string]string)
	opts[logRegion] = l.region
	opts[logGroup] = cloudlog.GenServiceLogGroupName(cluster, service, serviceUUID, l.k8snamespace)

	switch l.platform {
	case common.ContainerPlatformECS:
		opts[logStreamPrefix] = stream
		return &cloudlog.LogConfig{
			Name:    driverName,
			Options: opts,
		}
	case common.ContainerPlatformSwarm:
		// set the LogServiceUUIDKey for firecamp-log-driver to get the serviceUUID.
		opts[common.LogServiceUUIDKey] = serviceUUID
		// set the LogServiceMemberKey directly
		opts[common.LogServiceMemberKey] = stream
		return &cloudlog.LogConfig{
			Name:    common.LogDriverName,
			Options: opts,
		}
	case common.ContainerPlatformK8s:
		// k8s uses the Fluentd daemonset, which will set the LogGroup and LogStream names.
		glog.Infoln("no log config set for k8s, cluster", cluster, "service", service,
			"serviceUUID", serviceUUID, "namespace", l.k8snamespace)
		return nil
	}
	panic(fmt.Sprintf("unsupported container platform %s, cluster %s, service %s", l.platform, cluster, service))
	return nil
}

// InitializeServiceLogConfig creates the CloudWatch log group.
func (l *Log) InitializeServiceLogConfig(ctx context.Context, cluster string, service string, serviceUUID string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	group := cloudlog.GenServiceLogGroupName(cluster, service, serviceUUID, l.k8snamespace)
	crreq := &cloudwatchlogs.CreateLogGroupInput{
		LogGroupName: aws.String(group),
	}
	_, err := l.cli.CreateLogGroup(crreq)
	if err != nil && err.(awserr.Error).Code() != cloudwatchlogs.ErrCodeResourceAlreadyExistsException {
		glog.Errorln("CreateLogGroup error", err, group, "requuid", requuid)
		return err
	}

	// TODO make the RetentionInDays configurable
	putReq := &cloudwatchlogs.PutRetentionPolicyInput{
		LogGroupName:    aws.String(group),
		RetentionInDays: aws.Int64(defaultLogRetentionDays),
	}
	_, err = l.cli.PutRetentionPolicy(putReq)
	if err != nil {
		glog.Errorln("PutRetentionPolicy error", err, group, "requuid", requuid)
		return err
	}

	glog.Infoln("created log group", group, "requuid", requuid)
	return nil
}

// DeleteServiceLogConfig deletes the CloudWatch log group.
func (l *Log) DeleteServiceLogConfig(ctx context.Context, cluster string, service string, serviceUUID string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	group := cloudlog.GenServiceLogGroupName(cluster, service, serviceUUID, l.k8snamespace)
	req := &cloudwatchlogs.DeleteLogGroupInput{
		LogGroupName: aws.String(group),
	}
	_, err := l.cli.DeleteLogGroup(req)
	if err != nil && err.(awserr.Error).Code() != cloudwatchlogs.ErrCodeResourceNotFoundException {
		glog.Errorln("DeleteLogGroup error", err, group, "requuid", requuid)
		return err
	}

	glog.Infoln("deleted log group", group, "requuid", requuid)
	return nil
}
