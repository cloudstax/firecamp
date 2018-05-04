package catalogsvc

import (
	"encoding/json"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/catalog"
	"github.com/cloudstax/firecamp/api/manage"
	"github.com/cloudstax/firecamp/catalog/mongodb"
	"github.com/cloudstax/firecamp/common"
)

func (s *CatalogHTTPServer) createMongoDBService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &catalog.CatalogCreateMongoDBRequest{}
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
		return err.Error(), http.StatusInternalServerError
	}

	if len(keyfileContent) == 0 {
		// create a new keyfile
		keyfileContent, err = mongodbcatalog.GenKeyfileContent()
		if err != nil {
			glog.Errorln("GenKeyfileContent error", err, "requuid", requuid, req.Service)
			return err.Error(), http.StatusInternalServerError
		}
	}

	// create the service in the control plane and the container platform
	crReq := mongodbcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs,
		s.cluster, req.Service.ServiceName, keyfileContent, req.Options, req.Resource)

	serviceUUID, err := s.managecli.CreateService(ctx, crReq)
	if err != nil {
		glog.Errorln("CreateService error", err, "requuid", requuid, req.Service)
		return err.Error(), http.StatusInternalServerError
	}

	glog.Infoln("MongoDBService is created, add the init task, requuid", requuid, req.Service, req.Options.Admin)

	// run the init task in the background
	s.addMongoDBInitTask(ctx, crReq.Service, serviceUUID, req.Options, requuid)

	// send back the keyfile content
	resp := &catalog.CatalogCreateMongoDBResponse{KeyFileContent: keyfileContent}
	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal CatalogCreateMongoDBResponse error", err, "requuid", requuid, req.Service, req.Options)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}

func (s *CatalogHTTPServer) addMongoDBInitTask(ctx context.Context, req *manage.ServiceCommonRequest,
	serviceUUID string, opts *catalog.CatalogMongoDBOptions, requuid string) {
	logCfg := s.logIns.CreateStreamLogConfig(ctx, s.cluster, req.ServiceName, serviceUUID, common.TaskTypeInit)
	taskOpts := mongodbcatalog.GenDefaultInitTaskRequest(req, logCfg, serviceUUID, s.serverurl, opts)

	task := &serviceTask{
		serviceName: req.ServiceName,
		opts:        taskOpts,
	}

	s.catalogSvcInit.addInitTask(ctx, task)

	glog.Infoln("add init task for service", serviceUUID, "requuid", requuid, req)
}

func (s *CatalogHTTPServer) setMongoDBInit(ctx context.Context, req *catalog.CatalogSetServiceInitRequest, requuid string) (errmsg string, errcode int) {
	glog.Infoln("setMongoDBInit", req.ServiceName, "requuid", requuid)

	commonReq := &manage.ServiceCommonRequest{
		Region:      req.Region,
		Cluster:     req.Cluster,
		ServiceName: req.ServiceName,
	}

	// enable mongodb auth
	statusMsg := "enable mongodb auth"
	s.catalogSvcInit.UpdateTaskStatusMsg(req.ServiceName, statusMsg)
	err := s.enableMongoDBAuth(ctx, commonReq, requuid)
	if err != nil {
		glog.Errorln("enableMongoDBAuth error", err, "requuid", requuid, req)
		return err.Error(), http.StatusInternalServerError
	}

	// the config file is updated, restart all containers
	glog.Infoln("auth is enabled, restart all containers to make auth effective, requuid", requuid, req)

	// update the init task status message
	statusMsg = "restarting all containers"
	s.catalogSvcInit.UpdateTaskStatusMsg(req.ServiceName, statusMsg)

	// restart service containers
	err = s.managecli.StopService(ctx, commonReq)
	if err != nil {
		glog.Errorln("StopService error", err, "requuid", requuid, req)
		return err.Error(), http.StatusInternalServerError
	}

	err = s.managecli.StartService(ctx, commonReq)
	if err != nil {
		glog.Errorln("StartService error", err, "requuid", requuid, req)
		return err.Error(), http.StatusInternalServerError
	}

	// set service initialized
	glog.Infoln("all containers restarted, set service initialized, requuid", requuid, req)

	return s.managecli.SetServiceInitialized(ctx, commonReq)
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
		Service: &manage.ServiceCommonRequest{
			Region:      s.region,
			Cluster:     s.cluster,
			ServiceName: req.ServiceName,
		},
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

func (s *CatalogHTTPServer) getMongoDBExistingKeyfile(ctx context.Context, req *manage.ServiceCommonRequest, requuid string) (string, error) {
	getReq := &manage.GetServiceConfigFileRequest{
		Service:        req,
		ConfigFileName: mongodbcatalog.KeyfileName,
	}
	cfgfile, err := s.managecli.GetServiceConfigFile(ctx, getReq)
	if err != nil {
		glog.Errorln("GetConfigFile error", err, "requuid", requuid, getReq)
		return "", err
	}

	glog.Infoln("get mongodb existing keyfile, requuid", requuid, req)
	return cfgfile.Spec.Content, nil
}
