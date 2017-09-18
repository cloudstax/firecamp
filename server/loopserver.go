package server

import (
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
)

const (
	LoopFileDir   = "/tmp/"
	LoopDevPrefix = "/dev/loop"

	loopNetInterfaceIDPrefix   = "loopnet"
	loopServerInstanceIDPrefix = "loopserver"
	loopCidrPrefix             = "172.31.64."
	loopCidrIP                 = "172.31.64.0"
	loopCidrBlock              = loopCidrIP + "/20"
	loopCidrIPNet              = "172.31.64.0/20"
)

type LoopServer struct {
	vlock *sync.Mutex
	// key: volID, value: devName
	volumes map[string]string
	// count the number of volume creation, used to generate volID
	creationCount int
	// key: network interface id
	netInterfaces map[string]*memNetworkInterface
}

func NewLoopServer() *LoopServer {
	s := &LoopServer{
		vlock:         &sync.Mutex{},
		volumes:       map[string]string{},
		creationCount: 0,
		netInterfaces: map[string]*memNetworkInterface{},
	}
	s.AddNetworkInterface()
	return s
}

func (s *LoopServer) AttachVolume(ctx context.Context, volID string, instanceID string, devName string) error {
	s.vlock.Lock()
	defer s.vlock.Unlock()

	// check devName
	ok := strings.HasPrefix(devName, LoopDevPrefix)
	if !ok {
		glog.Errorln("unsupported devName", devName)
		return common.ErrInternal
	}

	// check if volume exists
	dev, ok := s.volumes[volID]
	if !ok || len(dev) != 0 {
		glog.Errorln("no such volume or already attached", volID, "dev", dev)
		return common.ErrInternal
	}

	// attach loopfile to dev
	loopfile := s.loopfilePath(volID)

	var args []string
	args = append(args, "losetup", devName, loopfile)
	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		glog.Errorln(args, "failed, output", string(output[:]), "error", err)
		return err
	}

	// update devName
	s.volumes[volID] = devName

	glog.Infoln(args)
	return nil
}

func (s *LoopServer) WaitVolumeAttached(ctx context.Context, volID string) error {
	return nil
}

func (s *LoopServer) GetVolumeState(ctx context.Context, volID string) (state string, err error) {
	s.vlock.Lock()
	defer s.vlock.Unlock()

	dev, ok := s.volumes[volID]
	if !ok {
		glog.Errorln("no such volume", volID)
		return "error", common.ErrInternal
	}

	if len(dev) == 0 {
		glog.Infoln("volume", volID, "available")
		return VolumeStateAvailable, nil
	}

	glog.Infoln("volume", volID, "in-use, dev", dev)
	return VolumeStateInUse, nil
}

func (s *LoopServer) GetVolumeInfo(ctx context.Context, volID string) (info VolumeInfo, err error) {
	s.vlock.Lock()
	defer s.vlock.Unlock()

	dev, ok := s.volumes[volID]
	if !ok {
		glog.Errorln("no such volume", volID)
		return info, common.ErrInternal
	}

	info = VolumeInfo{
		VolID: volID,
		State: VolumeStateInUse,
	}

	if len(dev) == 0 {
		info.State = VolumeStateAvailable
	}

	glog.Infoln("volume", info)
	return info, nil
}

func (s *LoopServer) DetachVolume(ctx context.Context, volID string, instanceID string, devName string) error {
	s.vlock.Lock()
	defer s.vlock.Unlock()

	dev, ok := s.volumes[volID]
	if !ok {
		glog.Errorln("no such volume, volID", volID, "dev", dev)
		return common.ErrInternal
	}
	if len(dev) == 0 {
		glog.Errorln("volume not attached, volID", volID, "dev", dev)
		return ErrVolumeIncorrectState
	}

	// check devName
	if dev != devName {
		glog.Errorln("wrong devName", devName, "for volume", volID, dev)
		return common.ErrInternal
	}

	// detach loop device
	var args []string
	args = append(args, "losetup", "-d", devName)
	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		glog.Errorln(args, "failed, output", string(output[:]), "error", err)
		return err
	}

	// reset volume attached devName
	s.volumes[volID] = ""

	glog.Infoln(args)
	return nil
}

func (s *LoopServer) WaitVolumeDetached(ctx context.Context, volID string) error {
	return nil
}

func (s *LoopServer) CreateVolume(ctx context.Context, az string, volSizeGB int64) (volID string, err error) {
	s.vlock.Lock()
	defer s.vlock.Unlock()

	s.creationCount++

	volID = az + "-" + strconv.Itoa(s.creationCount)

	// create loopfile
	loopfile := s.loopfilePath(volID)
	err = s.checkAndCreateLoopFile(loopfile)
	if err != nil {
		glog.Errorln("failed to create loop file", loopfile, "volID", volID, "error", err)
		return volID, err
	}

	// create volume with empty devName
	s.volumes[volID] = ""

	glog.Infoln("created volume", volID, "loopfile", loopfile)
	return volID, nil
}

func (s *LoopServer) WaitVolumeCreated(ctx context.Context, volID string) error {
	return nil
}

func (s *LoopServer) DeleteVolume(ctx context.Context, volID string) error {
	s.vlock.Lock()
	defer s.vlock.Unlock()

	devName, ok := s.volumes[volID]
	if !ok || len(devName) != 0 {
		glog.Errorln("no such volume or in-use", volID, "devName", devName)
		return common.ErrInternal
	}

	// delete the loopfile
	loopfile := s.loopfilePath(volID)
	err := os.Remove(loopfile)
	if err != nil {
		glog.Errorln("failed to remove loopfile", loopfile, "volID", volID, "error", err)
		return err
	}

	delete(s.volumes, volID)

	glog.Infoln("deleted volume", volID, "loopfile", loopfile)
	return nil
}

func (s *LoopServer) GetControlDBDeviceName() string {
	return "/dev/loop0"
}

func (s *LoopServer) GetFirstDeviceName() string {
	return "/dev/loop1"
}

func (s *LoopServer) GetNextDeviceName(lastDev string) (devName string, err error) {
	// format check
	devicePrefix := "/dev/loop"

	if !strings.HasPrefix(lastDev, devicePrefix) {
		glog.Errorln("device should have prefix", devicePrefix, "lastDev", lastDev)
		return "", common.ErrInternal
	}

	devSeq := strings.TrimPrefix(lastDev, devicePrefix)

	if len(devSeq) != 1 || devSeq == "z" {
		glog.Errorln("invalid or last lastDev", lastDev)
		return "", common.ErrInternal
	}

	if devSeq == "9" {
		devName = devicePrefix + "a"
	} else {
		devName = devicePrefix + string(devSeq[0]+1)
	}
	glog.Infoln("lastDev", lastDev, "assign next device", devName)
	return devName, nil
}

func (s *LoopServer) AddNetworkInterface() {
	idx := len(s.netInterfaces)

	netint := &memNetworkInterface{
		InterfaceID:      loopNetInterfaceIDPrefix + strconv.Itoa(idx),
		ServerInstanceID: loopServerInstanceIDPrefix + strconv.Itoa(idx),
		PrimaryPrivateIP: loopCidrPrefix + strconv.Itoa(idx),
		PrivateIPs:       map[string]bool{},
	}

	s.netInterfaces[netint.InterfaceID] = netint
}

func (s *LoopServer) GetCidrBlock() (cidrPrefix string, cidrIP string, cidrBlock string, cidrIPNet string) {
	return loopCidrPrefix, loopCidrIP, loopCidrBlock, loopCidrIPNet
}

func (s *LoopServer) GetNetworkInterfaces(ctx context.Context, cluster string,
	vpcID string, zone string) (netInterfaces []*NetworkInterface, cidrBlock string, err error) {
	s.vlock.Lock()
	defer s.vlock.Unlock()

	for _, memnetint := range s.netInterfaces {
		netInterfaces = append(netInterfaces, s.copyNetworkInterface(memnetint))
	}

	sort.Slice(netInterfaces, func(i, j int) bool {
		return netInterfaces[i].InterfaceID < netInterfaces[j].InterfaceID
	})

	return netInterfaces, loopCidrBlock, nil
}

func (s *LoopServer) GetInstanceNetworkInterface(ctx context.Context, instanceID string) (netInterface *NetworkInterface, err error) {
	s.vlock.Lock()
	defer s.vlock.Unlock()

	for _, memnetint := range s.netInterfaces {
		if memnetint.ServerInstanceID == instanceID {
			return s.copyNetworkInterface(memnetint), nil
		}
	}

	glog.Errorln("instance not exist", instanceID, s.netInterfaces)
	return nil, common.ErrNotFound
}

func (s *LoopServer) copyNetworkInterface(memnetint *memNetworkInterface) *NetworkInterface {
	ips := []string{}
	for ip := range memnetint.PrivateIPs {
		ips = append(ips, ip)
	}

	netInterface := &NetworkInterface{
		InterfaceID:      memnetint.InterfaceID,
		ServerInstanceID: memnetint.ServerInstanceID,
		PrimaryPrivateIP: memnetint.PrimaryPrivateIP,
		PrivateIPs:       ips,
	}
	return netInterface
}

func (s *LoopServer) UnassignStaticIP(ctx context.Context, networkInterfaceID string, staticIP string) error {
	s.vlock.Lock()
	defer s.vlock.Unlock()

	memnetint, ok := s.netInterfaces[networkInterfaceID]
	if !ok {
		glog.Errorln("networkInterfaceID not exist", networkInterfaceID, s.netInterfaces)
		return common.ErrNotFound
	}

	delete(memnetint.PrivateIPs, staticIP)
	return nil
}

func (s *LoopServer) AssignStaticIP(ctx context.Context, networkInterfaceID string, staticIP string) error {
	s.vlock.Lock()
	defer s.vlock.Unlock()

	memnetint, ok := s.netInterfaces[networkInterfaceID]
	if !ok {
		glog.Errorln("networkInterfaceID not exist", networkInterfaceID, s.netInterfaces)
		return common.ErrNotFound
	}

	memnetint.PrivateIPs[staticIP] = true
	return nil
}

func (s *LoopServer) loopfilePath(volID string) string {
	return LoopFileDir + volID
}

func (s *LoopServer) checkAndCreateLoopFile(loopfile string) error {
	// check and create loopfile
	fi, err := os.Lstat(loopfile)
	if err != nil {
		if !os.IsNotExist(err) {
			glog.Errorln("loopfile", loopfile, "error", err)
			return err
		}

		// loopfile not exist, create it
		var args []string
		args = append(args, "dd", "if=/dev/zero", "of="+loopfile, "bs=1M", "count=20")
		cmd := exec.Command(args[0], args[1:]...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			glog.Errorln(args, "failed, output", string(output[:]), "error", err)
			return err
		}

		glog.Infoln("created loopfile", loopfile)
		return nil
	}

	// loopfile exists
	if fi == nil || fi.IsDir() {
		glog.Errorln("loopfile", loopfile, "is at wrong stat", fi)
		return common.ErrInternal
	}

	glog.Infoln("loopfile", loopfile, "stat", fi)
	return nil
}
