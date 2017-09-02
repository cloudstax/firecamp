package swarmsvc

import (
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"golang.org/x/net/context"

	"github.com/golang/glog"
)

type SwarmInfo struct {
	localContainerInsID string
	clusterID           string
}

func NewSwarmInfo(clusterName string) (*SwarmInfo, error) {
	info, err := getDockerInfo()
	if err != nil {
		glog.Errorln("get docker info error", err)
		return nil, err
	}

	glog.Infoln("local node swarm info", info.Swarm)

	s := &SwarmInfo{
		localContainerInsID: info.Swarm.NodeID,
		clusterID:           clusterName,
	}

	glog.Infoln("NewSwarmInfo nodeID", s.localContainerInsID, "clusterID", s.clusterID)
	return s, nil
}

func (s *SwarmInfo) GetLocalContainerInstanceID() string {
	return s.localContainerInsID
}

func (s *SwarmInfo) GetContainerClusterID() string {
	return s.clusterID
}

func getDockerInfo() (info types.Info, err error) {
	cli, err := client.NewEnvClient()
	if err != nil {
		glog.Errorln("NewClient error", err)
		return info, err
	}

	// get info
	return cli.Info(context.Background())
}
