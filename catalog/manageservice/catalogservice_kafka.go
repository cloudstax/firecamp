package catalogsvc

import (
	"encoding/json"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/jazzl0ver/firecamp/api/catalog"
	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/jazzl0ver/firecamp/api/manage"
	"github.com/jazzl0ver/firecamp/api/manage/error"
	"github.com/jazzl0ver/firecamp/catalog/elasticsearch"
	"github.com/jazzl0ver/firecamp/catalog/kafka"
	"github.com/jazzl0ver/firecamp/catalog/kafkaconnect"
	"github.com/jazzl0ver/firecamp/catalog/kafkamanager"
	"github.com/jazzl0ver/firecamp/catalog/zookeeper"
)

func (s *CatalogHTTPServer) createKafkaService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogCreateKafkaRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err
	}

	// get the zk service
	getReq := &manage.ServiceCommonRequest{
		Region:             req.Service.Region,
		Cluster:            req.Service.Cluster,
		ServiceName:        req.Options.ZkServiceName,
		CatalogServiceType: common.CatalogService_ZooKeeper,
	}
	zkattr, err := s.managecli.GetServiceAttr(ctx, getReq)
	if err != nil {
		glog.Errorln("get zk service attr error", err, "requuid", requuid, req.Service)
		return err
	}

	zkservers := catalog.GenServiceMemberHostsWithPort(s.cluster, zkattr.Meta.ServiceName, zkattr.Spec.Replicas, zkcatalog.ClientPort)

	// create the service in the control plane and the container platform
	crReq, jmxUser, jmxPasswd := kafkacatalog.GenDefaultCreateServiceRequest(s.platform,
		s.region, s.azs, s.cluster, req.Service.ServiceName, req.Options, req.Resource, zkservers)

	err = s.managecli.CreateService(ctx, crReq)
	if err != nil {
		glog.Errorln("CreateService error", err, "requuid", requuid, req.Service)
		return err
	}

	glog.Infoln("created kafka service", req.Service, "requuid", requuid)

	// kafka does not require additional init work. set service initialized
	err = s.managecli.SetServiceInitialized(ctx, req.Service)
	if err != nil {
		return err
	}

	resp := &catalog.CatalogCreateKafkaResponse{
		JmxRemoteUser:   jmxUser,
		JmxRemotePasswd: jmxPasswd,
	}
	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal CatalogCreateKafkaResponse error", err, "requuid", requuid, req.Service, req.Options)
		return clienterr.New(http.StatusInternalServerError, err.Error())
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return nil
}

func (s *CatalogHTTPServer) createKafkaSinkESService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogCreateKafkaSinkESRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaSinkESRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaSinkESRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err
	}

	err = kccatalog.ValidateSinkESRequest(req)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaSinkESRequest invalid request", err, "requuid", requuid, req.Service)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	// get the kafka service
	commonReq := &manage.ServiceCommonRequest{
		Region:             req.Service.Region,
		Cluster:            req.Service.Cluster,
		ServiceName:        req.Options.KafkaServiceName,
		CatalogServiceType: common.CatalogService_Kafka,
	}
	kafkaAttr, err := s.managecli.GetServiceAttr(ctx, commonReq)
	if err != nil {
		glog.Errorln("get kafka service attr error", err, "requuid", requuid, req.Service)
		return err
	}

	// get the elasticsearch service config
	getReq := &manage.GetServiceConfigFileRequest{
		Service: &manage.ServiceCommonRequest{
			Region:             req.Service.Region,
			Cluster:            req.Service.Cluster,
			ServiceName:        req.Options.ESServiceName,
			CatalogServiceType: common.CatalogService_ElasticSearch,
		},
		ConfigFileName: catalog.SERVICE_FILE_NAME,
	}
	cfgfile, err := s.managecli.GetServiceConfigFile(ctx, getReq)
	if err != nil {
		glog.Errorln("get elasticsearch service config file error", err, "requuid", requuid, req.Service)
		return err
	}

	dataNodes, err := escatalog.GetDataNodes(cfgfile.Spec.Content)
	if err != nil {
		glog.Errorln("get elasticsearch data nodes error", err, "requuid", requuid, req.Service)
		return clienterr.New(http.StatusInternalServerError, err.Error())
	}

	// create elasticsearch data node uris
	esURIs := catalog.GenServiceMemberURIs(s.cluster, req.Options.ESServiceName, int64(dataNodes), escatalog.HTTPPort)

	// create kafka hosts
	kafkaServers := catalog.GenServiceMemberHostsWithPort(s.cluster, kafkaAttr.Meta.ServiceName, kafkaAttr.Spec.Replicas, kafkacatalog.ListenPort)

	// create the service in the control plane and the container platform
	crReq, sinkESConfigs := kccatalog.GenCreateESSinkServiceRequest(s.platform, s.region, s.cluster,
		req.Service.ServiceName, kafkaServers, esURIs, req)

	err = s.managecli.CreateService(ctx, crReq)
	if err != nil {
		glog.Errorln("CreateService error", err, "requuid", requuid, req.Service)
		return err
	}

	glog.Infoln("created kafka connector service", req.Service, "requuid", requuid)

	// add the init task to actually create the elasticsearch sink task in kafka connectors
	s.addKafkaSinkESInitTask(ctx, req.Service, req.Options.Replicas, sinkESConfigs, requuid)

	return nil
}

func (s *CatalogHTTPServer) addKafkaSinkESInitTask(ctx context.Context, req *manage.ServiceCommonRequest,
	replicas int64, sinkESConfigs string, requuid string) {
	taskReq := kccatalog.GenSinkESServiceInitRequest(req, replicas, s.serverurl, sinkESConfigs)

	s.catalogSvcInit.addInitTask(ctx, taskReq)

	glog.Infoln("add init task for service", req, "requuid", requuid)
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

	s.addKafkaSinkESInitTask(ctx, req, attr.Spec.Replicas, sinkESConfigs, requuid)

	glog.Infoln("addKafkaSinkESInitTask, requuid", requuid, req)
	return nil
}

func (s *CatalogHTTPServer) createKafkaManagerService(ctx context.Context, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogCreateKafkaManagerRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaManagerRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = kmcatalog.ValidateRequest(req.Options)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaManagerRequest parameters are not valid, requuid", requuid, req)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaManagerRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err
	}

	// get the zk service
	getReq := &manage.ServiceCommonRequest{
		Region:             req.Service.Region,
		Cluster:            req.Service.Cluster,
		ServiceName:        req.Options.ZkServiceName,
		CatalogServiceType: common.CatalogService_ZooKeeper,
	}
	zkattr, err := s.managecli.GetServiceAttr(ctx, getReq)
	if err != nil {
		glog.Errorln("get zk service attr error", err, "requuid", requuid, req.Service)
		return err
	}

	zkservers := catalog.GenServiceMemberHostsWithPort(s.cluster, zkattr.Meta.ServiceName, zkattr.Spec.Replicas, zkcatalog.ClientPort)

	// create the service in the control plane and the container platform
	crReq := kmcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.cluster,
		req.Service.ServiceName, zkservers, req.Options, req.Resource)

	err = s.managecli.CreateService(ctx, crReq)
	if err != nil {
		glog.Errorln("CreateService error", err, "requuid", requuid, req.Service)
		return err
	}

	glog.Infoln("created kafka manager service", req.Service, "requuid", requuid)

	// kafka manager does not require additional init work. set service initialized
	return s.managecli.SetServiceInitialized(ctx, req.Service)
}
