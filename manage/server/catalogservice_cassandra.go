package manageserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/catalog/cassandra"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/manage"
)

func (s *ManageHTTPServer) createCasService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateCassandraRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateCassandraRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("CatalogCreateCassandraRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	err = cascatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("invalid request", err, "requuid", requuid, req.Service, req.Options)
		return err.Error(), http.StatusBadRequest
	}

	// JmxRemotePasswd may be generated (uuid) locally.
	if len(req.Options.JmxRemotePasswd) == 0 {
		jmxUser, jmxPasswd, err := s.getExistingJmxPasswd(ctx, req.Service, requuid)
		if err != nil {
			glog.Errorln("getExistingJmxPasswd error", err, "requuid", requuid, req.Service)
			return manage.ConvertToHTTPError(err)
		}
		if len(jmxPasswd) != 0 {
			req.Options.JmxRemoteUser = jmxUser
			req.Options.JmxRemotePasswd = jmxPasswd
		}
	}

	// create the service in the control plane and the container platform
	crReq, err := cascatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs,
		s.cluster, req.Service.ServiceName, req.Options, req.Resource)
	if err != nil {
		glog.Errorln("GenDefaultCreateServiceRequest error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("Cassandra is created, add the init task, requuid", requuid, req.Service)

	if req.Options.Replicas != 1 {
		// run the init task in the background
		s.addCasInitTask(ctx, crReq.Service, serviceUUID, requuid)
	} else {
		glog.Infoln("single node Cassandra, skip the init task, requuid", requuid, req.Service, req.Options)

		errmsg, errcode := s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
		if errcode != http.StatusOK {
			return errmsg, errcode
		}
	}

	// send back the jmx remote user & passwd
	userAttr := &common.CasUserAttr{}
	err = json.Unmarshal(crReq.UserAttr.AttrBytes, userAttr)
	if err != nil {
		glog.Errorln("Unmarshal user attr error", err, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	resp := &manage.CatalogCreateCassandraResponse{
		JmxRemoteUser:   userAttr.JmxRemoteUser,
		JmxRemotePasswd: userAttr.JmxRemotePasswd,
	}
	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal CatalogCreateCassandraResponse error", err, "requuid", requuid, req.Service, req.Options)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) addCasInitTask(ctx context.Context,
	req *manage.ServiceCommonRequest, serviceUUID string, requuid string) {
	logCfg := s.logIns.CreateStreamLogConfig(ctx, s.cluster, req.ServiceName, serviceUUID, common.TaskTypeInit)
	taskOpts := cascatalog.GenDefaultInitTaskRequest(req, logCfg, serviceUUID, s.manageurl)

	task := &serviceTask{
		serviceUUID: serviceUUID,
		serviceName: req.ServiceName,
		serviceType: common.CatalogService_Cassandra,
		opts:        taskOpts,
	}

	s.catalogSvcInit.addInitTask(ctx, task)

	glog.Infoln("add init task for service", serviceUUID, "requuid", requuid, req)
}

func (s *ManageHTTPServer) scaleCasService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogScaleCassandraRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogScaleCassandraRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("CatalogScaleCassandraRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	svc, err := s.dbIns.GetService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	attr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	if req.Replicas == attr.Replicas {
		glog.Infoln("service already has", req.Replicas, "replicas, requuid", requuid, attr)
		return "", http.StatusOK
	}

	// not allow scaling down, as Cassandra nodes are peers. When one node goes down,
	// Cassandra will automatically recover the failed replica on another node.
	if req.Replicas < attr.Replicas {
		errmsg := "scale down Cassandra service is not supported"
		glog.Errorln(errmsg, "requuid", requuid, req.Service)
		return errmsg, http.StatusBadRequest
	}

	// TODO scaling from 1 node requires to add new seed nodes.
	if attr.Replicas == 1 {
		errmsg := "not support to scale from 1 node, please have at least 3 nodes"
		glog.Errorln(errmsg, "requuid", requuid, req.Service)
		return errmsg, http.StatusBadRequest
	}

	glog.Infoln("scale cassandra service from", attr.Replicas, "to", req.Replicas, "requuid", requuid, attr)

	// create new service members
	userAttr := &common.CasUserAttr{}
	err = json.Unmarshal(attr.UserAttr.AttrBytes, userAttr)
	if err != nil {
		glog.Errorln("Unmarshal user attr error", err, "requuid", requuid, attr)
		return manage.ConvertToHTTPError(err)
	}

	opts := &manage.CatalogCassandraOptions{
		Replicas:        req.Replicas,
		Volume:          &attr.Volumes.PrimaryVolume,
		JournalVolume:   &attr.Volumes.JournalVolume,
		HeapSizeMB:      userAttr.HeapSizeMB,
		JmxRemoteUser:   userAttr.JmxRemoteUser,
		JmxRemotePasswd: userAttr.JmxRemotePasswd,
	}

	// TODO for now, simply create the specs for all members, same for CheckAndCreateServiceMembers.
	// 			optimize to bypass the existing members in the future.
	newAttr := db.UpdateServiceReplicas(attr, req.Replicas)
	crReq, err := cascatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs,
		s.cluster, req.Service.ServiceName, opts, &(attr.Resource))
	if err != nil {
		glog.Errorln("GenDefaultCreateServiceRequest error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	err = s.svc.CheckAndCreateServiceMembers(ctx, newAttr, crReq)
	if err != nil {
		glog.Errorln("create new service members error", err, "requuid", requuid, req.Service, req.Replicas)
		return manage.ConvertToHTTPError(err)
	}

	// scale the container service
	glog.Infoln("scale container servie to", req.Replicas, "requuid", requuid, attr)

	err = s.containersvcIns.ScaleService(ctx, s.cluster, req.Service.ServiceName, req.Replicas)
	if err != nil {
		glog.Errorln("ScaleService new service members error", err, "requuid", requuid, req.Service, req.Replicas)
		return manage.ConvertToHTTPError(err)
	}

	// update service attr
	glog.Infoln("update service attr to", req.Replicas, "requuid", requuid, attr)

	err = s.dbIns.UpdateServiceAttr(ctx, attr, newAttr)
	if err != nil {
		glog.Errorln("UpdateServiceAttr error", err, "requuid", requuid, req.Service, req.Replicas)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("service scaled to", req.Replicas, "requuid", requuid, newAttr)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) updateCasService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogUpdateCassandraRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogUpdateCassandraRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("CatalogUpdateCassandraRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	err = cascatalog.ValidateUpdateRequest(req)
	if err != nil {
		glog.Errorln("invalid request", err, "requuid", requuid, req.Service, req)
		return err.Error(), http.StatusBadRequest
	}

	svc, err := s.dbIns.GetService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("update cassandra service configs, requuid", requuid, svc, req)

	err = s.updateCasConfigs(ctx, svc.ServiceUUID, req, requuid)
	if err != nil {
		glog.Errorln("updateCasConfigs error", err, "requuid", requuid, req.Service, req)
		return manage.ConvertToHTTPError(err)
	}

	return "", http.StatusOK
}

func (s *ManageHTTPServer) updateCasConfigs(ctx context.Context, serviceUUID string, req *manage.CatalogUpdateCassandraRequest, requuid string) error {
	attr, err := s.dbIns.GetServiceAttr(ctx, serviceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "serviceUUID", serviceUUID, "requuid", requuid, req)
		return err
	}

	ua := &common.CasUserAttr{}
	if attr.UserAttr != nil {
		// sanity check
		if attr.UserAttr.ServiceType != common.CatalogService_Cassandra {
			glog.Errorln("not a cassandra service", attr.UserAttr.ServiceType, "serviceUUID", serviceUUID, "requuid", requuid)
			return errors.New("the service is not a cassandra service")
		}

		err = json.Unmarshal(attr.UserAttr.AttrBytes, ua)
		if err != nil {
			glog.Errorln("Unmarshal UserAttr error", err, "serviceUUID", serviceUUID, "requuid", requuid, req)
			return err
		}
	}

	members, err := s.dbIns.ListServiceMembers(ctx, serviceUUID)
	if err != nil {
		glog.Errorln("ListServiceMembers failed", err, "serviceUUID", serviceUUID, "requuid", requuid, req)
		return err
	}

	newua := db.CopyCasUserAttr(ua)
	updated := false
	if req.HeapSizeMB != 0 && req.HeapSizeMB != ua.HeapSizeMB {
		err = s.updateCasHeapSize(ctx, serviceUUID, members, req.HeapSizeMB, requuid)
		if err != nil {
			glog.Errorln("updateCasHeapSize error", err, "serviceUUID", serviceUUID, "requuid", requuid, req)
			return err
		}

		updated = true
		newua.HeapSizeMB = req.HeapSizeMB
	}

	if len(req.JmxRemoteUser) != 0 && len(req.JmxRemotePasswd) != 0 &&
		(req.JmxRemoteUser != ua.JmxRemoteUser || req.JmxRemotePasswd != ua.JmxRemotePasswd) {
		err = s.updateCasJmx(ctx, serviceUUID, members, req.JmxRemoteUser, req.JmxRemotePasswd, requuid)
		if err != nil {
			glog.Errorln("updateCasJmx error", err, "serviceUUID", serviceUUID, "requuid", requuid, req)
			return err
		}

		updated = true
		newua.JmxRemoteUser = req.JmxRemoteUser
		newua.JmxRemotePasswd = req.JmxRemotePasswd
	}

	// update the user attr
	if updated {
		b, err := json.Marshal(newua)
		if err != nil {
			glog.Errorln("Marshal user attr error", err, "serviceUUID", serviceUUID, "requuid", requuid, req)
			return err
		}
		userAttr := &common.ServiceUserAttr{
			ServiceType: common.CatalogService_Cassandra,
			AttrBytes:   b,
		}

		newAttr := db.UpdateServiceUserAttr(attr, userAttr)
		err = s.dbIns.UpdateServiceAttr(ctx, attr, newAttr)
		if err != nil {
			glog.Errorln("UpdateServiceAttr error", err, "serviceUUID", serviceUUID, "requuid", requuid, req)
			return err
		}

		glog.Infoln("updated service configs from", ua, "to", newua, "requuid", requuid, req.Service)
	} else {
		glog.Infoln("service configs are not changed, skip the update, requuid", requuid, req.Service, req)
	}

	return nil
}

func (s *ManageHTTPServer) updateCasHeapSize(ctx context.Context, serviceUUID string, members []*common.ServiceMember, heapSizeMB int64, requuid string) error {
	for _, member := range members {
		var cfg *common.MemberConfig
		cfgIndex := -1
		for i, c := range member.Configs {
			if cascatalog.IsJvmConfFile(c.FileName) {
				cfg = c
				cfgIndex = i
				break
			}
		}

		// fetch the config file
		cfgfile, err := s.dbIns.GetConfigFile(ctx, member.ServiceUUID, cfg.FileID)
		if err != nil {
			glog.Errorln("GetConfigFile error", err, "requuid", requuid, cfg, member)
			return err
		}

		// replace the original member jvm conf file content
		// TODO if there are like 100 nodes, it may be worth for all members to use the same config file.
		newContent := cascatalog.NewJVMConfContent(heapSizeMB)
		err = s.updateMemberConfig(ctx, member, cfgfile, cfgIndex, newContent, requuid)
		if err != nil {
			glog.Errorln("updateMemberConfig error", err, "requuid", requuid, cfg, member)
			return err
		}

		glog.Infoln("updated cassandra heap size to", heapSizeMB, "for member", member, "requuid", requuid)
	}

	glog.Infoln("updated heap size for cassandra service", serviceUUID, "requuid", requuid)
	return nil
}

func (s *ManageHTTPServer) updateCasJmx(ctx context.Context, serviceUUID string, members []*common.ServiceMember, jmxUser string, jmxPasswd string, requuid string) error {
	for _, member := range members {
		var cfg *common.MemberConfig
		cfgIndex := -1
		for i, c := range member.Configs {
			if catalog.IsJmxConfFile(c.FileName) {
				cfg = c
				cfgIndex = i
				break
			}
		}

		// fetch the config file
		cfgfile, err := s.dbIns.GetConfigFile(ctx, member.ServiceUUID, cfg.FileID)
		if err != nil {
			glog.Errorln("GetConfigFile error", err, "requuid", requuid, cfg, member)
			return err
		}

		// replace the original member jmx conf file content
		// TODO if there are like 100 nodes, it may be worth for all members to use the same config file.
		newContent := catalog.CreateJmxConfFileContent(jmxUser, jmxPasswd)
		err = s.updateMemberConfig(ctx, member, cfgfile, cfgIndex, newContent, requuid)
		if err != nil {
			glog.Errorln("updateMemberConfig error", err, "requuid", requuid, cfg, member)
			return err
		}

		glog.Infoln("updated cassandra jmx user & password", jmxUser, jmxPasswd, "for member", member, "requuid", requuid)
	}

	glog.Infoln("updated jmx user & password for cassandra service", serviceUUID, "requuid", requuid)
	return nil
}
