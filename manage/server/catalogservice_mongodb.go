package manageserver

import (
	"encoding/json"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/catalog/mongodb"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/manage"
)

func (s *ManageHTTPServer) createMongoDBService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateMongoDBRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateMongoDBRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("CatalogCreateMongoDBRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	err = mongodbcatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("invalid request", err, "requuid", requuid, req.Service, req.Options)
		return err.Error(), http.StatusBadRequest
	}

	// The keyfile auth is enabled for ReplicaSet. mongodbcatalog GenDefaultCreateServiceRequest
	// generates the keyfile and sets it for each member. ManageService.CreateService is not aware
	// of the keyfile and simply creates the records for each member. If the manage server node
	// happens to crash before the service is created. The ServiceAttr and some members may be created,
	// while some members not. When the creation is retried, mongodbcatalog will generate a new keyfile.
	// The members of one ReplicaSet will then have different keyfiles. The service will not work.
	// Check if service already exists. If so, reuse the existing keyfile.
	existingKeyFileContent, err := s.getMongoDBExistingKeyFile(ctx, req.Service, requuid)
	if err != nil {
		glog.Errorln("getMongoDBExistingKeyFile error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	// create the service in the control plane and the container platform
	crReq, keyfileContent, err := mongodbcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Options, req.Resource, existingKeyFileContent)
	if err != nil {
		glog.Errorln("mongodbcatalog GenDefaultCreateServiceRequest error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("MongoDBService is created, add the init task, requuid", requuid, req.Service, req.Options.Admin)

	// run the init task in the background
	s.addMongoDBInitTask(ctx, crReq.Service, serviceUUID, req.Options, requuid)

	// send back the keyfile content
	resp := &manage.CatalogCreateMongoDBResponse{KeyFileContent: keyfileContent}
	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal CatalogCreateMongoDBResponse error", err, "requuid", requuid, req.Service, req.Options)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) getMongoDBExistingKeyFile(ctx context.Context, req *manage.ServiceCommonRequest, requuid string) (string, error) {
	svc, err := s.dbIns.GetService(ctx, req.Cluster, req.ServiceName)
	if err != nil {
		if err == db.ErrDBRecordNotFound {
			glog.Infoln("mongodb service not exist", req, "requuid", requuid)
			return "", nil
		}
		glog.Errorln("get mongodb service error", err, req, "requuid", requuid)
		return "", err
	}

	// service exists, get the service attributes
	attr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		if err == db.ErrDBRecordNotFound {
			glog.Infoln("mongodb service record exists, but service attr record not exists", req, "requuid", requuid)
			return "", nil
		}
		glog.Errorln("get mongodb service attr error", err, req, "requuid", requuid)
		return "", err
	}

	glog.Infoln("mongodb service attr exist", attr, "requuid", requuid)

	if attr.UserAttr.ServiceType != common.CatalogService_MongoDB {
		glog.Errorln("the existing service attr is not for mongodb", attr.UserAttr.ServiceType, attr, "requuid", requuid)
		return "", common.ErrServiceExist
	}

	userAttr := &common.MongoDBUserAttr{}
	err = json.Unmarshal(attr.UserAttr.AttrBytes, userAttr)
	if err != nil {
		glog.Errorln("Unmarshal mongodb user attr error", err, "requuid", requuid, attr)
		return "", err
	}

	return userAttr.KeyFileContent, nil
}

func (s *ManageHTTPServer) addMongoDBInitTask(ctx context.Context, req *manage.ServiceCommonRequest,
	serviceUUID string, opts *manage.CatalogMongoDBOptions, requuid string) {
	logCfg := s.logIns.CreateStreamLogConfig(ctx, s.cluster, req.ServiceName, serviceUUID, common.TaskTypeInit)
	taskOpts := mongodbcatalog.GenDefaultInitTaskRequest(req, logCfg, serviceUUID, s.manageurl, opts)

	task := &serviceTask{
		serviceUUID: serviceUUID,
		serviceName: req.ServiceName,
		serviceType: common.CatalogService_MongoDB,
		opts:        taskOpts,
	}

	s.catalogSvcInit.addInitTask(ctx, task)

	glog.Infoln("add init task for service", serviceUUID, "requuid", requuid, req)
}

func (s *ManageHTTPServer) setMongoDBInit(ctx context.Context, req *manage.CatalogSetServiceInitRequest, requuid string) (errmsg string, errcode int) {
	// get service uuid
	service, err := s.dbIns.GetService(ctx, s.cluster, req.ServiceName)
	if err != nil {
		glog.Errorln("GetService", req.ServiceName, req.ServiceType, "error", err, "requuid", requuid)
		return manage.ConvertToHTTPError(err)
	}

	// get service attr
	attr, err := s.dbIns.GetServiceAttr(ctx, service.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, service)
		return manage.ConvertToHTTPError(err)
	}

	// list all serviceMembers
	members, err := s.dbIns.ListServiceMembers(ctx, service.ServiceUUID)
	if err != nil {
		glog.Errorln("ListServiceMembers failed", err, "serviceUUID", service.ServiceUUID, "requuid", requuid)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("get service", service, "has", len(members), "replicas, requuid", requuid)

	// update the init task status message
	statusMsg := "enable auth for MongoDB"
	s.catalogSvcInit.UpdateTaskStatusMsg(service.ServiceUUID, statusMsg)

	// update the replica (serviceMember) mongod.conf file
	for _, member := range members {
		for i, cfg := range member.Configs {
			if !mongodbcatalog.IsMongoDBConfFile(cfg.FileName) {
				glog.V(5).Infoln("not mongod.conf file, skip the config, requuid", requuid, cfg)
				continue
			}

			glog.Infoln("enable auth on mongod.conf, requuid", requuid, cfg)

			err = s.enableMongoDBAuth(ctx, cfg, i, member, requuid)
			if err != nil {
				glog.Errorln("enableMongoDBAuth error", err, "requuid", requuid, cfg, member)
				return manage.ConvertToHTTPError(err)
			}

			glog.Infoln("enabled auth for replia, requuid", requuid, member)
			break
		}
	}

	// the config files of all replicas are updated, restart all containers
	glog.Infoln("all replicas are updated, restart all containers, requuid", requuid, req)

	// update the init task status message
	statusMsg = "restarting all MongoDB containers"
	s.catalogSvcInit.UpdateTaskStatusMsg(service.ServiceUUID, statusMsg)

	// restart service containers
	err = s.containersvcIns.StopService(ctx, s.cluster, req.ServiceName)
	if err != nil {
		glog.Errorln("StopService error", err, "requuid", requuid, req)
		return manage.ConvertToHTTPError(err)
	}
	err = s.containersvcIns.ScaleService(ctx, s.cluster, req.ServiceName, attr.Replicas)
	if err != nil {
		glog.Errorln("ScaleService error", err, "requuid", requuid, req)
		return manage.ConvertToHTTPError(err)
	}

	// set service initialized
	glog.Infoln("all containers restarted, set service initialized, requuid", requuid, req)

	return s.setServiceInitialized(ctx, req.ServiceName, requuid)
}

func (s *ManageHTTPServer) enableMongoDBAuth(ctx context.Context,
	cfg *common.MemberConfig, cfgIndex int, member *common.ServiceMember, requuid string) error {
	// fetch the config file
	cfgfile, err := s.dbIns.GetConfigFile(ctx, member.ServiceUUID, cfg.FileID)
	if err != nil {
		glog.Errorln("GetConfigFile error", err, "requuid", requuid, cfg, member)
		return err
	}

	// if auth is enabled, return
	if mongodbcatalog.IsAuthEnabled(cfgfile.Content) {
		glog.Infoln("auth is already enabled in the config file", db.PrintConfigFile(cfgfile), "requuid", requuid, member)
		return nil
	}

	// auth is not enabled, enable it
	newContent := mongodbcatalog.EnableMongoDBAuth(cfgfile.Content)

	return s.updateMemberConfig(ctx, member, cfgfile, cfgIndex, newContent, requuid)
}
