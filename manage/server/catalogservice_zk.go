package manageserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/catalog/zookeeper"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

func (s *ManageHTTPServer) createZkService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateZooKeeperRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateZooKeeperRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateZooKeeperRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
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
	crReq, err := zkcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Options, req.Resource)
	if err != nil {
		glog.Errorln("GenDefaultCreateServiceRequest error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}
	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("created zookeeper service", serviceUUID, "requuid", requuid, req.Service)

	// zookeeper does not require additional init work. set service initialized
	errmsg, errcode = s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
	if errcode != http.StatusOK {
		return errmsg, errcode
	}

	// send back the jmx remote user & passwd
	userAttr := &common.ZKUserAttr{}
	err = json.Unmarshal(crReq.UserAttr.AttrBytes, userAttr)
	if err != nil {
		glog.Errorln("Unmarshal user attr error", err, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	resp := &manage.CatalogCreateZooKeeperResponse{
		JmxRemoteUser:   userAttr.JmxRemoteUser,
		JmxRemotePasswd: userAttr.JmxRemotePasswd,
	}
	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal CatalogCreateZooKeeperResponse error", err, "requuid", requuid, req.Service, req.Options)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) updateZkService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogUpdateZooKeeperRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogUpdateZooKeeperRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("CatalogUpdateZooKeeperRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	err = zkcatalog.ValidateUpdateRequest(req)
	if err != nil {
		glog.Errorln("invalid request", err, "requuid", requuid, req.Service, req)
		return err.Error(), http.StatusBadRequest
	}

	svc, err := s.dbIns.GetService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("update zookeeper service configs, requuid", requuid, svc, req.Service)

	err = s.updateZkConfigs(ctx, svc.ServiceUUID, req, requuid)
	if err != nil {
		glog.Errorln("updateZkConfigs error", err, "requuid", requuid, req.Service, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	return "", http.StatusOK
}

func (s *ManageHTTPServer) updateZkConfigs(ctx context.Context, serviceUUID string, req *manage.CatalogUpdateZooKeeperRequest, requuid string) error {
	attr, err := s.dbIns.GetServiceAttr(ctx, serviceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "serviceUUID", serviceUUID, "requuid", requuid, req.Service)
		return err
	}

	ua := &common.ZKUserAttr{}
	if attr.UserAttr != nil {
		// sanity check
		if attr.UserAttr.ServiceType != common.CatalogService_ZooKeeper {
			glog.Errorln("not a zookeeper service", attr.UserAttr.ServiceType, "serviceUUID", serviceUUID, "requuid", requuid)
			return errors.New("the service is not a zookeeper service")
		}

		err = json.Unmarshal(attr.UserAttr.AttrBytes, ua)
		if err != nil {
			glog.Errorln("Unmarshal UserAttr error", err, "serviceUUID", serviceUUID, "requuid", requuid, req.Service)
			return err
		}
	}

	members, err := s.dbIns.ListServiceMembers(ctx, serviceUUID)
	if err != nil {
		glog.Errorln("ListServiceMembers failed", err, "serviceUUID", serviceUUID, "requuid", requuid, req.Service)
		return err
	}

	newua := db.CopyZKUserAttr(ua)
	updated := false

	// update the java env file
	if req.HeapSizeMB != 0 && req.HeapSizeMB != ua.HeapSizeMB {
		err = s.updateZkHeapSize(ctx, serviceUUID, members, req.HeapSizeMB, ua.HeapSizeMB, requuid)
		if err != nil {
			glog.Errorln("updateZkHeapSize error", err, "serviceUUID", serviceUUID, "requuid", requuid, req.Service)
			return err
		}

		updated = true
		newua.HeapSizeMB = req.HeapSizeMB
	}

	// update jmx file
	if len(req.JmxRemoteUser) != 0 && len(req.JmxRemotePasswd) != 0 &&
		(req.JmxRemoteUser != ua.JmxRemoteUser || req.JmxRemotePasswd != ua.JmxRemotePasswd) {
		err = s.updateJmxPasswdFile(ctx, serviceUUID, members, req.JmxRemoteUser, req.JmxRemotePasswd, requuid)
		if err != nil {
			glog.Errorln("updateJmxPasswdFile error", err, "serviceUUID", serviceUUID, "requuid", requuid, req.Service)
			return err
		}

		if req.JmxRemoteUser != ua.JmxRemoteUser {
			// update jmx access file
			err = s.updateJmxAccessFile(ctx, serviceUUID, members, req.JmxRemoteUser, ua.JmxRemoteUser, requuid)
			if err != nil {
				glog.Errorln("updateJmxAccessFile error", err, "serviceUUID", serviceUUID, "requuid", requuid, req.Service)
				return err
			}
		}

		updated = true
		newua.JmxRemoteUser = req.JmxRemoteUser
		newua.JmxRemotePasswd = req.JmxRemotePasswd
	}

	// update the user attr
	if updated {
		b, err := json.Marshal(newua)
		if err != nil {
			glog.Errorln("Marshal user attr error", err, "serviceUUID", serviceUUID, "requuid", requuid, req.Service)
			return err
		}
		userAttr := &common.ServiceUserAttr{
			ServiceType: common.CatalogService_ZooKeeper,
			AttrBytes:   b,
		}

		newAttr := db.UpdateServiceUserAttr(attr, userAttr)
		err = s.dbIns.UpdateServiceAttr(ctx, attr, newAttr)
		if err != nil {
			glog.Errorln("UpdateServiceAttr error", err, "serviceUUID", serviceUUID, "requuid", requuid, req.Service)
			return err
		}

		glog.Infoln("updated service configs, requuid", requuid, req.Service)
	} else {
		glog.Infoln("service configs are not changed, skip the update, requuid", requuid, req.Service)
	}

	return nil
}

func (s *ManageHTTPServer) updateZkHeapSize(ctx context.Context, serviceUUID string, members []*common.ServiceMember, newHeapSizeMB int64, oldHeapSizeMB int64, requuid string) error {
	for _, member := range members {
		var cfg *common.MemberConfig
		cfgIndex := -1
		for i, c := range member.Configs {
			if zkcatalog.IsJavaEnvFile(c.FileName) {
				cfg = c
				cfgIndex = i
				break
			}
		}
		if cfgIndex == -1 {
			errmsg := fmt.Sprintf("the java env file not found for member %s, requuid %s", member.MemberName, requuid)
			glog.Errorln(errmsg)
			return errors.New(errmsg)
		}

		// fetch the config file
		cfgfile, err := s.dbIns.GetConfigFile(ctx, member.ServiceUUID, cfg.FileID)
		if err != nil {
			glog.Errorln("GetConfigFile error", err, "requuid", requuid, cfg, member)
			return err
		}

		// update the original member jvm conf file content
		newContent := zkcatalog.UpdateHeapSize(newHeapSizeMB, oldHeapSizeMB, cfgfile.Content)
		_, err = s.updateMemberConfig(ctx, member, cfgfile, cfgIndex, newContent, requuid)
		if err != nil {
			glog.Errorln("updateMemberConfig error", err, "requuid", requuid, cfg, member)
			return err
		}

		glog.Infoln("updated zookeeper heap size from", oldHeapSizeMB, "to", newHeapSizeMB, "for member", member, "requuid", requuid)
	}

	glog.Infoln("updated heap size for zookeeper service", serviceUUID, "requuid", requuid)
	return nil
}

// Upgrade to 0.9.5

func (s *ManageHTTPServer) upgradeZkToV095(ctx context.Context, attr *common.ServiceAttr, req *manage.ServiceCommonRequest, requuid string) error {
	// upgrade zookeeper service created before version 0.9.5.
	// - create jmx password and access files
	// - update java env file
	// - add jmx user and password to ZKUserAttr
	ua := &common.ZKUserAttr{}
	err := json.Unmarshal(attr.UserAttr.AttrBytes, ua)
	if err != nil {
		glog.Errorln("Unmarshal UserAttr error", err, "requuid", requuid, req)
		return err
	}

	members, err := s.dbIns.ListServiceMembers(ctx, attr.ServiceUUID)
	if err != nil {
		glog.Errorln("ListServiceMembers failed", err, "requuid", requuid, req)
		return err
	}

	jmxUser := catalog.JmxDefaultRemoteUser
	jmxPasswd := utils.GenUUID()

	newua := db.CopyZKUserAttr(ua)
	newua.JmxRemoteUser = jmxUser
	newua.JmxRemotePasswd = jmxPasswd

	// create jmx password and access files
	err = s.createJmxFiles(ctx, members, jmxUser, jmxPasswd, requuid)
	if err != nil {
		glog.Errorln("createJmxFiles error", err, "requuid", requuid, req)
		return err
	}

	// list members again as createJmxFiles updates the member configs.
	// could let createJmxFiles function to return the updated members.
	// to simplify the implementation, simply list members again, as upgrade is
	// only executed once for one service.
	members, err = s.dbIns.ListServiceMembers(ctx, attr.ServiceUUID)
	if err != nil {
		glog.Errorln("ListServiceMembers failed", err, "requuid", requuid, req)
		return err
	}

	// upgrade zookeeper java env file
	err = s.upgradeZkJavaEnvFileV095(ctx, attr, members, ua.HeapSizeMB, requuid)
	if err != nil {
		glog.Errorln("upgradeZkJavaEnvFileV095 error", err, "requuid", requuid, req)
		return err
	}

	// update the user attr
	b, err := json.Marshal(newua)
	if err != nil {
		glog.Errorln("Marshal user attr error", err, "requuid", requuid, req)
		return err
	}
	userAttr := &common.ServiceUserAttr{
		ServiceType: common.CatalogService_ZooKeeper,
		AttrBytes:   b,
	}

	newAttr := db.UpdateServiceUserAttr(attr, userAttr)
	err = s.dbIns.UpdateServiceAttr(ctx, attr, newAttr)
	if err != nil {
		glog.Errorln("UpdateServiceAttr error", err, "requuid", requuid, req)
		return err
	}

	glog.Infoln("upgraded zookeeper to version 0.9.5, requuid", requuid, req)
	return nil
}

func (s *ManageHTTPServer) upgradeZkJavaEnvFileV095(ctx context.Context, attr *common.ServiceAttr, members []*common.ServiceMember, heapSizeMB int64, requuid string) error {
	for _, member := range members {
		var cfg *common.MemberConfig
		cfgIndex := -1
		for i, c := range member.Configs {
			if zkcatalog.IsJavaEnvFile(c.FileName) {
				cfg = c
				cfgIndex = i
				break
			}
		}
		if cfgIndex == -1 {
			errmsg := fmt.Sprintf("not find jvm conf file, service %s, requuid %s", attr.ServiceName, requuid)
			glog.Errorln(errmsg)
			return errors.New(errmsg)
		}

		// fetch the config file
		cfgfile, err := s.dbIns.GetConfigFile(ctx, member.ServiceUUID, cfg.FileID)
		if err != nil {
			glog.Errorln("GetConfigFile error", err, "requuid", requuid, cfg, member)
			return err
		}

		// update the original member java env conf file content
		memberHost := dns.GenDNSName(member.MemberName, attr.DomainName)
		newContent := zkcatalog.UpgradeJavaEnvFileContentToV095(heapSizeMB, memberHost)
		_, err = s.updateMemberConfig(ctx, member, cfgfile, cfgIndex, newContent, requuid)
		if err != nil {
			glog.Errorln("updateMemberConfig error", err, "requuid", requuid, cfg, member)
			return err
		}

		glog.Infoln("add jmx configs to java env file for member", member, "requuid", requuid)
	}

	glog.Infoln("upgraded java env file for kafka service, requuid", requuid)
	return nil
}
