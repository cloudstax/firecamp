package controldbserver

import (
	"path"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/db"
	pb "github.com/cloudstax/firecamp/db/controldb/protocols"
	"github.com/cloudstax/firecamp/utils"
)

// ControlDBServer implements DBService server.
//
// The server design needs to be scalable, to support like 100 services in one cluster,
// and 10,000 serviceMembers per service. Every cluster will run its own control db service.
// If one service needs to cross clusters, such as 2 aws regions, federation (could simply
// be the service init script) will coordinate it. For example, federation creates the
// services on each cluster with two-phases. Firstly create service with the initial status.
// After the services are created in all clusters, change status to active.
//
// Design options, go with #2.
// 1. use kv DB such as levelDB or RocksDB.
// looks over complex? If something goes wrong, not easy to debug. For example, some bug causes
// data corrupted, or file system crashes.
// 2. simply use file and directory.
// cluster/service data could be separated, will not impact each other. could control how data
// is organized, such as version support, atomic write via rename, etc.
// Could have the simple common solution for scalability. Limits the max children of one directory.
// If have more children, create a new directory. The example directory hierarchy:
//   /rootDir/cluster0/device.v0
//  				  cluster0/device.v1
//					  cluster0/service.v0
//					  cluster0/service.v1
//            cluster0/servicedir/service0-uuid/attr.v0
//                        	      service0-uuid/attr.v1
//      												  service0-uuid/serviceMemberdir/member0
//      												  service0-uuid/serviceMemberdir/...
//      												  service0-uuid/serviceMemberdir/member1000
//      												  service0-uuid/configdir/configfile0
//      												  service0-uuid/configdir/...
//      												  service0-uuid/configdir/configfile1000
//      												  ...
//						 				  			    service1000-uuid/
//
// For device, one device has very small data. Could simply write all devices' assignments
// into a single file. Keep like the last 10 versions would be enough.
// Similar for service, all services will be kept in a single file.
//
type ControlDBServer struct {
	rootPath    string
	clusterName string
	serviceDir  string

	deviceSvcIns        *deviceSvc
	serviceSvcIns       *serviceSvc
	serviceAttrSvcIns   *serviceAttrSvc
	serviceMemberSvcIns *serviceMemberSvc
	cfgSvcIns           *configFileSvc
}

func NewControlDBServer(rootPath string, clusterName string) (*ControlDBServer, error) {
	dev, err := newDeviceSvc(rootPath, clusterName)
	if err != nil {
		glog.Errorln("newDeviceServer error", err, "rootPath", rootPath, "cluster", clusterName)
		return nil, err
	}

	svc, err := newServiceSvc(rootPath, clusterName)
	if err != nil {
		glog.Errorln("newServiceServer error", err, "rootPath", rootPath, "cluster", clusterName)
		return nil, err
	}

	// put all services under a single dir. could expand if really required.
	serviceDir := path.Join(rootPath, clusterName, serviceDirName)
	err = utils.CreateDirIfNotExist(serviceDir)
	if err != nil {
		glog.Errorln("create service dir error", err, serviceDir)
		return nil, db.ErrDBInternal
	}

	attrsvc := newServiceAttrSvc(serviceDir)
	membersvc := newServiceMemberSvc(serviceDir)
	cfgsvc := newConfigFileSvc(serviceDir)

	s := &ControlDBServer{
		rootPath:            rootPath,
		clusterName:         clusterName,
		serviceDir:          serviceDir,
		deviceSvcIns:        dev,
		serviceSvcIns:       svc,
		serviceAttrSvcIns:   attrsvc,
		serviceMemberSvcIns: membersvc,
		cfgSvcIns:           cfgsvc,
	}
	return s, nil
}

func (s *ControlDBServer) Stop() {
	glog.Infoln("ControlDBServer stop, rootPath", s.rootPath, "cluster", s.clusterName)
	s.serviceAttrSvcIns.Stop()
	s.serviceMemberSvcIns.Stop()
	s.cfgSvcIns.Stop()
}

func (s *ControlDBServer) CreateDevice(ctx context.Context, dev *pb.Device) (*pb.CreateDeviceResponse, error) {
	return &pb.CreateDeviceResponse{}, s.deviceSvcIns.CreateDevice(ctx, dev)
}

func (s *ControlDBServer) GetDevice(ctx context.Context, key *pb.DeviceKey) (*pb.Device, error) {
	return s.deviceSvcIns.GetDevice(ctx, key)
}

func (s *ControlDBServer) DeleteDevice(ctx context.Context, key *pb.DeviceKey) (*pb.DeleteDeviceResponse, error) {
	return &pb.DeleteDeviceResponse{}, s.deviceSvcIns.DeleteDevice(ctx, key)
}

func (s *ControlDBServer) ListDevices(req *pb.ListDeviceRequest, stream pb.ControlDBService_ListDevicesServer) error {
	return s.deviceSvcIns.ListDevices(req, stream)
}

func (s *ControlDBServer) CreateService(ctx context.Context, svc *pb.Service) (*pb.CreateServiceResponse, error) {
	return &pb.CreateServiceResponse{}, s.serviceSvcIns.CreateService(ctx, svc)
}

func (s *ControlDBServer) GetService(ctx context.Context, key *pb.ServiceKey) (*pb.Service, error) {
	return s.serviceSvcIns.GetService(ctx, key)
}

func (s *ControlDBServer) DeleteService(ctx context.Context, key *pb.ServiceKey) (*pb.DeleteServiceResponse, error) {
	return &pb.DeleteServiceResponse{}, s.serviceSvcIns.DeleteService(ctx, key)
}

func (s *ControlDBServer) ListServices(req *pb.ListServiceRequest, stream pb.ControlDBService_ListServicesServer) error {
	return s.serviceSvcIns.ListServices(req, stream)
}

func (s *ControlDBServer) CreateServiceAttr(ctx context.Context, attr *pb.ServiceAttr) (*pb.CreateServiceAttrResponse, error) {
	return &pb.CreateServiceAttrResponse{}, s.serviceAttrSvcIns.CreateServiceAttr(ctx, attr)
}

func (s *ControlDBServer) GetServiceAttr(ctx context.Context, key *pb.ServiceAttrKey) (*pb.ServiceAttr, error) {
	return s.serviceAttrSvcIns.GetServiceAttr(ctx, key)
}

func (s *ControlDBServer) DeleteServiceAttr(ctx context.Context, key *pb.ServiceAttrKey) (*pb.DeleteServiceAttrResponse, error) {
	return &pb.DeleteServiceAttrResponse{}, s.serviceAttrSvcIns.DeleteServiceAttr(ctx, key)
}

func (s *ControlDBServer) UpdateServiceAttr(ctx context.Context, req *pb.UpdateServiceAttrRequest) (*pb.UpdateServiceAttrResponse, error) {
	return &pb.UpdateServiceAttrResponse{}, s.serviceAttrSvcIns.UpdateServiceAttr(ctx, req)
}

func (s *ControlDBServer) CreateServiceMember(ctx context.Context, member *pb.ServiceMember) (*pb.CreateServiceMemberResponse, error) {
	return &pb.CreateServiceMemberResponse{}, s.serviceMemberSvcIns.CreateServiceMember(ctx, member)
}

func (s *ControlDBServer) GetServiceMember(ctx context.Context, key *pb.ServiceMemberKey) (*pb.ServiceMember, error) {
	return s.serviceMemberSvcIns.GetServiceMember(ctx, key)
}

func (s *ControlDBServer) DeleteServiceMember(ctx context.Context, key *pb.ServiceMemberKey) (*pb.DeleteServiceMemberResponse, error) {
	return &pb.DeleteServiceMemberResponse{}, s.serviceMemberSvcIns.DeleteServiceMember(ctx, key)
}

func (s *ControlDBServer) UpdateServiceMember(ctx context.Context, req *pb.UpdateServiceMemberRequest) (*pb.UpdateServiceMemberResponse, error) {
	return &pb.UpdateServiceMemberResponse{}, s.serviceMemberSvcIns.UpdateServiceMember(ctx, req)
}

func (s *ControlDBServer) ListServiceMembers(req *pb.ListServiceMemberRequest, stream pb.ControlDBService_ListServiceMembersServer) error {
	return s.serviceMemberSvcIns.ListServiceMembers(req, stream)
}

func (s *ControlDBServer) CreateConfigFile(ctx context.Context, cfg *pb.ConfigFile) (*pb.CreateConfigFileResponse, error) {
	return &pb.CreateConfigFileResponse{}, s.cfgSvcIns.CreateConfigFile(ctx, cfg)
}

func (s *ControlDBServer) GetConfigFile(ctx context.Context, key *pb.ConfigFileKey) (*pb.ConfigFile, error) {
	return s.cfgSvcIns.GetConfigFile(ctx, key)
}

func (s *ControlDBServer) DeleteConfigFile(ctx context.Context, key *pb.ConfigFileKey) (*pb.DeleteConfigFileResponse, error) {
	return &pb.DeleteConfigFileResponse{}, s.cfgSvcIns.DeleteConfigFile(ctx, key)
}
