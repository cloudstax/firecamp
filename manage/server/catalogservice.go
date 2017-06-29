package manageserver

import (
	"encoding/json"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/catalog"
	"github.com/cloudstax/openmanage/catalog/mongodb"
	"github.com/cloudstax/openmanage/catalog/postgres"
	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/db"
	"github.com/cloudstax/openmanage/manage"
)

func (s *ManageHTTPServer) putCatalogServiceOp(ctx context.Context, w http.ResponseWriter,
	r *http.Request, trimURL string, requuid string) (errmsg string, errcode int) {
	switch trimURL {
	case manage.CatalogCreateMongoDBOp:
		return s.createMongoDBService(ctx, r, requuid)
	case manage.CatalogCreatePostgreSQLOp:
		return s.createPGService(ctx, r, requuid)
	case manage.CatalogSetServiceInitOp:
		return s.catalogSetServiceInit(ctx, r, requuid)
	default:
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}
}

func (s *ManageHTTPServer) getCatalogServiceOp(ctx context.Context,
	w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCheckServiceInitRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCheckServiceInitRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Service.Cluster != s.cluster || req.Service.Region != s.region {
		glog.Errorln("CatalogCheckServiceInitRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	// get service uuid
	service, err := s.dbIns.GetService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("GetService", req.Service.ServiceName, req.ServiceType, "error", err, "requuid", requuid)
		return manage.ConvertToHTTPError(err)
	}

	// check if the init task is running
	initialized := false
	if s.catalogSvcInit.hasInitTask(ctx, service.ServiceUUID) {
		glog.Infoln("The service", req.Service.ServiceName, req.ServiceType,
			"is under initialization, requuid", requuid)
	} else {
		// no init task is running, check if the service is initialized
		glog.Infoln("No init task for service", req.Service.ServiceName, req.ServiceType, "requuid", requuid)

		attr, err := s.dbIns.GetServiceAttr(ctx, service.ServiceUUID)
		if err != nil {
			glog.Errorln("GetServiceAttr error", err, service, "requuid", requuid)
			return manage.ConvertToHTTPError(err)
		}

		glog.Infoln("service attribute", attr, "requuid", requuid)

		switch attr.ServiceStatus {
		case common.ServiceStatusActive:
			initialized = true

		case common.ServiceStatusInitializing:
			// service is not initialized, and no init task is running.
			// This is possible. For example, the manage service node crashes and all in-memory
			// init tasks will be lost. Currently rely on the customer to query service status
			// to trigger the init task again.
			// TODO scan the pending init catalog service at start.

			// trigger the init task.
			switch req.ServiceType {
			case catalog.CatalogService_MongoDB:
				s.addMongoDBInitTask(ctx, req.Service, attr.ServiceUUID, attr.Replicas, req.Admin, req.AdminPasswd, requuid)

			case catalog.CatalogService_PostgreSQL:
				// PG does not require additional init work. set PG initialized
				errmsg, errcode := s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
				if errcode == http.StatusOK {
					initialized = true
				} else {
					return errmsg, errcode
				}

			default:
				return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
			}

		default:
			glog.Errorln("service is not at active or creating status", attr, "requuid", requuid)
			return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
		}
	}

	resp := &manage.CatalogCheckServiceInitResponse{
		Initialized: initialized,
	}

	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal error", err, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) createMongoDBService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateMongoDBRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateMongoDBRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Service.Cluster != s.cluster || req.Service.Region != s.region {
		glog.Errorln("CatalogCreateMongoDBRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	// create the service in the control plane and the container platform
	crReq, err := mongodbcatalog.GenDefaultCreateServiceRequest(s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Replicas, req.VolumeSizeGB, req.Resource)
	if err != nil {
		glog.Errorln("mongodbcatalog GenDefaultCreateServiceRequest error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("MongoDBService is created, add the init task, requuid", requuid, req.Service, req.Admin)

	// run the init task in the background
	s.addMongoDBInitTask(ctx, crReq.Service, serviceUUID, req.Replicas, req.Admin, req.AdminPasswd, requuid)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) addMongoDBInitTask(ctx context.Context, req *manage.ServiceCommonRequest,
	serviceUUID string, replicas int64, admin string, adminPasswd string, requuid string) {
	taskOpts := mongodbcatalog.GenDefaultInitTaskRequest(req, serviceUUID, replicas, s.manageurl, admin, adminPasswd)

	task := &serviceTask{
		serviceUUID: serviceUUID,
		serviceName: req.ServiceName,
		serviceType: catalog.CatalogService_MongoDB,
		opts:        taskOpts,
	}

	s.catalogSvcInit.addInitTask(ctx, task)

	glog.Infoln("add init task for service", serviceUUID, "requuid", requuid, req)
}

func (s *ManageHTTPServer) createPGService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreatePostgreSQLRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreatePostgreSQLRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Service.Cluster != s.cluster || req.Service.Region != s.region {
		glog.Errorln("CatalogCreatePostgreSQLRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	// create the service in the control plane and the container platform
	crReq := pgcatalog.GenDefaultCreateServiceRequest(s.region, s.azs, s.cluster, req.Service.ServiceName,
		req.Replicas, req.VolumeSizeGB, req.AdminPasswd, req.DBReplUser, req.ReplUserPasswd, req.Resource)
	_, err = s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	// PG does not require additional init work. set PG initialized
	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
}

func (s *ManageHTTPServer) catalogSetServiceInit(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogSetServiceInitRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogSetServiceInitRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Cluster != s.cluster || req.Region != s.region {
		glog.Errorln("CatalogSetServiceInitRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	switch req.ServiceType {
	case catalog.CatalogService_MongoDB:
		return s.setMongoDBInit(ctx, req, requuid)
	// no special handling for PostgreSQL
	default:
		glog.Errorln("unknown service type", req)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}
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

	// list all volumes
	vols, err := s.dbIns.ListVolumes(ctx, service.ServiceUUID)
	if err != nil {
		glog.Errorln("ListVolumes failed", err, "serviceUUID", service.ServiceUUID, "requuid", requuid)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("get service", service, "has", len(vols), "replicas, requuid", requuid)

	// update the replica (volume) mongod.conf file
	for _, vol := range vols {
		for i, cfg := range vol.Configs {
			if !mongodbcatalog.IsMongoDBConfFile(cfg.FileName) {
				glog.V(5).Infoln("not mongod.conf file, skip the config, requuid", requuid, cfg)
				continue
			}

			glog.Infoln("enable auth on mongod.conf, requuid", requuid, cfg)

			err = s.enableMongoDBAuth(ctx, cfg, i, vol, requuid)
			if err != nil {
				glog.Errorln("enableMongoDBAuth error", err, "requuid", requuid, cfg, vol)
				return manage.ConvertToHTTPError(err)
			}

			glog.Infoln("enabled auth for replia, requuid", requuid, vol)
			break
		}
	}

	// the config files of all replicas are updated, restart all containers
	glog.Infoln("all replicas are updated, restart all containers, requuid", requuid, req)

	err = s.containersvcIns.RestartService(ctx, s.cluster, req.ServiceName, attr.Replicas)
	if err != nil {
		glog.Errorln("RestartService error", err, "requuid", requuid, req)
		return manage.ConvertToHTTPError(err)
	}

	// set service initialized
	glog.Infoln("all containers restarted, set service initialized, requuid", requuid, req)

	return s.setServiceInitialized(ctx, req.ServiceName, requuid)
}

func (s *ManageHTTPServer) enableMongoDBAuth(ctx context.Context,
	cfg *common.MemberConfig, cfgIndex int, vol *common.Volume, requuid string) error {
	// fetch the config file
	cfgfile, err := s.dbIns.GetConfigFile(ctx, vol.ServiceUUID, cfg.FileID)
	if err != nil {
		glog.Errorln("GetConfigFile error", err, "requuid", requuid, cfg, vol)
		return err
	}

	// check if auth is enabled
	if !mongodbcatalog.IsAuthEnabled(cfgfile.Content) {
		newContent := mongodbcatalog.EnableMongoDBAuth(cfgfile.Content)
		newcfgfile := db.UpdateConfigFile(cfgfile, newContent)

		err = s.dbIns.UpdateConfigFile(ctx, cfgfile, newcfgfile)
		if err != nil {
			glog.Errorln("UpdateConfigFile error", err, "requuid", requuid, db.PrintConfigFile(cfgfile), vol)
			return err
		}

		// successfully update the replica config file
		cfgfile = newcfgfile
		glog.Infoln("updated config file, requuid", requuid, db.PrintConfigFile(cfgfile), vol)
	} else {
		glog.Infoln("auth is already enabled in the config file", db.PrintConfigFile(cfgfile), "requuid", requuid, vol)
	}

	// check if the config file MD5 in the volume is updated
	if cfg.FileMD5 != cfgfile.FileMD5 {
		glog.Infoln("update member config in the volume, requuid", requuid, cfg)

		newConfigs := db.CopyMemberConfigs(vol.Configs)
		newConfigs[cfgIndex].FileMD5 = cfgfile.FileMD5

		newVol := db.UpdateVolumeConfigs(vol, newConfigs)
		err = s.dbIns.UpdateVolume(ctx, vol, newVol)
		if err != nil {
			glog.Errorln("UpdateVolume error", err, "requuid", requuid, vol)
			return err
		}

		glog.Infoln("updated member configs in the volume, requuid", requuid, newVol)
	} else {
		glog.Infoln("the config file MD5 in the volume is already updated, requuid", requuid, cfg, vol)
	}

	return nil
}
