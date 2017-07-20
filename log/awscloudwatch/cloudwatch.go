package awscloudwatch

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/log"
	"github.com/cloudstax/openmanage/utils"
)

const (
	driverName = "awslogs"
	logRegion  = "awslogs-region"
	logGroup   = "awslogs-group"
	// AWS ECS does not support awslogs-stream, instead it introduces stream-prefix.
	// see http://docs.aws.amazon.com/AmazonECS/latest/developerguide/using_awslogs.html
	logStream       = "awslogs-stream"
	logStreamPrefix = "awslogs-stream-prefix"

	defaultLogRetentionDays = 30
)

// Log implements the cloudlog interface for AWS CloudWatchLogs.
type Log struct {
	platform string
	region   string
	cli      *cloudwatchlogs.CloudWatchLogs
}

// NewLog returns a new AWS CloudWatchLogs instance.
func NewLog(sess *session.Session, region string, containerPlatform string) *Log {
	return &Log{
		platform: containerPlatform,
		region:   region,
		cli:      cloudwatchlogs.New(sess),
	}
}

// CreateServiceLogConfig creates the LogConfig for the service to send logs to AWS CloudWatch.
func (l *Log) CreateServiceLogConfig(ctx context.Context, cluster string, service string, serviceUUID string) *cloudlog.LogConfig {
	opts := make(map[string]string)
	opts[logRegion] = l.region
	opts[logGroup] = l.genLogGroupName(cluster, service, serviceUUID)
	// not set the log stream name. By default, awslogs uses container id as the log stream name.
	// TODO switch to use the modified service member aware log driver, which requires docker 17.05.
	// after switch to the new driver, the new driver will get the service member name and
	// overwrite the log stream name.

	return &cloudlog.LogConfig{
		Name:    driverName,
		Options: opts,
	}
}

// CreateLogConfigForStream creates the LogConfig for the stream to send logs to AWS CloudWatch.
// This is used for the service task or the system service that only runs one container.
func (l *Log) CreateLogConfigForStream(ctx context.Context, cluster string, service string, serviceUUID string, stream string) *cloudlog.LogConfig {
	opts := make(map[string]string)
	opts[logRegion] = l.region
	opts[logGroup] = l.genLogGroupName(cluster, service, serviceUUID)
	switch l.platform {
	case common.ContainerPlatformECS:
		opts[logStreamPrefix] = stream
	default:
		opts[logStream] = stream
	}

	return &cloudlog.LogConfig{
		Name:    driverName,
		Options: opts,
	}
}

func (l *Log) genLogGroupName(cluster string, service string, serviceUUID string) string {
	return cluster + common.NameSeparator + service + common.NameSeparator + serviceUUID
}

// InitializeServiceLogConfig creates the CloudWatch log group.
func (l *Log) InitializeServiceLogConfig(ctx context.Context, cluster string, service string, serviceUUID string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	group := l.genLogGroupName(cluster, service, serviceUUID)
	crreq := &cloudwatchlogs.CreateLogGroupInput{
		LogGroupName: aws.String(group),
	}
	_, err := l.cli.CreateLogGroup(crreq)
	if err != nil && err.(awserr.Error).Code() != cloudwatchlogs.ErrCodeResourceAlreadyExistsException {
		glog.Errorln("CreateLogGroup error", err, group, "requuid", requuid)
		return err
	}

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

	group := l.genLogGroupName(cluster, service, serviceUUID)
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
