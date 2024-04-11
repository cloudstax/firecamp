package catalogsvc

import (
	"encoding/json"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/jazzl0ver/firecamp/api/catalog"
	"github.com/jazzl0ver/firecamp/api/manage"
	"github.com/jazzl0ver/firecamp/api/manage/error"
	"github.com/jazzl0ver/firecamp/catalog/mongodb"
)

func (s *CatalogHTTPServer) createMongoDBService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogCreateMongoDBRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateMongoDBRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateMongoDBRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err
	}

	err = mongodbcatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("invalid request", err, "requuid", requuid, req.Service, req.Options)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	// create a new keyfile
	keyfileContent, err := mongodbcatalog.GenKeyfileContent()
	if err != nil {
		glog.Errorln("GenKeyfileContent error", err, "requuid", requuid, req.Service)
		return clienterr.New(http.StatusInternalServerError, err.Error())
	}

	// create the service in the control plane and the container platform
	crReq := mongodbcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs,
		s.cluster, req.Service.ServiceName, keyfileContent, req.Options, req.Resource)

	err = s.managecli.CreateService(ctx, crReq)
	if err != nil {
		glog.Errorln("CreateService error", err, "requuid", requuid, req.Service)
		return err
	}

	glog.Infoln("MongoDBService is created, add the init task, requuid", requuid, req.Service, req.Options.Admin)

	// run the init task in the background
	s.addMongoDBInitTask(ctx, crReq.Service, req.Options, requuid)

	// send back the keyfile content
	resp := &catalog.CatalogCreateMongoDBResponse{KeyFileContent: keyfileContent}
	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal CatalogCreateMongoDBResponse error", err, "requuid", requuid, req.Service, req.Options)
		return clienterr.New(http.StatusInternalServerError, err.Error())
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return nil
}

func (s *CatalogHTTPServer) addMongoDBInitTask(ctx context.Context, req *manage.ServiceCommonRequest,
	opts *catalog.CatalogMongoDBOptions, requuid string) {
	taskReq := mongodbcatalog.GenDefaultInitTaskRequest(req, s.serverurl, opts)

	s.catalogSvcInit.addInitTask(ctx, taskReq)

	glog.Infoln("add init task for service", req, "requuid", requuid)
}

func (s *CatalogHTTPServer) setMongoDBInit(ctx context.Context, req *manage.ServiceCommonRequest, requuid string) error {
	glog.Infoln("setMongoDBInit", req.ServiceName, "requuid", requuid)

	// enable mongodb auth
	statusMsg := "enable mongodb auth"
	s.catalogSvcInit.UpdateTaskStatusMsg(req.ServiceName, statusMsg)
	err := s.enableMongoDBAuth(ctx, req, requuid)
	if err != nil {
		glog.Errorln("enableMongoDBAuth error", err, "requuid", requuid, req)
		return err
	}

	// the config file is updated, restart all containers
	glog.Infoln("auth is enabled, restart all containers to make auth effective, requuid", requuid, req)

	// update the init task status message
	statusMsg = "restarting all containers"
	s.catalogSvcInit.UpdateTaskStatusMsg(req.ServiceName, statusMsg)

	// restart service containers
	err = s.managecli.StopService(ctx, req)
	if err != nil {
		glog.Errorln("StopService error", err, "requuid", requuid, req)
		return err
	}

	err = s.managecli.StartService(ctx, req)
	if err != nil {
		glog.Errorln("StartService error", err, "requuid", requuid, req)
		return err
	}

	// set service initialized
	glog.Infoln("all containers restarted, set service initialized, requuid", requuid, req)

	return s.managecli.SetServiceInitialized(ctx, req)
}

func (s *CatalogHTTPServer) enableMongoDBAuth(ctx context.Context, req *manage.ServiceCommonRequest, requuid string) error {
	// enable auth in service.conf
	// fetch the config file
	getReq := &manage.GetServiceConfigFileRequest{
		Service:        req,
		ConfigFileName: catalog.SERVICE_FILE_NAME,
	}
	cfgfile, err := s.managecli.GetServiceConfigFile(ctx, getReq)
	if err != nil {
		glog.Errorln("GetServiceConfigFile error", err, "requuid", requuid, req)
		return err
	}

	// enable auth
	newContent := mongodbcatalog.EnableMongoDBAuth(cfgfile.Spec.Content)

	updateReq := &manage.UpdateServiceConfigRequest{
		Service:           req,
		ConfigFileName:    cfgfile.Meta.FileName,
		ConfigFileContent: newContent,
	}
	err = s.managecli.UpdateServiceConfig(ctx, updateReq)
	if err != nil {
		glog.Errorln("updateServiceConfig error", err, "requuid", requuid, req)
		return err
	}

	glog.Infoln("enabled mongodb auth, requuid", requuid, req)
	return nil
}
