package manageserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/catalog/redis"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/manage"
)

func (s *ManageHTTPServer) createRedisService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateRedisRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateRedisRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateRedisRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	glog.Infoln("create redis service", req.Service, req.Options, req.Resource)

	err = rediscatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("CatalogCreateRedisRequest parameters are not valid, requuid", requuid, req.Service, req.Options)
		return err.Error(), http.StatusBadRequest
	}

	// create the service in the control plane and the container platform
	crReq := rediscatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Resource, req.Options)

	// create the service in the control plane
	serviceUUID, err := s.svc.CreateService(ctx, crReq, s.domain, s.vpcID)
	if err != nil {
		glog.Errorln("create service error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("created Redis service in the control plane", serviceUUID, "requuid", requuid, req.Service, req.Options)

	// update static ip in member config file
	err = s.updateRedisConfigs(ctx, serviceUUID, requuid)
	if err != nil {
		glog.Errorln("updateRedisConfigs error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	err = s.createContainerService(ctx, crReq, serviceUUID, requuid)
	if err != nil {
		glog.Errorln("createContainerService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("created Redis service", serviceUUID, "requuid", requuid, req.Service, req.Options)

	if rediscatalog.IsClusterMode(req.Options.Shards) {
		glog.Infoln("The cluster mode Redis is created, add the init task, requuid", requuid, req.Service, req.Options)

		// for Redis cluster mode, run the init task in the background
		err = s.addRedisInitTask(ctx, crReq.Service, serviceUUID, req.Options.Shards, req.Options.ReplicasPerShard, requuid)
		if err != nil {
			glog.Errorln("addRedisInitTask error", err, "requuid", requuid, req.Service)
			return manage.ConvertToHTTPError(err)
		}

		return "", http.StatusOK
	}

	// redis single instance or master-slave mode does not require additional init work. set service initialized
	glog.Infoln("created Redis service", serviceUUID, "requuid", requuid, req.Service, req.Options)

	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
}

func (s *ManageHTTPServer) addRedisInitTask(ctx context.Context, req *manage.ServiceCommonRequest,
	serviceUUID string, shards int64, replicasPerShard int64, requuid string) error {
	logCfg := s.logIns.CreateStreamLogConfig(ctx, s.cluster, req.ServiceName, serviceUUID, common.TaskTypeInit)

	taskOpts, err := rediscatalog.GenDefaultInitTaskRequest(req, logCfg, shards, replicasPerShard, serviceUUID, s.manageurl)
	if err != nil {
		return err
	}

	task := &serviceTask{
		serviceUUID: serviceUUID,
		serviceName: req.ServiceName,
		serviceType: common.CatalogService_Redis,
		opts:        taskOpts,
	}

	s.catalogSvcInit.addInitTask(ctx, task)

	glog.Infoln("add init task for Redis service", serviceUUID, "requuid", requuid, req)
	return nil
}

func (s *ManageHTTPServer) updateRedisConfigs(ctx context.Context, serviceUUID string, requuid string) error {
	attr, err := s.dbIns.GetServiceAttr(ctx, serviceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "serviceUUID", serviceUUID, "requuid", requuid)
		return err
	}

	// update the static ip in the member.conf file
	members, err := s.dbIns.ListServiceMembers(ctx, serviceUUID)
	if err != nil {
		glog.Errorln("ListServiceMembers failed", err, "serviceUUID", serviceUUID, "requuid", requuid)
		return err
	}

	for _, m := range members {
		// update member config with static ip
		find := false
		for cfgIndex, cfg := range m.Spec.Configs {
			if catalog.IsMemberConfigFile(cfg.FileName) {
				cfgfile, err := s.dbIns.GetConfigFile(ctx, attr.ServiceUUID, cfg.FileID)
				if err != nil {
					glog.Errorln("get member config file error", err, cfg, "requuid", requuid, m)
					return err
				}

				newContent := rediscatalog.SetMemberStaticIP(cfgfile.Spec.Content, m.Spec.StaticIP)

				newMember, err := s.updateMemberConfig(ctx, m, cfgfile, cfgIndex, newContent, requuid)
				if err != nil {
					glog.Errorln("updateMemberConfig error", err, cfg, "requuid", requuid, m)
					return err
				}

				glog.Infoln("update member config with static ip, requuid", requuid, newMember)

				find = true
				break
			}
		}
		if !find {
			glog.Errorln("member config file not found, requuid", requuid, m)
			return errors.New("internal error - member config file not found")
		}
	}
	return nil
}

func (s *ManageHTTPServer) setRedisInit(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogSetRedisInitRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogSetRedisInitRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Cluster != s.cluster || req.Region != s.region {
		glog.Errorln("CatalogSetRedisInitRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	glog.Infoln("setRedisInit", req.ServiceName, "requuid", requuid)

	// get service uuid
	service, err := s.dbIns.GetService(ctx, s.cluster, req.ServiceName)
	if err != nil {
		glog.Errorln("GetService", req, "error", err, "requuid", requuid)
		return manage.ConvertToHTTPError(err)
	}

	// get service attr
	attr, err := s.dbIns.GetServiceAttr(ctx, service.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, service)
		return manage.ConvertToHTTPError(err)
	}

	// enable redis auth
	statusMsg := "enable redis auth"
	s.catalogSvcInit.UpdateTaskStatusMsg(service.ServiceUUID, statusMsg)
	err = s.enableRedisAuth(ctx, attr, requuid)
	if err != nil {
		glog.Errorln("enableRedisAuth error", err, "requuid", requuid, service)
		return manage.ConvertToHTTPError(err)
	}

	// the config files of all replicas are updated, restart all containers
	glog.Infoln("all replicas are updated, restart all containers, requuid", requuid, req)

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

func (s *ManageHTTPServer) enableRedisAuth(ctx context.Context, attr *common.ServiceAttr, requuid string) error {
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
			newContent := rediscatalog.EnableRedisAuth(cfgfile.Spec.Content)

			err = s.updateServiceConfigFile(ctx, attr, cfgIndex, cfgfile, newContent, requuid)
			if err != nil {
				glog.Errorln("updateServiceConfigFile error", err, cfgfile, "requuid", requuid, attr)
				return err
			}

			glog.Infoln("enabled redis auth, requuid", requuid, attr)
			return nil
		}
	}

	glog.Errorln("internal error - redis.conf not found, requuid", requuid, attr)
	return errors.New("internal error - redis.conf not found")
}
