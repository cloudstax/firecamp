package catalogsvc

import (
	"encoding/json"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/catalog"
	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/api/manage"
	"github.com/cloudstax/firecamp/api/manage/error"
	"github.com/cloudstax/firecamp/catalog/redis"
)

func (s *CatalogHTTPServer) createRedisService(ctx context.Context, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogCreateRedisRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateRedisRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateRedisRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err
	}

	glog.Infoln("create redis service", req.Service, req.Options, req.Resource)

	err = rediscatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("CatalogCreateRedisRequest parameters are not valid, requuid", requuid, req.Service, req.Options)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	// create the service in the control plane and the container platform
	crReq := rediscatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Resource, req.Options)

	// create the service in the control plane
	err = s.managecli.CreateManageService(ctx, crReq)
	if err != nil {
		glog.Errorln("create service error", err, "requuid", requuid, req.Service)
		return err
	}

	glog.Infoln("created Redis service in the control plane", req.Service, "requuid", requuid)

	// update static ip in member config file
	err = s.updateRedisConfigs(ctx, req.Service, requuid)
	if err != nil {
		glog.Errorln("updateRedisConfigs error", err, "requuid", requuid, req.Service)
		return err
	}

	err = s.managecli.CreateContainerService(ctx, crReq)
	if err != nil {
		glog.Errorln("createContainerService error", err, "requuid", requuid, req.Service)
		return err
	}

	glog.Infoln("created Redis service", req.Service, "requuid", requuid)

	if rediscatalog.IsClusterMode(req.Options.Shards) {
		glog.Infoln("The cluster mode Redis is created, add the init task, requuid", requuid, req.Service)

		// for Redis cluster mode, run the init task in the background
		s.addRedisInitTask(ctx, crReq.Service, req.Options.Shards, req.Options.ReplicasPerShard, requuid)
		return nil
	}

	// redis single instance or master-slave mode does not require additional init work. set service initialized
	glog.Infoln("created Redis service", req.Service, "requuid", requuid)

	return s.managecli.SetServiceInitialized(ctx, req.Service)
}

func (s *CatalogHTTPServer) addRedisInitTask(ctx context.Context, req *manage.ServiceCommonRequest,
	shards int64, replicasPerShard int64, requuid string) {

	taskReq := rediscatalog.GenDefaultInitTaskRequest(req, shards, replicasPerShard, s.serverurl)

	s.catalogSvcInit.addInitTask(ctx, taskReq)

	glog.Infoln("add init task for Redis service", req, "requuid", requuid)
}

func (s *CatalogHTTPServer) updateRedisConfigs(ctx context.Context, req *manage.ServiceCommonRequest, requuid string) error {
	// update the member config with member static ip
	listReq := &manage.ListServiceMemberRequest{
		Service: req,
	}
	members, err := s.managecli.ListServiceMember(ctx, listReq)
	if err != nil {
		glog.Errorln("ListServiceMembers failed", err, "requuid", requuid, req)
		return err
	}

	for _, m := range members {
		// update member config with static ip
		getReq := &manage.GetMemberConfigFileRequest{
			Service:        req,
			MemberName:     m.MemberName,
			ConfigFileName: catalog.MEMBER_FILE_NAME,
		}
		cfgfile, err := s.managecli.GetMemberConfigFile(ctx, getReq)
		if err != nil {
			glog.Errorln("get member config file error", err, "requuid", requuid, getReq)
			return err
		}

		newContent := rediscatalog.SetMemberStaticIP(cfgfile.Spec.Content, m.Spec.StaticIP)

		updateReq := &manage.UpdateMemberConfigRequest{
			Service:           req,
			MemberName:        m.MemberName,
			ConfigFileName:    catalog.MEMBER_FILE_NAME,
			ConfigFileContent: newContent,
		}
		err = s.managecli.UpdateMemberConfig(ctx, updateReq)
		if err != nil {
			glog.Errorln("updateMemberConfig error", err, "requuid", requuid, getReq)
			return err
		}

		glog.Infoln("update member", m.MemberName, "config with ip", m.Spec.StaticIP, "requuid", requuid)
	}
	return nil
}

func (s *CatalogHTTPServer) setRedisInit(ctx context.Context, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogSetRedisInitRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogSetRedisInitRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	if req.Cluster != s.cluster || req.Region != s.region {
		glog.Errorln("CatalogSetRedisInitRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req)
		return clienterr.New(http.StatusBadRequest, "invalid region or cluster")
	}

	glog.Infoln("setRedisInit", req.ServiceName, "requuid", requuid)

	commonReq := &manage.ServiceCommonRequest{
		Region:             req.Region,
		Cluster:            req.Cluster,
		ServiceName:        req.ServiceName,
		CatalogServiceType: common.CatalogService_Redis,
	}

	// enable redis auth
	statusMsg := "enable redis auth"
	s.catalogSvcInit.UpdateTaskStatusMsg(req.ServiceName, statusMsg)
	err = s.enableRedisAuth(ctx, commonReq, requuid)
	if err != nil {
		glog.Errorln("enableRedisAuth error", err, "requuid", requuid, req)
		return err
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
		return err
	}

	err = s.managecli.StartService(ctx, commonReq)
	if err != nil {
		glog.Errorln("StartService error", err, "requuid", requuid, req)
		return err
	}

	// set service initialized
	glog.Infoln("all containers restarted, set service initialized, requuid", requuid, req)

	return s.managecli.SetServiceInitialized(ctx, commonReq)
}

func (s *CatalogHTTPServer) enableRedisAuth(ctx context.Context, req *manage.ServiceCommonRequest, requuid string) error {
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
	newContent := rediscatalog.EnableRedisAuth(cfgfile.Spec.Content)

	updateReq := &manage.UpdateServiceConfigRequest{
		Service:           req,
		ConfigFileName:    catalog.SERVICE_FILE_NAME,
		ConfigFileContent: newContent,
	}
	err = s.managecli.UpdateServiceConfig(ctx, updateReq)
	if err != nil {
		glog.Errorln("updateServiceConfig error", err, "requuid", requuid, req)
		return err
	}

	glog.Infoln("enabled redis auth, requuid", requuid, req)
	return nil
}
