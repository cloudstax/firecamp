package swarmsvc

import (
	"github.com/docker/docker/client"
	"github.com/golang/glog"
	"golang.org/x/net/context"
)

type SwarmClient struct {
	apiVersion string
}

// NewSwarmClient creates a new SwarmClient instance
func NewSwarmClient() (*SwarmClient, error) {
	cli, err := client.NewEnvClient()
	if err != nil {
		glog.Errorln("NewEnvClient to unix sock error", err)
		return nil, err
	}

	// get the lower api version between client and server
	apiVersion := cli.ClientVersion()
	ver, err := cli.ServerVersion(context.Background())
	if err != nil {
		glog.Errorln("ServerVersion error", err)
		return nil, err
	}

	if apiVersion > ver.APIVersion {
		apiVersion = ver.APIVersion
	}

	glog.Infoln("NewSwarmClient, docker api version", apiVersion)

	s := &SwarmClient{
		apiVersion: apiVersion,
	}
	return s, nil
}

// NewClient returns a new docker swarm client.
// This function is only used by swarmservice, which is used by manageserver.
// And the managerserver runs only on the Swarm manager nodes.
func (s *SwarmClient) NewClient() (*client.Client, error) {
	return client.NewClient(client.DefaultDockerHost, s.apiVersion, nil, nil)
}
