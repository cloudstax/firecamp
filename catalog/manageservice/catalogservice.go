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
	"github.com/jazzl0ver/firecamp/catalog/consul"
	"github.com/jazzl0ver/firecamp/catalog/couchdb"
	"github.com/jazzl0ver/firecamp/catalog/elasticsearch"
	"github.com/jazzl0ver/firecamp/catalog/kibana"
	"github.com/jazzl0ver/firecamp/catalog/logstash"
	"github.com/jazzl0ver/firecamp/catalog/mongodb"
	"github.com/jazzl0ver/firecamp/catalog/postgres"
	"github.com/jazzl0ver/firecamp/catalog/redis"
	"github.com/jazzl0ver/firecamp/catalog/telegraf"
	"github.com/jazzl0ver/firecamp/catalog/zookeeper"
	"github.com/jazzl0ver/firecamp/pkg/dns"
)

func (s *CatalogHTTPServer) putCatalogServiceOp(ctx context.Context, w http.ResponseWriter,
	r *http.Request, trimURL string, requuid string) error {
	switch trimURL {
	case catalog.CatalogCreateMongoDBOp:
		return s.createMongoDBService(ctx, w, r, requuid)
	case catalog.CatalogCreatePostgreSQLOp:
		return s.createPGService(ctx, r, requuid)
	case catalog.CatalogCreateCassandraOp:
		return s.createCasService(ctx, w, r, requuid)
	case catalog.CatalogCreateZooKeeperOp:
		return s.createZkService(ctx, w, r, requuid)
	case catalog.CatalogCreateKafkaOp:
		return s.createKafkaService(ctx, w, r, requuid)
	case catalog.CatalogCreateKafkaSinkESOp:
		return s.createKafkaSinkESService(ctx, w, r, requuid)
	case catalog.CatalogCreateKafkaManagerOp:
		return s.createKafkaManagerService(ctx, r, requuid)
	case catalog.CatalogCreateRedisOp:
		return s.createRedisService(ctx, r, requuid)
	case catalog.CatalogCreateCouchDBOp:
		return s.createCouchDBService(ctx, r, requuid)
	case catalog.CatalogCreateConsulOp:
		return s.createConsulService(ctx, w, r, requuid)
	case catalog.CatalogCreateElasticSearchOp:
		return s.createElasticSearchService(ctx, r, requuid)
	case catalog.CatalogCreateKibanaOp:
		return s.createKibanaService(ctx, r, requuid)
	case catalog.CatalogCreateLogstashOp:
		return s.createLogstashService(ctx, r, requuid)
	case catalog.CatalogCreateTelegrafOp:
		return s.createTelegrafService(ctx, r, requuid)
	case catalog.CatalogSetServiceInitOp:
		return s.catalogSetServiceInit(ctx, r, requuid)
	case catalog.CatalogScaleCassandraOp:
		return s.scaleCasService(ctx, r, requuid)
	default:
		return clienterr.New(http.StatusNotImplemented, "unsupported request")
	}
}

func (s *CatalogHTTPServer) getCatalogServiceOp(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogCheckServiceInitRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCheckServiceInitRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	// check if the init task is running
	initialized := false
	hasTask, statusMsg := s.catalogSvcInit.hasInitTask(ctx, req.Service.ServiceName)
	if hasTask {
		glog.Infoln("The service", req.Service.ServiceName, req.Service.CatalogServiceType,
			"is under initialization, requuid", requuid)
	} else {
		// no init task is running, check if the service is initialized
		glog.Infoln("No init task for service", req.Service.ServiceName, req.Service.CatalogServiceType, "requuid", requuid)

		attr, err := s.managecli.GetServiceAttr(ctx, req.Service)
		if err != nil {
			glog.Errorln("GetServiceAttr error", err, "requuid", requuid, req.Service)
			return err
		}

		glog.Infoln("service attribute", attr, "requuid", requuid)

		getReq := &manage.GetServiceConfigFileRequest{
			Service:        req.Service,
			ConfigFileName: catalog.SERVICE_FILE_NAME,
		}

		switch attr.Meta.ServiceStatus {
		case common.ServiceStatusActive:
			initialized = true

		case common.ServiceStatusInitializing:
			// service is not initialized, and no init task is running.
			// This is possible. For example, the manage service node crashes and all in-memory
			// init tasks will be lost. Currently rely on the customer to query service status
			// to trigger the init task again.
			// TODO scan the pending init catalog service at start.

			// trigger the init task.
			switch req.Service.CatalogServiceType {
			case common.CatalogService_MongoDB:
				cfgfile, err := s.managecli.GetServiceConfigFile(ctx, getReq)
				if err != nil {
					glog.Errorln("getServiceConfigFile error", err, "requuid", requuid, attr)
					return err
				}

				opts, err := mongodbcatalog.ParseServiceConfigs(cfgfile.Spec.Content)
				if err != nil {
					glog.Errorln("ParseServiceConfigs error", err, "requuid", requuid, cfgfile)
					return clienterr.New(http.StatusInternalServerError, err.Error())
				}

				s.addMongoDBInitTask(ctx, req.Service, opts, requuid)

			case common.CatalogService_PostgreSQL:
				// PG does not require additional init work. set PG initialized
				err = s.managecli.SetServiceInitialized(ctx, req.Service)
				if err != nil {
					return err
				}
				initialized = true

			case common.CatalogService_Cassandra:
				s.addCasInitTask(ctx, req.Service, requuid)

			case common.CatalogService_ZooKeeper:
				// zookeeper does not require additional init work. set initialized
				err = s.managecli.SetServiceInitialized(ctx, req.Service)
				if err != nil {
					return err
				}
				initialized = true

			case common.CatalogService_Kafka:
				// Kafka does not require additional init work. set initialized
				err = s.managecli.SetServiceInitialized(ctx, req.Service)
				if err != nil {
					return err
				}
				initialized = true

			case common.CatalogService_KafkaSinkES:
				err = s.restartKafkaSinkESInitTask(ctx, req.Service, attr, requuid)
				if err != nil {
					glog.Errorln("restartKafkaSinkESInitTask error", err, "requuid", requuid, req.Service)
					return err
				}

			case common.CatalogService_Redis:
				cfgfile, err := s.managecli.GetServiceConfigFile(ctx, getReq)
				if err != nil {
					glog.Errorln("getServiceConfigFile error", err, "requuid", requuid, attr)
					return err
				}

				opts, err := mongodbcatalog.ParseServiceConfigs(cfgfile.Spec.Content)
				if err != nil {
					glog.Errorln("ParseServiceConfigs error", err, "requuid", requuid, cfgfile)
					return clienterr.New(http.StatusInternalServerError, err.Error())
				}

				if rediscatalog.IsClusterMode(opts.Shards) {
					s.addRedisInitTask(ctx, req.Service, opts.Shards, opts.ReplicasPerShard, requuid)
				} else {
					// not cluster mode, set redis service initialized
					err = s.managecli.SetServiceInitialized(ctx, req.Service)
					if err != nil {
						return err
					}
					initialized = true
				}

			case common.CatalogService_CouchDB:
				cfgfile, err := s.managecli.GetServiceConfigFile(ctx, getReq)
				if err != nil {
					glog.Errorln("getServiceConfigFile error", err, "requuid", requuid, attr)
					return err
				}

				admin, adminPass := couchdbcatalog.GetAdminFromServiceConfigs(cfgfile.Spec.Content)

				s.addCouchDBInitTask(ctx, req.Service, attr.Spec.Replicas, admin, adminPass, requuid)

			default:
				return clienterr.New(http.StatusBadRequest, http.StatusText(http.StatusBadRequest))
			}

		default:
			glog.Errorln("service is not at active or creating status", attr, "requuid", requuid)
			return clienterr.New(http.StatusBadRequest, http.StatusText(http.StatusBadRequest))
		}
	}

	resp := &catalog.CatalogCheckServiceInitResponse{
		Initialized:   initialized,
		StatusMessage: statusMsg,
	}

	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal error", err, "requuid", requuid, req.Service)
		return clienterr.New(http.StatusInternalServerError, err.Error())
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return nil
}

func (s *CatalogHTTPServer) catalogSetServiceInit(ctx context.Context, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogSetServiceInitRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogSetServiceInitRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	if req.Cluster != s.cluster || req.Region != s.region {
		glog.Errorln("CatalogSetServiceInitRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req)
		return clienterr.New(http.StatusBadRequest, "invalid request")
	}

	initReq := &manage.ServiceCommonRequest{
		Region:             req.Region,
		Cluster:            req.Cluster,
		ServiceName:        req.ServiceName,
		CatalogServiceType: req.ServiceType,
	}

	switch req.ServiceType {
	case common.CatalogService_MongoDB:
		return s.setMongoDBInit(ctx, initReq, requuid)

	case common.CatalogService_Cassandra:
		glog.Infoln("set cassandra service initialized, requuid", requuid, req)
		return s.managecli.SetServiceInitialized(ctx, initReq)

	case common.CatalogService_CouchDB:
		glog.Infoln("set couchdb service initialized, requuid", requuid, req)
		return s.managecli.SetServiceInitialized(ctx, initReq)

	case common.CatalogService_KafkaSinkES:
		glog.Infoln("set kafka sink elasticsearch service initialized, requuid", requuid, req)
		return s.managecli.SetServiceInitialized(ctx, initReq)

	case common.CatalogService_Redis:
		return s.setRedisInit(ctx, initReq, requuid)

	// other services do not require the init task.
	default:
		glog.Errorln("unknown service type", req)
		return clienterr.New(http.StatusBadRequest, "unknown service type")
	}
}

func (s *CatalogHTTPServer) createPGService(ctx context.Context, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogCreatePostgreSQLRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreatePostgreSQLRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreatePostgreSQLRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err
	}

	err = pgcatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("invalid request", err, "requuid", requuid, req.Service, req.Options)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	// create the service in the control plane and the container platform
	crReq := pgcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster, req.Service.ServiceName, req.Resource, req.Options)

	err = s.managecli.CreateService(ctx, crReq)
	if err != nil {
		glog.Errorln("CreateService error", err, "requuid", requuid, req.Service)
		return err
	}

	glog.Infoln("created postgresql service", req.Service, "requuid", requuid)

	// PG does not require additional init work. set PG initialized
	return s.managecli.SetServiceInitialized(ctx, req.Service)
}

func (s *CatalogHTTPServer) createZkService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogCreateZooKeeperRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateZooKeeperRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateZooKeeperRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err
	}

	// create the service in the control plane and the container platform
	crReq, jmxUser, jmxPasswd := zkcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Options, req.Resource)

	err = s.managecli.CreateService(ctx, crReq)
	if err != nil {
		glog.Errorln("CreateService error", err, "requuid", requuid, req.Service)
		return err
	}

	glog.Infoln("created zookeeper service", req.Service, "requuid", requuid)

	// zookeeper does not require additional init work. set service initialized
	err = s.managecli.SetServiceInitialized(ctx, req.Service)
	if err != nil {
		return err
	}

	resp := &catalog.CatalogCreateZooKeeperResponse{
		JmxRemoteUser:   jmxUser,
		JmxRemotePasswd: jmxPasswd,
	}
	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal CatalogCreateZooKeeperResponse error", err, "requuid", requuid, req.Service, req.Options)
		return clienterr.New(http.StatusInternalServerError, err.Error())
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return nil
}

func (s *CatalogHTTPServer) createElasticSearchService(ctx context.Context, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogCreateElasticSearchRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateElasticSearchRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateElasticSearchRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err
	}

	err = escatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("invalid elasticsearch create request", err, "requuid", requuid, req)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	// create the service in the control plane and the container platform
	crReq := escatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Resource, req.Options)

	err = s.managecli.CreateService(ctx, crReq)
	if err != nil {
		glog.Errorln("CreateService error", err, "requuid", requuid, req.Service)
		return err
	}

	glog.Infoln("created elasticsearch service", req.Service, "requuid", requuid)

	// elasticsearch does not require additional init work. set service initialized
	return s.managecli.SetServiceInitialized(ctx, req.Service)
}

func (s *CatalogHTTPServer) createKibanaService(ctx context.Context, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogCreateKibanaRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateKibanaRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateKibanaRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err
	}

	err = kibanacatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("invalid kibana create request", err, "requuid", requuid, req.Options)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	// get the dedicated master nodes of the elasticsearch service
	// get the elasticsearch service uuid
	getESReq := &manage.ServiceCommonRequest{
		Region:             req.Service.Region,
		Cluster:            req.Service.Cluster,
		ServiceName:        req.Options.ESServiceName,
		CatalogServiceType: common.CatalogService_ElasticSearch,
	}
	attr, err := s.managecli.GetServiceAttr(ctx, getESReq)
	if err != nil {
		glog.Errorln("get elasticsearch service error", err, req.Options.ESServiceName, "requuid", requuid)
		return err
	}

	esNodeURI := escatalog.GetFirstMemberURI(attr.Spec.DomainName, attr.Meta.ServiceName)

	// create the service in the control plane and the container platform
	crReq := kibanacatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Resource, req.Options, esNodeURI)

	err = s.managecli.CreateService(ctx, crReq)
	if err != nil {
		glog.Errorln("CreateService error", err, "requuid", requuid, req.Service)
		return err
	}

	glog.Infoln("created kibana service", req.Service, "requuid", requuid)

	// kibana does not require additional init work. set service initialized
	return s.managecli.SetServiceInitialized(ctx, crReq.Service)
}

func (s *CatalogHTTPServer) createLogstashService(ctx context.Context, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogCreateLogstashRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateLogstashRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateLogstashRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err
	}

	err = logstashcatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("invalid logstash create request", err, "requuid", requuid, req.Options)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	// create the service in the control plane and the container platform
	crReq := logstashcatalog.GenDefaultCreateServiceRequest(s.platform, s.region,
		s.azs, s.cluster, req.Service.ServiceName, req.Resource, req.Options)

	err = s.managecli.CreateService(ctx, crReq)
	if err != nil {
		glog.Errorln("CreateService error", err, "requuid", requuid, req.Service)
		return err
	}

	glog.Infoln("created logstash service", req.Service, "requuid", requuid)

	// logstash does not require additional init work. set service initialized
	return s.managecli.SetServiceInitialized(ctx, crReq.Service)
}

func (s *CatalogHTTPServer) createTelegrafService(ctx context.Context, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogCreateTelegrafRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateTelegrafRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = telcatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("CatalogCreateTelegrafRequest parameters are not valid, requuid", requuid, req)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateTelegrafRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err
	}

	// get the monitor service
	mCommonReq := &manage.ServiceCommonRequest{
		Region:             req.Service.Region,
		Cluster:            req.Service.Cluster,
		ServiceName:        req.Options.MonitorServiceName,
		CatalogServiceType: req.Options.MonitorServiceType,
	}
	attr, err := s.managecli.GetServiceAttr(ctx, mCommonReq)
	if err != nil {
		glog.Errorln("get monitor target service error", err, req.Options.MonitorServiceName, "requuid", requuid, req.Service)
		return err
	}

	listReq := &manage.ListServiceMemberRequest{
		Service: mCommonReq,
	}
	members, err := s.managecli.ListServiceMember(ctx, listReq)
	if err != nil {
		glog.Errorln("ListServiceMembers error", err, "requuid", requuid, req.Service)
		return err
	}

	// create the service in the control plane and the container platform
	crReq := telcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.cluster,
		req.Service.ServiceName, attr, members, req.Options, req.Resource)

	err = s.managecli.CreateService(ctx, crReq)
	if err != nil {
		glog.Errorln("CreateService error", err, "requuid", requuid, req.Service)
		return err
	}

	glog.Infoln("created telegraf service", req.Service, "to monitor service", req.Options, "requuid", requuid)

	// telegraf does not require additional init work. set service initialized
	return s.managecli.SetServiceInitialized(ctx, crReq.Service)
}

func (s *CatalogHTTPServer) createCouchDBService(ctx context.Context, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogCreateCouchDBRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateCouchDBRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateCouchDBRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err
	}

	err = couchdbcatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("CatalogCreateCouchDBRequest parameters are not valid, requuid", requuid, req)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	// create the service in the control plane and the container platform
	crReq := couchdbcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Resource, req.Options)

	err = s.managecli.CreateService(ctx, crReq)
	if err != nil {
		glog.Errorln("CreateService error", err, "requuid", requuid, req.Service)
		return err
	}

	// add the init task
	s.addCouchDBInitTask(ctx, crReq.Service, req.Options.Replicas, req.Options.Admin, req.Options.AdminPasswd, requuid)

	glog.Infoln("created CouchDB service", req.Service, "requuid", requuid)

	return err
}

func (s *CatalogHTTPServer) addCouchDBInitTask(ctx context.Context, req *manage.ServiceCommonRequest,
	replicas int64, admin string, adminPass string, requuid string) {
	taskReq := couchdbcatalog.GenDefaultInitTaskRequest(req, s.azs, replicas, s.serverurl, admin, adminPass)

	s.catalogSvcInit.addInitTask(ctx, taskReq)

	glog.Infoln("add init task for CouchDB service", req, "requuid", requuid)
}

func (s *CatalogHTTPServer) createConsulService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogCreateConsulRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateConsulRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateConsulRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err
	}

	err = consulcatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("CatalogCreateConsulRequest parameters are not valid, requuid", requuid, req)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	// create the service in the control plane and the container platform
	crReq := consulcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Resource, req.Options)

	// create the service in the control plane
	err = s.managecli.CreateManageService(ctx, crReq)
	if err != nil {
		glog.Errorln("create service error", err, "requuid", requuid, req.Service)
		return err
	}

	glog.Infoln("create consul service in the control plane", req.Service, "requuid", requuid, req.Options)

	serverips, err := s.updateConsulConfigs(ctx, crReq.Service, requuid)
	if err != nil {
		glog.Errorln("updateConsulConfigs error", err, "requuid", requuid, req.Service)
		return err
	}

	err = s.managecli.CreateContainerService(ctx, crReq)
	if err != nil {
		glog.Errorln("createContainerService error", err, "requuid", requuid, req.Service)
		return err
	}

	glog.Infoln("created Consul service", req.Service, "server ips", serverips, "requuid", requuid, req.Options)

	// consul does not require additional init work. set service initialized
	err = s.managecli.SetServiceInitialized(ctx, req.Service)
	if err != nil {
		glog.Errorln("SetServiceInitialized error", err, "requuid", requuid, req.Service, req.Options)
		return err
	}

	resp := &catalog.CatalogCreateConsulResponse{ConsulServerIPs: serverips}
	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal CatalogCreateConsulResponse error", err, "requuid", requuid, req.Service, req.Options)
		return clienterr.New(http.StatusInternalServerError, err.Error())
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return nil
}

func (s *CatalogHTTPServer) updateConsulConfigs(ctx context.Context, req *manage.ServiceCommonRequest, requuid string) (serverips []string, err error) {
	// update the member config with member static ip
	listReq := &manage.ListServiceMemberRequest{
		Service: req,
	}
	members, err := s.managecli.ListServiceMember(ctx, listReq)
	if err != nil {
		glog.Errorln("ListServiceMembers failed", err, "requuid", requuid, req)
		return nil, err
	}

	domain := dns.GenDefaultDomainName(req.Cluster)

	serverips = make([]string, len(members))
	memberips := make(map[string]string)
	for i, m := range members {
		memberdns := dns.GenDNSName(m.MemberName, domain)
		memberips[memberdns] = m.Spec.StaticIP
		serverips[i] = m.Spec.StaticIP

		getReq := &manage.GetMemberConfigFileRequest{
			Service:        req,
			MemberName:     m.MemberName,
			ConfigFileName: catalog.MEMBER_FILE_NAME,
		}
		cfgfile, err := s.managecli.GetMemberConfigFile(ctx, getReq)
		if err != nil {
			glog.Errorln("get member config file error", err, "requuid", requuid, getReq)
			return nil, err
		}

		newContent := consulcatalog.SetMemberStaticIP(cfgfile.Spec.Content, memberdns, m.Spec.StaticIP)

		updateReq := &manage.UpdateMemberConfigRequest{
			Service:           req,
			MemberName:        m.MemberName,
			ConfigFileName:    catalog.MEMBER_FILE_NAME,
			ConfigFileContent: newContent,
		}
		err = s.managecli.UpdateMemberConfig(ctx, updateReq)
		if err != nil {
			glog.Errorln("updateMemberConfig error", err, "requuid", requuid, getReq)
			return nil, err
		}

		glog.Infoln("update member", m.MemberName, "config with ip", m.Spec.StaticIP, "requuid", requuid)
	}

	// update the consul member address to the assigned static ip in the basic_config.json file
	getReq := &manage.GetServiceConfigFileRequest{
		Service:        req,
		ConfigFileName: consulcatalog.BasicConfFileName,
	}
	cfgfile, err := s.managecli.GetServiceConfigFile(ctx, getReq)
	if err != nil {
		glog.Errorln("GetConfigFile error", err, "requuid", requuid, getReq)
		return nil, err
	}

	newContent := consulcatalog.UpdateBasicConfigsWithIPs(cfgfile.Spec.Content, memberips)

	updateReq := &manage.UpdateServiceConfigRequest{
		Service:           req,
		ConfigFileName:    consulcatalog.BasicConfFileName,
		ConfigFileContent: newContent,
	}
	err = s.managecli.UpdateServiceConfig(ctx, updateReq)
	if err != nil {
		glog.Errorln("updateServiceConfigFile error", err, "requuid", requuid, getReq)
		return nil, err
	}

	glog.Infoln("updated basic config, requuid", requuid, req)
	return serverips, nil
}
