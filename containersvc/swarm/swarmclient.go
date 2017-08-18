package swarmsvc

import (
	"net/http"

	"github.com/docker/docker/client"
	"github.com/docker/go-connections/tlsconfig"
	"github.com/golang/glog"
)

// the current max docker version of Amazon Linux is 1.27
const clientVersion = "1.27"

type SwarmClient struct {
	// the swarm manager listening address, such as http://127.0.0.1:2376
	managerAddrs []string
	tlsVerify    bool
	caFile       string
	certFile     string
	keyFile      string
}

// NewSwarmClient creates a new SwarmClient instance
func NewSwarmClient(managerAddrs []string, tlsVerify bool, caFile, certFile, keyFile string) *SwarmClient {
	s := &SwarmClient{
		managerAddrs: managerAddrs,
		tlsVerify:    tlsVerify,
		caFile:       caFile,
		certFile:     certFile,
		keyFile:      keyFile,
	}
	return s
}

// NewClient returns a new docker swarm client
func (s *SwarmClient) NewClient() (*client.Client, error) {
	var httpclient *http.Client
	if s.tlsVerify {
		options := tlsconfig.Options{
			CAFile:   s.caFile,
			CertFile: s.certFile,
			KeyFile:  s.keyFile,
			//InsecureSkipVerify: s.tlsVerify,
		}
		tlsc, err := tlsconfig.Client(options)
		if err != nil {
			glog.Errorln("tlsconfig.Client error", err)
			return nil, err
		}

		httpclient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsc,
			},
		}
	}

	// TODO only talk with the first manager. Support retrying other managers on failure.
	return client.NewClient(s.managerAddrs[0], clientVersion, httpclient, nil)
}
