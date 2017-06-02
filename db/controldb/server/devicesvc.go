package controldbserver

import (
	"path"
	"sync"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/db"
	"github.com/cloudstax/openmanage/db/controldb"
	pb "github.com/cloudstax/openmanage/db/controldb/protocols"
	"github.com/cloudstax/openmanage/utils"
)

type deviceSvc struct {
	clusterName string

	// the lock to serialize the device operations
	lock *sync.Mutex
	// the in-memory copy of the last device
	allDevices []*pb.Device

	// manage the device related version files
	store *versionfilestore
}

func newDeviceSvc(rootDir string, clusterName string) (*deviceSvc, error) {
	store, err := newVersionFileStore(fileTypeDevice, path.Join(rootDir, clusterName))
	if err != nil {
		glog.Errorln("new device versionfilestore error", err, "rootDir", rootDir, "clusterName", clusterName)
		return nil, db.ErrDBInternal
	}

	d := &deviceSvc{
		clusterName: clusterName,
		lock:        &sync.Mutex{},
		allDevices:  nil,
		store:       store,
	}

	if !d.store.HasCurrentVersion() {
		glog.Infoln("newDeviceSvc done, no device, rootDir", rootDir, "clusterName", clusterName)
		return d, nil
	}

	// load all devices from the current version file
	data, err := d.store.ReadCurrentVersion("newDeviceSvc", "req-newDeviceSvc")
	if err != nil {
		glog.Errorln("read current version error", err, "rootDir", rootDir, "clusterName", clusterName)
		return nil, db.ErrDBInternal
	}

	// deserialize
	allDevs := &pb.AllDevices{}
	err = proto.Unmarshal(data, allDevs)
	if err != nil {
		glog.Errorln("Unmarshal AllDevices error", err, "rootDir", rootDir, "clusterName", clusterName)
		return nil, db.ErrDBInternal
	}

	d.allDevices = allDevs.Devices

	glog.Infoln("newDeviceSvc done, last device", d.allDevices[len(d.allDevices)-1],
		"rootDir", rootDir, "clusterName", clusterName)

	return d, nil
}

func (d *deviceSvc) CreateDevice(ctx context.Context, dev *pb.Device) error {
	requuid := utils.GetReqIDFromContext(ctx)

	if dev.ClusterName != d.clusterName {
		glog.Errorln("createDevice wrong cluster, dev", dev, "current cluster", d.clusterName, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	// check and create device under lock protection
	d.lock.Lock()
	defer d.lock.Unlock()

	allDevs := &pb.AllDevices{}
	// check if device is already assigned to some other service
	for _, device := range d.allDevices {
		if device.DeviceName == dev.DeviceName {
			if controldb.EqualDevice(device, dev) {
				glog.Infoln("createDevice done, same device exists", dev, "requuid", requuid)
				return nil
			}
			glog.Errorln("createDevice, device", dev, "is assigned to another service",
				device.ServiceName, "requuid", requuid)
			return db.ErrDBConditionalCheckFailed
		}
		allDevs.Devices = append(allDevs.Devices, device)
	}

	if len(allDevs.Devices) != 0 {
		glog.Infoln("createDevice", dev, "is available, current last assigned device",
			allDevs.Devices[len(allDevs.Devices)-1], "requuid", requuid)
	} else {
		glog.Infoln("create the first device file", dev, "requuid", requuid)
	}

	// device is available, append to allDevs
	allDevs.Devices = append(allDevs.Devices, dev)

	data, err := proto.Marshal(allDevs)
	if err != nil {
		glog.Errorln("createDevice, Marshal all devices error", err, allDevs, "requuid", requuid)
		return db.ErrDBInternal
	}

	err = d.store.CreateNewVersionFile(data, "createDevice", requuid)
	if err != nil {
		glog.Errorln("createDevice, createNewVersionFile error", err, dev, "requuid", requuid)
		return db.ErrDBInternal
	}

	// successfully created a new version device file, update in-memory cache
	d.allDevices = allDevs.Devices

	glog.Infoln("createDevice done", dev, "requuid", requuid)
	return nil
}

func (d *deviceSvc) GetDevice(ctx context.Context, key *pb.DeviceKey) (*pb.Device, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	if key.ClusterName != d.clusterName {
		glog.Errorln("getDevice wrong cluster, dev", key, "current cluster", d.clusterName, "requuid", requuid)
		return nil, db.ErrDBInvalidRequest
	}

	d.lock.Lock()
	defer d.lock.Unlock()

	for _, device := range d.allDevices {
		if device.DeviceName == key.DeviceName {
			glog.Infoln("getDevice", device, "requuid", requuid)
			return device, nil
		}
	}

	glog.Errorln("getDevice not exist", key, "requuid", requuid)
	return nil, db.ErrDBRecordNotFound
}

func (d *deviceSvc) DeleteDevice(ctx context.Context, key *pb.DeviceKey) error {
	requuid := utils.GetReqIDFromContext(ctx)

	if key.ClusterName != d.clusterName {
		glog.Errorln("deleteDevice wrong cluster, dev", key, "current cluster", d.clusterName, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	d.lock.Lock()
	defer d.lock.Unlock()

	var dev *pb.Device
	allDevs := &pb.AllDevices{}
	for _, device := range d.allDevices {
		if device.DeviceName == key.DeviceName {
			glog.Errorln("deleteDevice", key, "find device", device, "requuid", requuid)
			dev = device
			continue
		}
		allDevs.Devices = append(allDevs.Devices, device)
	}

	if dev == nil {
		glog.Errorln("deleteDevice not exist", key, "requuid", requuid)
		return db.ErrDBRecordNotFound
	}

	// device exists, create a new version file with device deleted
	data, err := proto.Marshal(allDevs)
	if err != nil {
		glog.Errorln("deleteDevice, Marshal all devices error", err, allDevs, "requuid", requuid)
		return db.ErrDBInternal
	}

	err = d.store.CreateNewVersionFile(data, "deleteDevice", requuid)
	if err != nil {
		glog.Errorln("deleteDevice, createNewVersionFile error", err, key, "requuid", requuid)
		return db.ErrDBInternal
	}

	// successfully created a new version device file, update in-memory cache
	d.allDevices = allDevs.Devices

	glog.Infoln("deleteDevice done", dev, "requuid", requuid)
	return nil
}

func (d *deviceSvc) listDevices() []*pb.Device {
	d.lock.Lock()
	devs := make([]*pb.Device, len(d.allDevices))
	for i, device := range d.allDevices {
		devs[i] = device
	}
	d.lock.Unlock()
	return devs
}

func (d *deviceSvc) ListDevices(req *pb.ListDeviceRequest, stream pb.ControlDBService_ListDevicesServer) error {
	if req.ClusterName != d.clusterName {
		glog.Errorln("listDevices wrong cluster, current cluster", d.clusterName, "req", req)
		return db.ErrDBInvalidRequest
	}

	devs := d.listDevices()

	// send back to client outside the lock
	for _, device := range devs {
		err := stream.Send(device)
		if err != nil {
			glog.Errorln("send device error", err, device, "cluster", d.clusterName)
			return err
		}
	}

	glog.Infoln("listed", len(devs), "devices, cluster", d.clusterName)
	return nil
}
