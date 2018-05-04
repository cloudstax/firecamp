package catalogsvc

import (
	"encoding/json"
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

func (s *CatalogHTTPServer) createKafkaService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
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
	getReq := &manage.ServiceCommonRequest{
		Region:      req.Service.Region,
		Cluster:     req.Service.Cluster,
		ServiceName: req.Options.ZkServiceName,
	}
	zkattr, err := s.managecli.GetServiceAttr(ctx, getReq)
	if err != nil {
		glog.Errorln("get zk service attr error", err, "requuid", requuid, req.Service)
		return err.Error(), http.StatusInternalServerError
	}

	zkservers := catalog.GenServiceMemberHostsWithPort(s.cluster, zkattr.Meta.ServiceName, zkattr.Spec.Replicas, zkcatalog.ClientPort)

	// create the service in the control plane and the container platform
	crReq, jmxUser, jmxPasswd := kafkacatalog.GenDefaultCreateServiceRequest(s.platform,
		s.region, s.azs, s.cluster, req.Service.ServiceName, req.Options, req.Resource, zkservers)

	serviceUUID, err := s.managecli.CreateService(ctx, crReq)
	if err != nil {
		glog.Errorln("CreateService error", err, "requuid", requuid, req.Service)
		return err.Error(), http.StatusInternalServerError
	}

	glog.Infoln("created kafka service", serviceUUID, "requuid", requuid, req.Service)

	// kafka does not require additional init work. set service initialized
	errmsg, errcode = s.managecli.SetServiceInitialized(ctx, req.Service)
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

func (s *CatalogHTTPServer) createKafkaSinkESService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
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
	commonReq := &manage.ServiceCommonRequest{
		Region:      req.Service.Region,
		Cluster:     req.Service.Cluster,
		ServiceName: req.Options.KafkaServiceName,
	}
	kafkaAttr, err := s.managecli.GetServiceAttr(ctx, commonReq)
	if err != nil {
		glog.Errorln("get kafka service attr error", err, "requuid", requuid, req.Service)
		return err.Error(), http.StatusInternalServerError
	}

	// get the elasticsearch service config
	getReq := &manage.GetServiceConfigFileRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      req.Service.Region,
			Cluster:     req.Service.Cluster,
			ServiceName: req.Options.ESServiceName,
		},
		ConfigFileName: catalog.SERVICE_FILE_NAME,
	}
	cfgfile, err := s.managecli.GetServiceConfigFile(ctx, getReq)
	if err != nil {
		glog.Errorln("get elasticsearch service config file error", err, "requuid", requuid, req.Service)
		return err.Error(), http.StatusInternalServerError
	}

	dataNodes, err := escatalog.GetDataNodes(cfgfile.Spec.Content)
	if err != nil {
		glog.Errorln("get elasticsearch data nodes error", err, "requuid", requuid, req.Service)
		return err.Error(), http.StatusInternalServerError
	}

	// create elasticsearch data node uris
	esURIs := catalog.GenServiceMemberURIs(s.cluster, req.Options.ESServiceName, int64(dataNodes), escatalog.HTTPPort)

	// create kafka hosts
	kafkaServers := catalog.GenServiceMemberHostsWithPort(s.cluster, kafkaAttr.Meta.ServiceName, kafkaAttr.Spec.Replicas, kafkacatalog.ListenPort)

	// create the service in the control plane and the container platform
	crReq, sinkESConfigs := kccatalog.GenCreateESSinkServiceRequest(s.platform, s.region, s.cluster,
		req.Service.ServiceName, kafkaServers, esURIs, req)

	serviceUUID, err := s.managecli.CreateService(ctx, crReq)
	if err != nil {
		glog.Errorln("CreateService error", err, "requuid", requuid, req.Service)
		return err.Error(), http.StatusInternalServerError
	}

	glog.Infoln("created kafka connector service", serviceUUID, "requuid", requuid, req.Service)

	// add the init task to actually create the elasticsearch sink task in kafka connectors
	s.addKafkaSinkESInitTask(ctx, req.Service, serviceUUID, req.Options.Replicas, sinkESConfigs, requuid)

	return "", http.StatusOK
}

func (s *CatalogHTTPServer) addKafkaSinkESInitTask(ctx context.Context, req *manage.ServiceCommonRequest,
	serviceUUID string, replicas int64, sinkESConfigs string, requuid string) {
	logCfg := s.logIns.CreateStreamLogConfig(ctx, s.cluster, req.ServiceName, serviceUUID, common.TaskTypeInit)
	taskOpts := kccatalog.GenSinkESServiceInitRequest(req, logCfg, serviceUUID, replicas, s.serverurl, sinkESConfigs)

	task := &serviceTask{
		serviceName: req.ServiceName,
		opts:        taskOpts,
	}

	s.catalogSvcInit.addInitTask(ctx, task)

	glog.Infoln("add init task for service", serviceUUID, "requuid", requuid, req)
}

func (s *CatalogHTTPServer) restartKafkaSinkESInitTask(ctx context.Context, req *manage.ServiceCommonRequest, attr *common.ServiceAttr, requuid string) error {
	getReq := &manage.GetServiceConfigFileRequest{
		Service:        req,
		ConfigFileName: kccatalog.SinkESConfFileName,
	}

	cfgfile, err := s.managecli.GetServiceConfigFile(ctx, getReq)
	if err != nil {
		glog.Errorln("GetConfigFile error", err, "requuid", requuid, req)
		return err
	}

	sinkESConfigs := cfgfile.Spec.Content

	s.addKafkaSinkESInitTask(ctx, req, attr.ServiceUUID, attr.Spec.Replicas, sinkESConfigs, requuid)

	glog.Infoln("addKafkaSinkESInitTask, requuid", requuid, req)
	return nil
}

func (s *CatalogHTTPServer) createKafkaManagerService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
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
	getReq := &manage.ServiceCommonRequest{
		Region:      req.Service.Region,
		Cluster:     req.Service.Cluster,
		ServiceName: req.Options.ZkServiceName,
	}
	zkattr, err := s.managecli.GetServiceAttr(ctx, getReq)
	if err != nil {
		glog.Errorln("get zk service attr error", err, "requuid", requuid, req.Service)
		return err.Error(), http.StatusInternalServerError
	}

	zkservers := catalog.GenServiceMemberHostsWithPort(s.cluster, zkattr.Meta.ServiceName, zkattr.Spec.Replicas, zkcatalog.ClientPort)

	// create the service in the control plane and the container platform
	crReq := kmcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.cluster,
		req.Service.ServiceName, zkservers, req.Options, req.Resource)

	serviceUUID, err := s.managecli.CreateService(ctx, crReq)
	if err != nil {
		glog.Errorln("CreateService error", err, "requuid", requuid, req.Service)
		return err.Error(), http.StatusInternalServerError
	}

	glog.Infoln("created kafka manager service", serviceUUID, "requuid", requuid, req.Service)

	// kafka manager does not require additional init work. set service initialized
	return s.managecli.SetServiceInitialized(ctx, req.Service)
}
