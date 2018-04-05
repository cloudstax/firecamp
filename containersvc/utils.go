package containersvc

import (
	"strconv"

	"github.com/cloudstax/firecamp/common"
)

const (
	taskSlot = "{{.Task.Slot}}"
)

// GenVolumeSourceForSwarm creates the volume mount source for swarm service.
// https://docs.docker.com/docker-for-aws/persistent-data-volumes/#use-a-unique-volume-per-task-using-ebs.
// so the volume driver could directly know which volume to mount.
func GenVolumeSourceForSwarm(source string) string {
	return source + common.NameSeparator + taskSlot
}

// GenVolumeSourceName creates the volume source name.
func GenVolumeSourceName(source string, index int64) string {
	return source + common.NameSeparator + strconv.FormatInt(index, 10)
}
