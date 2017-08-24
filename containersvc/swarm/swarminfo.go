package swarmsvc

import (
	"strings"

	"github.com/cloudstax/firecamp/common"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"golang.org/x/net/context"

	"github.com/golang/glog"
)

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

	managers, err := formatManagerAddrs(&info)
	if err != nil {
		return nil, err
	}
	glog.Infoln("swarm managers", managers, info.Swarm.RemoteManagers)

	// check if the local node is Swarm manager
	clusterID := info.Swarm.Cluster.ID
	if !info.Swarm.ControlAvailable {
		// the local node is not Swarm manager, info will not include the clusterID.
		scli := NewSwarmClient(managers, false, "", "", "")
		cli, err := scli.NewClient()
		if err != nil {
			glog.Errorln("NewClient to swarm manager error", err, managers)
			return nil, err
		}
		minfo, err := cli.Info(context.Background())
		if err != nil {
			glog.Errorln("get swarm manager info error", err, managers)
			return nil, err
		}
		clusterID = minfo.Swarm.Cluster.ID
	}

	s := &SwarmInfo{
		localContainerInsID: info.Swarm.NodeID,
		clusterID:           clusterID,
		managers:            managers,
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

func (s *SwarmInfo) GetSwarmManagers() []string {
	// TODO periodically refresh the swarm managers or refresh on failure
	return s.managers
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

func formatManagerAddrs(info *types.Info) ([]string, error) {
	// By default, docker swarm managerAddrs is ip:2377. TLS is enabled to talk with swarm manager on port 2377.
	// The default swarm manager tls files are not exposed for the external usage. If the client wants
	// to talk with swarm manager on 2377, we have to manually create the tls files, and update swarm manager
	// to use our tls files. Then client could talk with swarm manager using the same tls files.
	//
	// Another option is to have docker daemon listen on another port, such as 2376. So the remote docker client
	// in the manageserver and volume driver could talk to swarm manager via 2376 without tls.
	// ref: https://forums.docker.com/t/cannot-connect-to-cluster-created-with-swarm-mode/16826/7
	swarmManagerNonTLSPort := "2376"

	managers := make([]string, len(info.Swarm.RemoteManagers))
	for i, m := range info.Swarm.RemoteManagers {
		// m.Addr format is, ip:2377
		addrs := strings.Split(m.Addr, ":")
		if len(addrs) != 2 {
			glog.Errorln("invalid managerAddrs", m.Addr)
			return nil, common.ErrInternal
		}
		managers[i] = "http://" + addrs[0] + ":" + swarmManagerNonTLSPort
	}

	return managers, nil
}
