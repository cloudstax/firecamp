package manageserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/catalog/kafka"
	"github.com/cloudstax/firecamp/catalog/kafkamanager"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/manage"
)

func (s *ManageHTTPServer) createKafkaService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateKafkaRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	// get the zk service
	zksvc, err := s.dbIns.GetService(ctx, s.cluster, req.Options.ZkServiceName)
	if err != nil {
		glog.Errorln("get zk service", req.Options.ZkServiceName, "error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("get zk service", zksvc, "requuid", requuid)

	zkattr, err := s.dbIns.GetServiceAttr(ctx, zksvc.ServiceUUID)
	if err != nil {
		glog.Errorln("get zk service attr", zksvc.ServiceUUID, "error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
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
	crReq, err := kafkacatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Options, req.Resource, zkattr)
	if err != nil {
		glog.Errorln("GenDefaultCreateServiceRequest error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}
	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("created kafka service", serviceUUID, "requuid", requuid, req.Service)

	// kafka does not require additional init work. set service initialized
	errmsg, errcode = s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
	if errcode != http.StatusOK {
		return errmsg, errcode
	}

	// send back the jmx remote user & passwd
	userAttr := &common.KafkaUserAttr{}
	err = json.Unmarshal(crReq.UserAttr.AttrBytes, userAttr)
	if err != nil {
		glog.Errorln("Unmarshal user attr error", err, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	resp := &manage.CatalogCreateKafkaResponse{
		JmxRemoteUser:   userAttr.JmxRemoteUser,
		JmxRemotePasswd: userAttr.JmxRemotePasswd,
	}
	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal CatalogCreateKafkaResponse error", err, "requuid", requuid, req.Service, req.Options)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) createKafkaManagerService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateKafkaManagerRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaManagerRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = kafkamanagercatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaManagerRequest parameters are not valid, requuid", requuid, req)
		return err.Error(), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaManagerRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	// get the zk service
	zksvc, err := s.dbIns.GetService(ctx, s.cluster, req.Options.ZkServiceName)
	if err != nil {
		glog.Errorln("get zk service", req.Options.ZkServiceName, "error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("get zk service", zksvc, "requuid", requuid)

	zkattr, err := s.dbIns.GetServiceAttr(ctx, zksvc.ServiceUUID)
	if err != nil {
		glog.Errorln("get zk service attr", zksvc.ServiceUUID, "error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	// create the service in the control plane and the container platform
	crReq := kafkamanagercatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.cluster,
		req.Service.ServiceName, req.Options, req.Resource, zkattr)
	if err != nil {
		glog.Errorln("GenDefaultCreateServiceRequest error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createContainerService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("created kafka manager service", serviceUUID, "requuid", requuid, req.Service)

	// kafka manager does not require additional init work. set service initialized
	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
}

func (s *ManageHTTPServer) updateKafkaService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogUpdateKafkaRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogUpdateKafkaRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("CatalogUpdateKafkaRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	err = kafkacatalog.ValidateUpdateRequest(req)
	if err != nil {
		glog.Errorln("invalid request", err, "requuid", requuid, req.Service, req)
		return err.Error(), http.StatusBadRequest
	}

	svc, err := s.dbIns.GetService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("update kafka service configs, requuid", requuid, svc, req)

	err = s.updateKafkaConfigs(ctx, svc.ServiceUUID, req, requuid)
	if err != nil {
		glog.Errorln("updateCasConfigs error", err, "requuid", requuid, req.Service, req)
		return manage.ConvertToHTTPError(err)
	}

	return "", http.StatusOK
}

func (s *ManageHTTPServer) updateKafkaConfigs(ctx context.Context, serviceUUID string, req *manage.CatalogUpdateKafkaRequest, requuid string) error {
	attr, err := s.dbIns.GetServiceAttr(ctx, serviceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "serviceUUID", serviceUUID, "requuid", requuid, req)
		return err
	}

	ua := &common.KafkaUserAttr{}
	if attr.UserAttr != nil {
		// sanity check
		if attr.UserAttr.ServiceType != common.CatalogService_Kafka {
			glog.Errorln("not a kafka service", attr.UserAttr.ServiceType, "serviceUUID", serviceUUID, "requuid", requuid)
			return errors.New("the service is not a kafka service")
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

	newua := db.CopyKafkaUserAttr(ua)
	updated := false

	// update kafka java env file
	if req.HeapSizeMB != 0 && req.HeapSizeMB != ua.HeapSizeMB {
		err = s.updateKafkaHeapSize(ctx, serviceUUID, members, req.HeapSizeMB, ua.HeapSizeMB, requuid)
		if err != nil {
			glog.Errorln("updateKafkaHeapSize error", err, "serviceUUID", serviceUUID, "requuid", requuid, req)
			return err
		}

		updated = true
		newua.HeapSizeMB = req.HeapSizeMB
	}

	// update kafka config file
	if (req.AllowTopicDel != nil && *req.AllowTopicDel != ua.AllowTopicDel) ||
		(req.RetentionHours != 0 && req.RetentionHours != ua.RetentionHours) {
		// update user attr
		updated = true
		if req.AllowTopicDel != nil && *req.AllowTopicDel != ua.AllowTopicDel {
			newua.AllowTopicDel = *req.AllowTopicDel
		}
		if req.RetentionHours != 0 && req.RetentionHours != ua.RetentionHours {
			newua.RetentionHours = req.RetentionHours
		}

		// update the server properties file
		err = s.updateKafkaServerPropConfFile(ctx, serviceUUID, members, newua, ua, requuid)
		if err != nil {
			glog.Errorln("updateKafkaServerPropConfFile error", err, "serviceUUID", serviceUUID, "requuid", requuid, req)
			return err
		}
	}

	// update jmx file
	if len(req.JmxRemoteUser) != 0 && len(req.JmxRemotePasswd) != 0 &&
		(req.JmxRemoteUser != ua.JmxRemoteUser || req.JmxRemotePasswd != ua.JmxRemotePasswd) {
		err = s.updateJmxConfigFile(ctx, serviceUUID, members, req.JmxRemoteUser, req.JmxRemotePasswd, requuid)
		if err != nil {
			glog.Errorln("updateJmxConfigFile error", err, "serviceUUID", serviceUUID, "requuid", requuid, req)
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
			ServiceType: common.CatalogService_Kafka,
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

func (s *ManageHTTPServer) updateKafkaServerPropConfFile(ctx context.Context, serviceUUID string, members []*common.ServiceMember, newua *common.KafkaUserAttr, ua *common.KafkaUserAttr, requuid string) error {
	for _, member := range members {
		var cfg *common.MemberConfig
		cfgIndex := -1
		for i, c := range member.Configs {
			if kafkacatalog.IsServerPropConfFile(c.FileName) {
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

		// update the original member server properties conf file content
		newContent := kafkacatalog.UpdateServerPropFile(newua, ua, cfgfile.Content)
		err = s.updateMemberConfig(ctx, member, cfgfile, cfgIndex, newContent, requuid)
		if err != nil {
			glog.Errorln("updateMemberConfig error", err, "requuid", requuid, cfg, member)
			return err
		}

		glog.Infoln("updated kafka properties file for member", member, "requuid", requuid)
	}

	glog.Infoln("updated the properties file for kafka service", serviceUUID, "requuid", requuid)
	return nil
}

func (s *ManageHTTPServer) updateKafkaHeapSize(ctx context.Context, serviceUUID string, members []*common.ServiceMember, newHeapSizeMB int64, oldHeapSizeMB int64, requuid string) error {
	for _, member := range members {
		var cfg *common.MemberConfig
		cfgIndex := -1
		for i, c := range member.Configs {
			if kafkacatalog.IsJvmConfFile(c.FileName) {
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

		// update the original member jvm conf file content
		newContent := kafkacatalog.UpdateHeapSize(newHeapSizeMB, oldHeapSizeMB, cfgfile.Content)
		err = s.updateMemberConfig(ctx, member, cfgfile, cfgIndex, newContent, requuid)
		if err != nil {
			glog.Errorln("updateMemberConfig error", err, "requuid", requuid, cfg, member)
			return err
		}

		glog.Infoln("updated kafka heap size from", oldHeapSizeMB, "to", newHeapSizeMB, "for member", member, "requuid", requuid)
	}

	glog.Infoln("updated heap size for kafka service", serviceUUID, "requuid", requuid)
	return nil
}
