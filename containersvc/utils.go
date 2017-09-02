package containersvc

import (
	"strconv"
	"strings"

	"github.com/cloudstax/firecamp/common"
)

const (
	volumeTaskSlot = "{{.Task.Slot}}"
)

// GenVolumeSourceForSwarm creates the volume mount source for swarm service.
// https://docs.docker.com/docker-for-aws/persistent-data-volumes/#use-a-unique-volume-per-task-using-ebs.
// so the volume driver could directly know which volume to mount.
func GenVolumeSourceForSwarm(serviceUUID string) string {
	return serviceUUID + common.NameSeparator + volumeTaskSlot
}

// VolumeSourceHasTaskSlot checks whether the volume name includes the task slot.
// Docker swarm could pass the volume name with task slot, such as serviceUUID-1.
func VolumeSourceHasTaskSlot(volName string) bool {
	return strings.Contains(volName, common.NameSeparator)
}

// GenVolumeSourceName creates the volume source name.
func GenVolumeSourceName(serviceUUID string, index int64) string {
	return serviceUUID + common.NameSeparator + strconv.FormatInt(index, 10)
}

// ParseVolumeSource parses the volume name. return serviceUUID, task slot, error.
func ParseVolumeSource(volName string) (string, int64, error) {
	strs := strings.Split(volName, common.NameSeparator)
	if len(strs) != 2 {
		return "", -1, common.ErrInvalidArgs
	}

	i, err := strconv.ParseInt(strs[1], 10, 64)
	return strs[0], i, err
}
