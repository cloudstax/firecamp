package engine

import (
	"errors"
	"strings"

	"github.com/aws/amazon-ecs-agent/agent/api"

	"github.com/cihub/seelog"
	docker "github.com/fsouza/go-dockerclient"
)

type VolumeDriverConfigError struct {
	msg string
}

func (err *VolumeDriverConfigError) Error() string     { return err.msg }
func (err *VolumeDriverConfigError) ErrorName() string { return "VolumeDriverConfigError" }

// Define here again to avoid the dependency on githut.com/cloudstax/firecamp
const volumeDriver = "cloudstax/firecamp-volume"
const versionKey = "VERSION"

// Create a taskDefinition at AWS ECS console, created one volume, "Name" my-ecs-volume,
// "Source Path" /test-volume. Then in "Container Definitions", create "Mount Points",
// "Container Path" /usr/test-volume, "Source Volume" my-ecs-volume.
//
// ECS will not pass my-ecs-volume to agent. agent.createContainer() will directly get
// "/test-volume:/usr/test-volume" in docker.HostConfig.
// So to pass in the ServiceUUID, when creating volume at ECS console, should set both
// "Name" and "Source Path" to ServiceUUID.
//
// docker.Config is not the place for volume. At createContainer(), task.DockerConfig()
// calls task.dockerConfigVolumes(), which looks does nothing. comments say, "you can
// handle most volume mount types in the HostConfig at run-time".

// AddVolumeDriver updates the docker container volume config, if the task
// belongs to the service registered to us.
// update HostConfig with "-v volumename:/data --volume-driver=ourDriver"
func AddVolumeDriver(hostConfig *docker.HostConfig, container *api.Container, cluster string, taskArn string, taskDef string) (newConfig *docker.HostConfig, err *VolumeDriverConfigError) {
	if setVolumeDriver(hostConfig, cluster, taskArn, taskDef) {
		// get the volume driver version from environment
		version, ok := container.Environment[versionKey]
		if !ok {
			err := errors.New("firecamp container environment does not have version")
			seelog.Error("firecamp container environment does not have version", container.Environment,
				"cluster", cluster, "taskArn", taskArn, "taskDef", taskDef)
			return nil, &VolumeDriverConfigError{err.Error()}
		}

		// set volume-driver in docker HostConfig
		hostConfig.VolumeDriver = volumeDriver + ":" + version

		seelog.Info("Add volume driver", hostConfig.VolumeDriver,
			"to HostConfig Binds", hostConfig.Binds,
			"cluster", cluster, "taskArn", taskArn, "taskDef", taskDef)
	}

	return hostConfig, nil
}

func setVolumeDriver(hostConfig *docker.HostConfig, cluster string, taskArn string, taskDef string) bool {
	if len(hostConfig.VolumesFrom) != 0 {
		seelog.Info("task HostConfig has VolumeFrom, not a valid volume, cluster",
			cluster, "taskArn", taskArn, "taskDef", taskDef)
		return false
	}

	if len(hostConfig.Binds) == 0 || len(hostConfig.Binds) > 1 {
		seelog.Info("HostConfig binds 0 volume or more than 1 volume2", hostConfig.Binds,
			"cluster", cluster, "taskArn", taskArn, "taskDef", taskDef)
		return false
	}

	// binds one volume, further check whether is valid volume.
	// currently only support one volume per task

	// check if HostConfig Binds is correct, should be ServiceUUID:/mountPathInContainer
	volSep := ":/"
	s := strings.Split(hostConfig.Binds[0], volSep)
	if len(s) != 2 {
		seelog.Info("Bad HostConfig Binds", hostConfig.Binds,
			"cluster", cluster, "taskArn", taskArn, "taskDef", taskDef)
		return false
	}

	// TODO should we check if service is created in DB? For now, ECS allows non-path
	// style "Source Path" for one volume.
	if strings.HasPrefix(s[0], "/") {
		seelog.Info("HostConfig Binds is for local path", hostConfig.Binds,
			"cluster", cluster, "taskArn", taskArn, "taskDef", taskDef)
		return false
	}

	seelog.Info("HostConfig binds the valid volume", hostConfig.Binds,
		"cluster", cluster, "taskArn", taskArn, "taskDef", taskDef)
	return true
}
