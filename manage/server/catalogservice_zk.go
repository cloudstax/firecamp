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

	// create the service in the control plane and the container platform
	crReq, jmxUser, jmxPasswd := zkcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Options, req.Resource)

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

	resp := &manage.CatalogCreateZooKeeperResponse{
		JmxRemoteUser:   jmxUser,
		JmxRemotePasswd: jmxPasswd,
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
	members, err = s.createJmxFiles(ctx, members, jmxUser, jmxPasswd, requuid)
	if err != nil {
		glog.Errorln("createJmxFiles error", err, "requuid", requuid, req)
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
		var cfg *common.ConfigID
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
