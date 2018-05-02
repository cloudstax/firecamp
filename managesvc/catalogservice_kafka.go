package managesvc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/catalog"
	"github.com/cloudstax/firecamp/api/manage"
	"github.com/cloudstax/firecamp/catalog/elasticsearch"
	"github.com/cloudstax/firecamp/catalog/kafka"
	"github.com/cloudstax/firecamp/catalog/kafkaconnect"
	"github.com/cloudstax/firecamp/catalog/kafkamanager"
	"github.com/cloudstax/firecamp/catalog/zookeeper"
	"github.com/cloudstax/firecamp/common"
)

func (s *ManageHTTPServer) createKafkaService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &catalog.CatalogCreateKafkaRequest{}
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
		return convertToHTTPError(err)
	}

	glog.Infoln("get zk service", zksvc, "requuid", requuid)

	zkattr, err := s.dbIns.GetServiceAttr(ctx, zksvc.ServiceUUID)
	if err != nil {
		glog.Errorln("get zk service attr", zksvc.ServiceUUID, "error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	zkservers := catalog.GenServiceMemberHostsWithPort(s.cluster, zkattr.Meta.ServiceName, zkattr.Spec.Replicas, zkcatalog.ClientPort)

	// create the service in the control plane and the container platform
	crReq, jmxUser, jmxPasswd := kafkacatalog.GenDefaultCreateServiceRequest(s.platform,
		s.region, s.azs, s.cluster, req.Service.ServiceName, req.Options, req.Resource, zkservers)

	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("created kafka service", serviceUUID, "requuid", requuid, req.Service)

	// kafka does not require additional init work. set service initialized
	errmsg, errcode = s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
	if errcode != http.StatusOK {
		return errmsg, errcode
	}

	resp := &catalog.CatalogCreateKafkaResponse{
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
	req := &catalog.CatalogCreateKafkaSinkESRequest{}
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
		return convertToHTTPError(err)
	}

	glog.Infoln("get kafka service", svc, "requuid", requuid)

	kafkaAttr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("get kafka service attr", svc, "error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	// get the elasticsearch service
	svc, err = s.dbIns.GetService(ctx, s.cluster, req.Options.ESServiceName)
	if err != nil {
		glog.Errorln("get elasticsearch service", req.Options.ESServiceName, "error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("get elasticsearch service", svc, "requuid", requuid)

	esAttr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("get elasticsearch service attr", svc, "error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	dataNodes := 0
	for _, cfg := range esAttr.Spec.ServiceConfigs {
		if catalog.IsServiceConfigFile(cfg.FileName) {
			// get the number of data nodes from service.conf file
			cfgfile, err := s.dbIns.GetConfigFile(ctx, esAttr.ServiceUUID, cfg.FileID)
			if err != nil {
				glog.Errorln("get elasticsearch service config file error", err, svc, "requuid", requuid, req.Service)
				return convertToHTTPError(err)
			}
			dataNodes, err = escatalog.GetDataNodes(cfgfile.Spec.Content)
			if err != nil {
				glog.Errorln("get elasticsearch data nodes error", err, svc, "requuid", requuid, req.Service)
				return convertToHTTPError(err)
			}
			break
		}
	}
	if dataNodes == 0 {
		err = fmt.Errorf("elasticsearch service %s has no data node, requuid %s, %s", svc.ServiceName, requuid, req.Service)
		glog.Errorln(err)
		return convertToHTTPError(err)
	}

	// create elasticsearch data node uris
	esURIs := catalog.GenServiceMemberURIs(s.cluster, esAttr.Meta.ServiceName, int64(dataNodes), escatalog.HTTPPort)

	// create kafka hosts
	kafkaServers := catalog.GenServiceMemberHostsWithPort(s.cluster, kafkaAttr.Meta.ServiceName, kafkaAttr.Spec.Replicas, kafkacatalog.ListenPort)

	// create the service in the control plane and the container platform
	crReq, sinkESConfigs := kccatalog.GenCreateESSinkServiceRequest(s.platform, s.region, s.cluster,
		req.Service.ServiceName, kafkaServers, esURIs, req)

	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("created kafka connector service", serviceUUID, "requuid", requuid, req.Service)

	// add the init task to actually create the elasticsearch sink task in kafka connectors
	s.addKafkaSinkESInitTask(ctx, req.Service, serviceUUID, req.Options.Replicas, sinkESConfigs, requuid)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) addKafkaSinkESInitTask(ctx context.Context, req *manage.ServiceCommonRequest,
	serviceUUID string, replicas int64, sinkESConfigs string, requuid string) {
	logCfg := s.logIns.CreateStreamLogConfig(ctx, s.cluster, req.ServiceName, serviceUUID, common.TaskTypeInit)
	taskOpts := kccatalog.GenSinkESServiceInitRequest(req, logCfg, serviceUUID, replicas, s.manageurl, sinkESConfigs)

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
	for _, cfg := range attr.Spec.ServiceConfigs {
		if kccatalog.IsSinkESConfFile(cfg.FileName) {
			cfgfile, err := s.dbIns.GetConfigFile(ctx, attr.ServiceUUID, cfg.FileID)
			if err != nil {
				glog.Errorln("GetConfigFile error", err, cfg, "requuid", requuid, req)
				return err
			}

			sinkESConfigs := cfgfile.Spec.Content

			s.addKafkaSinkESInitTask(ctx, req, attr.ServiceUUID, attr.Spec.Replicas, sinkESConfigs, requuid)

			glog.Infoln("addKafkaSinkESInitTask, requuid", requuid, req)
			return nil
		}
	}

	errmsg := fmt.Sprintf("sinkESConfigs not found, requuid %s, %s", requuid, req)
	glog.Errorln(errmsg)
	return errors.New(errmsg)
}

func (s *ManageHTTPServer) createKafkaManagerService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &catalog.CatalogCreateKafkaManagerRequest{}
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
		return convertToHTTPError(err)
	}

	glog.Infoln("get zk service", zksvc, "requuid", requuid)

	zkattr, err := s.dbIns.GetServiceAttr(ctx, zksvc.ServiceUUID)
	if err != nil {
		glog.Errorln("get zk service attr", zksvc.ServiceUUID, "error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	zkservers := catalog.GenServiceMemberHostsWithPort(s.cluster, zkattr.Meta.ServiceName, zkattr.Spec.Replicas, zkcatalog.ClientPort)

	// create the service in the control plane and the container platform
	crReq := kmcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.cluster,
		req.Service.ServiceName, zkservers, req.Options, req.Resource)

	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createContainerService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("created kafka manager service", serviceUUID, "requuid", requuid, req.Service)

	// kafka manager does not require additional init work. set service initialized
	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
}
