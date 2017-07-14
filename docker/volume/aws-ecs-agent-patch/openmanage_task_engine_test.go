package engine

import (
	"testing"

	docker "github.com/fsouza/go-dockerclient"
)

func TestDockerConfig(t *testing.T) {
	cluster := "cluster1"
	taskArn := "task1"
	taskDef := "taskDef1"
	hostConfig := &docker.HostConfig{}

	binds := make([]string, 2)

	// negative case, no volume
	set := setVolumeDriver(hostConfig, cluster, taskArn, taskDef)
	if set {
		t.Fatalf("expect not valid volume for HostConfig %s", hostConfig)
	}

	// negative case, has VolumesFrom
	binds[0] = "/test-volume:/usr/test-volume"
	hostConfig.VolumesFrom = binds
	set = setVolumeDriver(hostConfig, cluster, taskArn, taskDef)
	if set {
		t.Fatalf("expect not valid volume for HostConfig %s", hostConfig)
	}

	// negative case, binds 1 local volume
	hostConfig.VolumesFrom = nil
	hostConfig.Binds = binds
	set = setVolumeDriver(hostConfig, cluster, taskArn, taskDef)
	if set {
		t.Fatalf("expect not valid volume for HostConfig %s", hostConfig)
	}

	// negative case, binds 2 volumes
	binds[1] = "/vol2:/usr/vol2"
	hostConfig.Binds = binds
	set = setVolumeDriver(hostConfig, cluster, taskArn, taskDef)
	if set {
		t.Fatalf("expect not valid volume for HostConfig %s", hostConfig)
	}

	// binds 1 volume
	binds = make([]string, 1)
	binds[0] = "serviceuuid:/usr/vol"
	hostConfig.Binds = binds
	set = setVolumeDriver(hostConfig, cluster, taskArn, taskDef)
	if !set {
		t.Fatalf("expect valid volume for HostConfig %s", hostConfig)
	}
}
