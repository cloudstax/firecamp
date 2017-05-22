package server

import (
	"strconv"
	"sync"

	"github.com/golang/glog"
	"github.com/openconnectio/openmanage/common"
)

const devPrefix = "/dev/memd"

type MemServer struct {
	// key: volID, value: devName
	volumes map[string]string
	vlock   *sync.Mutex
	// count the number of volume creation, used to generate volID
	creationCount int
}

func NewMemServer() *MemServer {
	s := &MemServer{
		volumes:       map[string]string{},
		vlock:         &sync.Mutex{},
		creationCount: 0,
	}
	return s
}

func (s *MemServer) AttachVolume(volID string, instanceID string, devName string) error {
	return common.ErrInternal
}

func (s *MemServer) WaitVolumeAttached(volID string) error {
	return common.ErrInternal
}

func (s *MemServer) GetVolumeState(volID string) (state string, err error) {
	return "", common.ErrInternal
}

func (s *MemServer) GetVolumeInfo(volID string) (info VolumeInfo, err error) {
	return info, common.ErrInternal
}

func (s *MemServer) DetachVolume(volID string, instanceID string, devName string) error {
	return common.ErrInternal
}

func (s *MemServer) WaitVolumeDetached(volID string) error {
	return common.ErrInternal
}

func (s *MemServer) CreateVolume(az string, volSizeGB int64) (volID string, err error) {
	s.vlock.Lock()
	defer s.vlock.Unlock()

	s.creationCount++

	volID = az + "-" + strconv.Itoa(s.creationCount)
	devName := devPrefix + strconv.Itoa(s.creationCount)

	s.volumes[volID] = devName

	glog.Infoln("created volume", volID, "device", devName)
	return volID, nil
}

func (s *MemServer) WaitVolumeCreated(volID string) error {
	return nil
}

func (s *MemServer) DeleteVolume(volID string) error {
	s.vlock.Lock()
	defer s.vlock.Unlock()

	devName, ok := s.volumes[volID]
	if !ok {
		glog.Errorln("no such volume", volID)
		return common.ErrInternal
	}

	delete(s.volumes, volID)

	glog.Infoln("deleted volume", volID, "device", devName)
	return nil
}

func (s *MemServer) GetControlDBDeviceName() string {
	return devPrefix + strconv.Itoa(0)
}

func (s *MemServer) GetFirstDeviceName() string {
	return devPrefix + strconv.Itoa(1)
}

func (s *MemServer) GetNextDeviceName(lastDev string) (devName string, err error) {
	devName = devPrefix + strconv.Itoa(s.creationCount+1)
	glog.Infoln("lastDev", lastDev, "assign next device", devName)
	return devName, nil
}
