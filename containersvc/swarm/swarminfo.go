package swarmsvc

import (
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"golang.org/x/net/context"

	"github.com/golang/glog"
)

const localDockerSockAddr = "unix:///var/run/docker.sock"

type SwarmInfo struct {
	localContainerInsID string
	clusterID           string
	managers            []string
}

func NewSwarmInfo() (*SwarmInfo, error) {
	info, err := getDockerInfo()
	if err != nil {
		glog.Errorln("get docker info error", err)
		return nil, err
	}

	glog.Infoln("local node swarm info", info.Swarm)

	if len(info.Swarm.RemoteManagers) == 0 {
		glog.Errorln("local node does not have swarm RemoteManagers info", info.Swarm)
		return nil, err
	}

	managers := make([]string, len(info.Swarm.RemoteManagers))
	for i, m := range info.Swarm.RemoteManagers {
		managers[i] = m.Addr
	}

	s := &SwarmInfo{
		localContainerInsID: info.Swarm.NodeID,
		clusterID:           info.Swarm.Cluster.ID,
		managers:            managers,
	}
	return s, nil
}

func (s *SwarmInfo) GetLocalContainerInstanceID() string {
	return s.localContainerInsID
}

func (s *SwarmInfo) GetContainerClusterID() string {
	return s.clusterID
}

func (s *SwarmInfo) GetSwarmManagers() []string {
	// TODO periodically refresh the swarm managers or refresh on failure
	return s.managers
}

func getDockerInfo() (info types.Info, err error) {
	cli, err := client.NewClient(localDockerSockAddr, client.DefaultVersion, nil, nil)
	if err != nil {
		glog.Errorln("NewClient error", err, "local docker", localDockerSockAddr)
		return info, err
	}

	// get info
	return cli.Info(context.Background())
}
