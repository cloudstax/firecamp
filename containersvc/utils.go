package containersvc

import (
	"github.com/cloudstax/openmanage/common"
)

// GenJSONFileLogDriver creates the default json-file log driver.
func GenJSONFileLogDriver() *LogDriver {
	return &LogDriver{
		Name:    "json-file",
		Options: make(map[string]string),
	}
}

// GenServiceAWSLogDriver creates the LogDriver for the service to send logs to AWS CloudWatch.
func GenServiceAWSLogDriver(region string, cluster string, service string, serviceUUID string) *LogDriver {
	opts := make(map[string]string)
	opts[common.AWSLOGS_REGION] = region
	opts[common.AWSLOGS_GROUP] = genLogGroupName(cluster, service, serviceUUID)
	// not set the log stream name. By default, awslogs uses container id as the log stream name.
	// TODO switch to use the modified service member aware log driver, which requires docker 17.05.
	// after switch to the new driver, the new driver will get the service member name and
	// overwrite the log stream name.

	return &LogDriver{
		Name:    common.LOGDRIVER_AWSLOGS,
		Options: opts,
	}
}

// GenAWSLogDriverForStream creates the LogDriver for the stream to send logs to AWS CloudWatch.
// This is used for the service task or the system service that only runs one container.
func GenAWSLogDriverForStream(region string, cluster string, service string, serviceUUID string, stream string) *LogDriver {
	opts := make(map[string]string)
	opts[common.AWSLOGS_REGION] = region
	opts[common.AWSLOGS_GROUP] = genLogGroupName(cluster, service, serviceUUID)
	opts[common.AWSLOGS_STREAM] = stream

	return &LogDriver{
		Name:    common.LOGDRIVER_AWSLOGS,
		Options: opts,
	}
}

func genLogGroupName(cluster string, service string, serviceUUID string) string {
	return cluster + common.NameSeparator + service + common.NameSeparator + serviceUUID
}
