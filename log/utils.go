package cloudlog

import (
	"fmt"

	"github.com/cloudstax/firecamp/common"
)

// GenServiceLogGroupName creates the service log group name: firecamp-clustername-servicename-serviceUUID.
func GenServiceLogGroupName(cluster string, service string, serviceUUID string, k8snamespace string) string {
	if len(k8snamespace) != 0 {
		return fmt.Sprintf("%s-%s-%s-%s-%s", common.SystemName, cluster, k8snamespace, service, serviceUUID)
	}
	return fmt.Sprintf("%s-%s-%s-%s", common.SystemName, cluster, service, serviceUUID)
}

// GenServiceMemberLogStreamName creates the log stream name for one service member: membername/hostname/containerID.
func GenServiceMemberLogStreamName(memberName string, hostname string, containerID string) string {
	shortID := containerID
	shortIDLen := 12
	if len(containerID) > shortIDLen {
		shortID = containerID[0:shortIDLen]
	}
	return fmt.Sprintf("%s/%s/%s", memberName, hostname, shortID)
}
