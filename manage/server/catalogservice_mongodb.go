package manageserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/catalog/mongodb"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

func (s *ManageHTTPServer) createMongoDBService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateMongoDBRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateMongoDBRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkRequest(req.Service, req.Resource)
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

	// The service config file is created before service attribute. The service creation may
	// get interrupted at any time, for example, node goes down. The creation could be retried.
	// The service config file creation will return error as keyfileContent mismatch.
	// Get the possible existing keyfileContent first.
	keyfileContent, err := s.getMongoDBExistingKeyfile(ctx, req.Service, requuid)
	if err != nil {
		glog.Errorln("getMongoDBExistingKeyfile error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	if len(keyfileContent) == 0 {
		// create a new keyfile
		keyfileContent, err = mongodbcatalog.GenKeyfileContent()
		if err != nil {
			glog.Errorln("GenKeyfileContent error", err, "requuid", requuid, req.Service)
			return manage.ConvertToHTTPError(err)
		}
	}

	// create the service in the control plane and the container platform
	crReq := mongodbcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs,
		s.cluster, req.Service.ServiceName, keyfileContent, req.Options, req.Resource)

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
	glog.Infoln("setMongoDBInit", req.ServiceName, "requuid", requuid)

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

	// enable mongodb auth
	statusMsg := "enable mongodb auth"
	s.catalogSvcInit.UpdateTaskStatusMsg(service.ServiceUUID, statusMsg)
	err = s.enableMongoDBAuth(ctx, attr, requuid)
	if err != nil {
		glog.Errorln("enableMongoDBAuth error", err, "requuid", requuid, service)
		return manage.ConvertToHTTPError(err)
	}

	// the config file is updated, restart all containers
	glog.Infoln("auth is enabled, restart all containers to make auth effective, requuid", requuid, req)

	// update the init task status message
	statusMsg = "restarting all containers"
	s.catalogSvcInit.UpdateTaskStatusMsg(service.ServiceUUID, statusMsg)

	// restart service containers
	err = s.containersvcIns.StopService(ctx, s.cluster, req.ServiceName)
	if err != nil {
		glog.Errorln("StopService error", err, "requuid", requuid, req)
		return manage.ConvertToHTTPError(err)
	}
	err = s.containersvcIns.ScaleService(ctx, s.cluster, req.ServiceName, attr.Spec.Replicas)
	if err != nil {
		glog.Errorln("ScaleService error", err, "requuid", requuid, req)
		return manage.ConvertToHTTPError(err)
	}

	// set service initialized
	glog.Infoln("all containers restarted, set service initialized, requuid", requuid, req)

	return s.setServiceInitialized(ctx, req.ServiceName, requuid)
}

func (s *ManageHTTPServer) enableMongoDBAuth(ctx context.Context, attr *common.ServiceAttr, requuid string) error {
	// enable auth in service.conf
	for cfgIndex, cfg := range attr.Spec.ServiceConfigs {
		if catalog.IsServiceConfigFile(cfg.FileName) {
			// fetch the config file
			cfgfile, err := s.dbIns.GetConfigFile(ctx, attr.ServiceUUID, cfg.FileID)
			if err != nil {
				glog.Errorln("GetConfigFile error", err, cfg, "requuid", requuid, attr)
				return err
			}

			// auth is not enabled, enable it
			newContent := mongodbcatalog.EnableMongoDBAuth(cfgfile.Spec.Content)

			err = s.updateServiceConfigFile(ctx, attr, cfgIndex, cfgfile, newContent, requuid)
			if err != nil {
				glog.Errorln("updateServiceConfigFile error", err, cfgfile, "requuid", requuid, attr)
				return err
			}

			glog.Infoln("enabled mongodb auth, requuid", requuid, attr)
			return nil
		}
	}

	glog.Errorln("internal error - service.conf not found, requuid", requuid, attr)
	return errors.New("internal error - service.conf not found")
}

func (s *ManageHTTPServer) getMongoDBExistingKeyfile(ctx context.Context, req *manage.ServiceCommonRequest, requuid string) (string, error) {
	svc, err := s.dbIns.GetService(ctx, req.Cluster, req.ServiceName)
	if err != nil {
		if err == db.ErrDBRecordNotFound {
			glog.Infoln("mongodb service not exist", req, "requuid", requuid)
			return "", nil
		}

		glog.Errorln("get mongodb service error", err, req, "requuid", requuid)
		return "", err
	}

	// get the config file for keyfileContent
	version := int64(0)
	fileID := utils.GenConfigFileID(req.ServiceName, mongodbcatalog.KeyfileName, version)
	cfgfile, err := s.dbIns.GetConfigFile(ctx, svc.ServiceUUID, fileID)
	if err != nil {
		if err == db.ErrDBRecordNotFound {
			glog.Infoln("mongodb keyfile not exist", req, "requuid", requuid)
			return "", nil
		}

		glog.Errorln("get mongodb keyfile error", err, req, "requuid", requuid)
		return "", err
	}

	glog.Infoln("get mongodb existing keyfile, requuid", requuid, svc)
	return cfgfile.Spec.Content, nil
}
