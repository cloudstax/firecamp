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

type serviceSvc struct {
	clusterName string

	// the lock to serialize the service operations
	lock *sync.Mutex
	// the in-memory copy of the last service
	allServices []*pb.Service

	store *versionfilestore
}

func newServiceSvc(rootDir string, clusterName string) (*serviceSvc, error) {
	store, err := newVersionFileStore(fileTypeService, path.Join(rootDir, clusterName))
	if err != nil {
		glog.Errorln("new service versionfilestore error", err, "rootDir", rootDir, "clusterName", clusterName)
		return nil, db.ErrDBInternal
	}

	s := &serviceSvc{
		clusterName: clusterName,
		lock:        &sync.Mutex{},
		allServices: nil,
		store:       store,
	}

	if !s.store.HasCurrentVersion() {
		glog.Infoln("newServiceSvc done, no service, rootDir", rootDir, "clusterName", clusterName)
		return s, nil
	}

	// load all services from the current version file
	data, err := s.store.ReadCurrentVersion("newServiceSvc", "req-newServiceSvc")
	if err != nil {
		glog.Errorln("read current version error", err, "rootDir", rootDir, "clusterName", clusterName)
		return nil, db.ErrDBInternal
	}

	// deserialize
	allSvcs := &pb.AllServices{}
	err = proto.Unmarshal(data, allSvcs)
	if err != nil {
		glog.Errorln("Unmarshal AllServices error", err, "rootDir", rootDir, "clusterName", clusterName)
		return nil, db.ErrDBInternal
	}

	s.allServices = allSvcs.Services

	glog.Infoln("NewServiceSvc done, last service", s.allServices[len(s.allServices)-1],
		"rootDir", rootDir, "clusterName", clusterName)
	return s, nil
}

func (s *serviceSvc) CreateService(ctx context.Context, svc *pb.Service) error {
	requuid := utils.GetReqIDFromContext(ctx)

	if svc.ClusterName != s.clusterName {
		glog.Errorln("createService wrong cluster, svc", svc, "current cluster", s.clusterName, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	// check and create service under lock protection
	s.lock.Lock()
	defer s.lock.Unlock()

	allSvcs := &pb.AllServices{}
	// check if service is already assigned to some other service
	for _, service := range s.allServices {
		if service.ServiceName == svc.ServiceName {
			if controldb.EqualService(service, svc) {
				glog.Infoln("createService done, same service exists", service, "requuid", requuid)
				return nil
			}
			glog.Errorln("createService, existing service has a different uuid", service,
				"creating", svc, "requuid", requuid)
			return db.ErrDBConditionalCheckFailed
		}
		allSvcs.Services = append(allSvcs.Services, service)
	}

	if len(allSvcs.Services) != 0 {
		glog.Infoln("createService", svc, "is available, current last assigned service",
			allSvcs.Services[len(allSvcs.Services)-1], "requuid", requuid)
	} else {
		glog.Infoln("create the first service file", svc, "requuid", requuid)
	}

	// service is available, append to allSvcs
	allSvcs.Services = append(allSvcs.Services, svc)

	// create a new version file
	data, err := proto.Marshal(allSvcs)
	if err != nil {
		glog.Errorln("createService, Marshal all services error", err, allSvcs, "requuid", requuid)
		return db.ErrDBInternal
	}

	err = s.store.CreateNewVersionFile(data, "createService", requuid)
	if err != nil {
		glog.Errorln("createService, createNewVersionFile error", err, svc, "requuid", requuid)
		return db.ErrDBInternal
	}

	// successfully created a new version service file, update in-memory cache
	s.allServices = allSvcs.Services

	glog.Infoln("createService done", svc, "requuid", requuid)
	return nil
}

func (s *serviceSvc) GetService(ctx context.Context, key *pb.ServiceKey) (*pb.Service, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	if key.ClusterName != s.clusterName {
		glog.Errorln("getService wrong cluster, svc", key, "current cluster", s.clusterName, "requuid", requuid)
		return nil, db.ErrDBInvalidRequest
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	for _, service := range s.allServices {
		if service.ServiceName == key.ServiceName {
			glog.Infoln("getService", service, "requuid", requuid)
			return service, nil
		}
	}

	glog.Errorln("getService not exist", key, "requuid", requuid)
	return nil, db.ErrDBRecordNotFound
}

func (s *serviceSvc) DeleteService(ctx context.Context, key *pb.ServiceKey) error {
	requuid := utils.GetReqIDFromContext(ctx)

	if key.ClusterName != s.clusterName {
		glog.Errorln("deleteService wrong cluster, svc", key, "current cluster", s.clusterName, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	var svc *pb.Service
	allSvcs := &pb.AllServices{}
	for _, service := range s.allServices {
		if service.ServiceName == key.ServiceName {
			glog.Errorln("deleteService", key, "find service", service, "requuid", requuid)
			svc = service
			continue
		}
		allSvcs.Services = append(allSvcs.Services, service)
	}

	if svc == nil {
		glog.Errorln("deleteService not exist", key, "requuid", requuid)
		return db.ErrDBRecordNotFound
	}

	// create a new version file
	data, err := proto.Marshal(allSvcs)
	if err != nil {
		glog.Errorln("deleteService, Marshal all services error", err, allSvcs, "requuid", requuid)
		return db.ErrDBInternal
	}

	err = s.store.CreateNewVersionFile(data, "createService", requuid)
	if err != nil {
		glog.Errorln("deleteService, createNewVersionFile error", err, key, "requuid", requuid)
		return db.ErrDBInternal
	}

	// successfully created a new version service file, update in-memory cache
	s.allServices = allSvcs.Services

	glog.Infoln("deleteService done", svc, "requuid", requuid)
	return nil
}

func (s *serviceSvc) listServices() []*pb.Service {
	s.lock.Lock()
	svcs := make([]*pb.Service, len(s.allServices))
	for i, service := range s.allServices {
		svcs[i] = service
	}
	s.lock.Unlock()
	return svcs
}

func (s *serviceSvc) ListServices(req *pb.ListServiceRequest, stream pb.ControlDBService_ListServicesServer) error {
	if req.ClusterName != s.clusterName {
		glog.Errorln("listServices wrong cluster, current cluster", s.clusterName, "req", req)
		return db.ErrDBInvalidRequest
	}

	svcs := s.listServices()
	for _, service := range svcs {
		err := stream.Send(service)
		if err != nil {
			glog.Errorln("send service error", err, service, "cluster", s.clusterName)
			return err
		}
	}

	glog.Infoln("listed", len(svcs), "services, cluster", req.ClusterName)
	return nil
}
