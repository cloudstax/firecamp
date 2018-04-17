package manageserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/catalog/elasticsearch"
	"github.com/cloudstax/firecamp/catalog/kafka"
	"github.com/cloudstax/firecamp/catalog/kafkaconnect"
	"github.com/cloudstax/firecamp/catalog/kafkamanager"
	"github.com/cloudstax/firecamp/catalog/zookeeper"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

func (s *ManageHTTPServer) createKafkaService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateKafkaRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkRequest(req.Service, req.Resource)
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

	zkservers := catalog.GenServiceMemberHostsWithPort(s.cluster, zkattr.Meta.ServiceName, zkattr.Spec.Replicas, zkcatalog.ClientPort)

	// create the service in the control plane and the container platform
	crReq, jmxUser, jmxPasswd := kafkacatalog.GenDefaultCreateServiceRequest(s.platform,
		s.region, s.azs, s.cluster, req.Service.ServiceName, req.Options, req.Resource, zkservers)

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

	resp := &manage.CatalogCreateKafkaResponse{
		JmxRemoteUser:   jmxUser,
		JmxRemotePasswd: jmxPasswd,
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

func (s *ManageHTTPServer) createKafkaSinkESService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateKafkaSinkESRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaSinkESRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaSinkESRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	err = kccatalog.ValidateSinkESRequest(req)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaSinkESRequest invalid request", err, "requuid", requuid, req.Service)
		return err.Error(), http.StatusBadRequest
	}

	// get the kafka service
	svc, err := s.dbIns.GetService(ctx, s.cluster, req.Options.KafkaServiceName)
	if err != nil {
		glog.Errorln("get kafka service", req.Options.KafkaServiceName, "error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("get kafka service", svc, "requuid", requuid)

	kafkaAttr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("get kafka service attr", svc, "error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	// get the elasticsearch service
	svc, err = s.dbIns.GetService(ctx, s.cluster, req.Options.ESServiceName)
	if err != nil {
		glog.Errorln("get elasticsearch service", req.Options.ESServiceName, "error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("get elasticsearch service", svc, "requuid", requuid)

	esAttr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("get elasticsearch service attr", svc, "error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	esURIs, err := escatalog.GenDataNodesURIs(esAttr)
	if err != nil {
		glog.Errorln("create elasticsearch data nodes uris error", err, svc, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	// create the service in the control plane and the container platform
	crReq, ua, err := kccatalog.GenCreateESSinkServiceRequest(s.platform, s.region, s.cluster,
		req.Service.ServiceName, kafkaAttr, req.Options, req.Resource)
	if err != nil {
		glog.Errorln("GenCreateESSinkServiceRequest error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}
	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("created kafka connector service", serviceUUID, "requuid", requuid, req.Service)

	// add the init task to actually create the elasticsearch sink task in kafka connectors
	s.addKafkaSinkESInitTask(ctx, req.Service, serviceUUID, req.Options.Replicas, ua, esURIs, requuid)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) addKafkaSinkESInitTask(ctx context.Context, req *manage.ServiceCommonRequest,
	serviceUUID string, replicas int64, ua *common.KCSinkESUserAttr, esURIs string, requuid string) {
	logCfg := s.logIns.CreateStreamLogConfig(ctx, s.cluster, req.ServiceName, serviceUUID, common.TaskTypeInit)
	taskOpts := kccatalog.GenSinkESServiceInitRequest(req, logCfg, serviceUUID, replicas, ua, esURIs, s.manageurl)

	task := &serviceTask{
		serviceUUID: serviceUUID,
		serviceName: req.ServiceName,
		serviceType: common.CatalogService_KafkaSinkES,
		opts:        taskOpts,
	}

	s.catalogSvcInit.addInitTask(ctx, task)

	glog.Infoln("add init task for service", serviceUUID, "requuid", requuid, req)
}

func (s *ManageHTTPServer) restartKafkaSinkESInitTask(ctx context.Context, req *manage.ServiceCommonRequest, attr *common.ServiceAttr, requuid string) error {
	userAttr := &common.KCSinkESUserAttr{}
	err := json.Unmarshal(attr.UserAttr.AttrBytes, userAttr)
	if err != nil {
		glog.Errorln("Unmarshal kafka sink elasticsearch user attr error", err, "requuid", requuid, attr)
		return err
	}

	// get the elasticsearch service
	svc, err := s.dbIns.GetService(ctx, s.cluster, userAttr.ESServiceName)
	if err != nil {
		glog.Errorln("get elasticsearch service", userAttr.ESServiceName, "error", err, "requuid", requuid, req)
		return err
	}

	glog.Infoln("get elasticsearch service", svc, "requuid", requuid)

	esAttr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("get elasticsearch service attr", svc, "error", err, "requuid", requuid, req)
		return err
	}

	esURIs, err := escatalog.GenDataNodesURIs(esAttr)
	if err != nil {
		glog.Errorln("create elasticsearch data nodes uris error", err, svc, "requuid", requuid, req)
		return err
	}

	s.addKafkaSinkESInitTask(ctx, req, attr.ServiceUUID, attr.Replicas, userAttr, esURIs, requuid)
	return nil
}

func (s *ManageHTTPServer) createKafkaManagerService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateKafkaManagerRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaManagerRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = kmcatalog.ValidateRequest(req.Options)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaManagerRequest parameters are not valid, requuid", requuid, req)
		return err.Error(), http.StatusBadRequest
	}

	err = s.checkRequest(req.Service, req.Resource)
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
	crReq, err := kmcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.cluster,
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

// Upgrade to 0.9.5

func (s *ManageHTTPServer) upgradeKafkaToVersion095(ctx context.Context, attr *common.ServiceAttr, req *manage.ServiceCommonRequest, requuid string) error {
	// upgrade kafka service created before version 0.9.5.
	// - update java env file
	// - create jmx password and access files
	// - add jmx user and password to KafkaUserAttr
	ua := &common.KafkaUserAttr{}
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

	newua := db.CopyKafkaUserAttr(ua)
	newua.JmxRemoteUser = jmxUser
	newua.JmxRemotePasswd = jmxPasswd

	// create jmx password and access files
	members, err = s.createJmxFiles(ctx, members, jmxUser, jmxPasswd, requuid)
	if err != nil {
		glog.Errorln("createJmxFiles error", err, "requuid", requuid, req)
		return err
	}

	// upgrade kafka java env file
	err = s.upgradeKafkaJavaEnvFileVersion095(ctx, attr, members, ua.HeapSizeMB, requuid)
	if err != nil {
		glog.Errorln("upgradeKafkaJavaEnvFileVersion095 error", err, "requuid", requuid, req)
		return err
	}

	// update the user attr
	b, err := json.Marshal(newua)
	if err != nil {
		glog.Errorln("Marshal user attr error", err, "requuid", requuid, req)
		return err
	}
	userAttr := &common.ServiceUserAttr{
		ServiceType: common.CatalogService_Kafka,
		AttrBytes:   b,
	}

	newAttr := db.UpdateServiceUserAttr(attr, userAttr)
	err = s.dbIns.UpdateServiceAttr(ctx, attr, newAttr)
	if err != nil {
		glog.Errorln("UpdateServiceAttr error", err, "requuid", requuid, req)
		return err
	}

	glog.Infoln("upgraded kafka to version 0.9.5, requuid", requuid, req)
	return nil
}

func (s *ManageHTTPServer) upgradeKafkaJavaEnvFileVersion095(ctx context.Context, attr *common.ServiceAttr, members []*common.ServiceMember, heapSizeMB int64, requuid string) error {
	for _, member := range members {
		var cfg *common.ConfigID
		cfgIndex := -1
		for i, c := range member.Configs {
			if kafkacatalog.IsJvmConfFile(c.FileName) {
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
		newContent := kafkacatalog.UpgradeJavaEnvFileContentToV095(heapSizeMB, memberHost)
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
