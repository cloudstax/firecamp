package server

import (
	"strconv"
	"sync"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/jazzl0ver/firecamp/api/common"
)

const devPrefix = "/dev/memd"
const defaultIPPrefix = "172.31.64."
const defaultCidrBlock = defaultIPPrefix + ".0/20"

type memNetworkInterface struct {
	InterfaceID      string
	ServerInstanceID string
	PrimaryPrivateIP string
	PrivateIPs       map[string]bool
}

type MemServer struct {
	// key: volID, value: devName
	volumes      map[string]string
	netInterface *memNetworkInterface
	vlock        *sync.Mutex
	// count the number of volume creation, used to generate volID
	creationCount int
}

func NewMemServer() *MemServer {
	info := NewMockServerInfo()
	s := &MemServer{
		volumes: map[string]string{},
		netInterface: &memNetworkInterface{
			InterfaceID:      "local-interfaceid",
			ServerInstanceID: info.GetLocalInstanceID(),
			PrimaryPrivateIP: "172.31.64.1",
			PrivateIPs:       map[string]bool{},
		},
		vlock:         &sync.Mutex{},
		creationCount: 0,
	}
	return s
}

func (s *MemServer) AttachVolume(ctx context.Context, volID string, instanceID string, devName string) error {
	return common.ErrInternal
}

func (s *MemServer) WaitVolumeAttached(ctx context.Context, volID string) error {
	return common.ErrInternal
}

func (s *MemServer) GetVolumeState(ctx context.Context, volID string) (state string, err error) {
	return VolumeStateAvailable, nil
}

func (s *MemServer) GetVolumeInfo(ctx context.Context, volID string) (info VolumeInfo, err error) {
	return info, common.ErrInternal
}

func (s *MemServer) DetachVolume(ctx context.Context, volID string, instanceID string, devName string) error {
	return common.ErrInternal
}

func (s *MemServer) WaitVolumeDetached(ctx context.Context, volID string) error {
	return common.ErrInternal
}

func (s *MemServer) CreateVolume(ctx context.Context, opts *CreateVolumeOptions) (volID string, err error) {
	s.vlock.Lock()
	defer s.vlock.Unlock()

	s.creationCount++

	volID = opts.AvailabilityZone + "-" + strconv.Itoa(s.creationCount)
	devName := devPrefix + strconv.Itoa(s.creationCount)

	s.volumes[volID] = devName

	glog.Infoln("created volume", volID, "device", devName)
	return volID, nil
}

func (s *MemServer) WaitVolumeCreated(ctx context.Context, volID string) error {
	return nil
}

func (s *MemServer) DeleteVolume(ctx context.Context, volID string) error {
	s.vlock.Lock()
	defer s.vlock.Unlock()

	devName, ok := s.volumes[volID]
	if !ok {
		glog.Errorln("no such volume", volID)
		return common.ErrNotFound
	}

	delete(s.volumes, volID)

	glog.Infoln("deleted volume", volID, "device", devName)
	return nil
}

func (s *MemServer) GetFirstDeviceName() string {
	return devPrefix + strconv.Itoa(0)
}

func (s *MemServer) GetNextDeviceName(lastDev string) (devName string, err error) {
	devName = devPrefix + strconv.Itoa(s.creationCount+1)
	glog.Infoln("lastDev", lastDev, "assign next device", devName)
	return devName, nil
}

func (s *MemServer) GetNetworkInterfaces(ctx context.Context, cluster string,
	vpcID string, zone string) (netInterfaces []*NetworkInterface, cidrBlock string, err error) {
	s.vlock.Lock()
	defer s.vlock.Unlock()

	netInterfaces = append(netInterfaces, s.copyNetworkInterface())
	return netInterfaces, defaultCidrBlock, nil
}

func (s *MemServer) GetInstanceNetworkInterface(ctx context.Context, instanceID string) (netInterface *NetworkInterface, err error) {
	if instanceID != s.netInterface.ServerInstanceID {
		glog.Errorln("instance not exist", instanceID, "local instance", s.netInterface.ServerInstanceID)
		return nil, common.ErrNotFound
	}

	s.vlock.Lock()
	defer s.vlock.Unlock()

	return s.copyNetworkInterface(), nil
}

func (s *MemServer) copyNetworkInterface() *NetworkInterface {
	ips := []string{}
	for ip := range s.netInterface.PrivateIPs {
		ips = append(ips, ip)
	}

	netInterface := &NetworkInterface{
		InterfaceID:      s.netInterface.InterfaceID,
		ServerInstanceID: s.netInterface.ServerInstanceID,
		PrimaryPrivateIP: s.netInterface.PrimaryPrivateIP,
		PrivateIPs:       ips,
	}
	return netInterface
}

func (s *MemServer) UnassignStaticIP(ctx context.Context, networkInterfaceID string, staticIP string) error {
	if networkInterfaceID != s.netInterface.InterfaceID {
		glog.Errorln("networkInterfaceID not exist", networkInterfaceID, "local id", s.netInterface.InterfaceID)
		return common.ErrNotFound
	}

	s.vlock.Lock()
	defer s.vlock.Unlock()

	delete(s.netInterface.PrivateIPs, staticIP)
	return nil
}

func (s *MemServer) AssignStaticIP(ctx context.Context, networkInterfaceID string, staticIP string) error {
	if networkInterfaceID != s.netInterface.InterfaceID {
		glog.Errorln("networkInterfaceID not exist", networkInterfaceID, "local id", s.netInterface.InterfaceID)
		return common.ErrNotFound
	}

	s.vlock.Lock()
	defer s.vlock.Unlock()

	s.netInterface.PrivateIPs[staticIP] = true
	return nil
}
