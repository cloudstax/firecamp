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
	"github.com/cloudstax/firecamp/catalog/consul"
	"github.com/cloudstax/firecamp/catalog/couchdb"
	"github.com/cloudstax/firecamp/catalog/elasticsearch"
	"github.com/cloudstax/firecamp/catalog/kibana"
	"github.com/cloudstax/firecamp/catalog/logstash"
	"github.com/cloudstax/firecamp/catalog/mongodb"
	"github.com/cloudstax/firecamp/catalog/postgres"
	"github.com/cloudstax/firecamp/catalog/telegraf"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/utils"
)

func (s *ManageHTTPServer) putCatalogServiceOp(ctx context.Context, w http.ResponseWriter,
	r *http.Request, trimURL string, requuid string) (errmsg string, errcode int) {
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
	case catalog.CatalogSetRedisInitOp:
		return s.setRedisInit(ctx, r, requuid)
	case catalog.CatalogScaleCassandraOp:
		return s.scaleCasService(ctx, r, requuid)
	default:
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}
}

func (s *ManageHTTPServer) getCatalogServiceOp(ctx context.Context,
	w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &catalog.CatalogCheckServiceInitRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCheckServiceInitRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("CatalogCheckServiceInitRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	// get service uuid
	service, err := s.dbIns.GetService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("GetService", req.Service.ServiceName, req.CatalogServiceType, "error", err, "requuid", requuid)
		return convertToHTTPError(err)
	}

	// check if the init task is running
	initialized := false
	hasTask, statusMsg := s.catalogSvcInit.hasInitTask(ctx, service.ServiceUUID)
	if hasTask {
		glog.Infoln("The service", req.Service.ServiceName, req.CatalogServiceType,
			"is under initialization, requuid", requuid)
	} else {
		// no init task is running, check if the service is initialized
		glog.Infoln("No init task for service", req.Service.ServiceName, req.CatalogServiceType, "requuid", requuid)

		attr, err := s.dbIns.GetServiceAttr(ctx, service.ServiceUUID)
		if err != nil {
			glog.Errorln("GetServiceAttr error", err, service, "requuid", requuid)
			return convertToHTTPError(err)
		}

		glog.Infoln("service attribute", attr, "requuid", requuid)

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
			switch req.CatalogServiceType {
			case common.CatalogService_MongoDB:
				cfgfile, err := s.getServiceConfigFile(ctx, attr)
				if err != nil {
					glog.Errorln("getServiceConfigFile error", err, "requuid", requuid, attr)
					return convertToHTTPError(err)
				}

				opts, err := mongodbcatalog.ParseServiceConfigs(cfgfile.Spec.Content)
				if err != nil {
					glog.Errorln("ParseServiceConfigs error", err, "requuid", requuid, cfgfile)
					return convertToHTTPError(err)
				}

				s.addMongoDBInitTask(ctx, req.Service, attr.ServiceUUID, opts, requuid)

			case common.CatalogService_PostgreSQL:
				// PG does not require additional init work. set PG initialized
				errmsg, errcode := s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
				if errcode != http.StatusOK {
					return errmsg, errcode
				}
				initialized = true

			case common.CatalogService_Cassandra:
				s.addCasInitTask(ctx, req.Service, attr.ServiceUUID, requuid)

			case common.CatalogService_ZooKeeper:
				// zookeeper does not require additional init work. set initialized
				errmsg, errcode := s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
				if errcode != http.StatusOK {
					return errmsg, errcode
				}
				initialized = true

			case common.CatalogService_Kafka:
				// Kafka does not require additional init work. set initialized
				errmsg, errcode := s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
				if errcode != http.StatusOK {
					return errmsg, errcode
				}
				initialized = true

			case common.CatalogService_KafkaSinkES:
				s.restartKafkaSinkESInitTask(ctx, req.Service, attr, requuid)
				if err != nil {
					glog.Errorln("restartKafkaSinkESInitTask error", err, "requuid", requuid, req.Service)
					return convertToHTTPError(err)
				}

			case common.CatalogService_Redis:
				cfgfile, err := s.getServiceConfigFile(ctx, attr)
				if err != nil {
					glog.Errorln("getServiceConfigFile error", err, "requuid", requuid, attr)
					return convertToHTTPError(err)
				}

				opts, err := mongodbcatalog.ParseServiceConfigs(cfgfile.Spec.Content)
				if err != nil {
					glog.Errorln("ParseServiceConfigs error", err, "requuid", requuid, cfgfile)
					return convertToHTTPError(err)
				}

				err = s.addRedisInitTask(ctx, req.Service, attr.ServiceUUID, opts.Shards, opts.ReplicasPerShard, requuid)
				if err != nil {
					glog.Errorln("addRedisInitTask error", err, "requuid", requuid, req.Service)
					return convertToHTTPError(err)
				}

			case common.CatalogService_CouchDB:
				cfgfile, err := s.getServiceConfigFile(ctx, attr)
				if err != nil {
					glog.Errorln("getServiceConfigFile error", err, "requuid", requuid, attr)
					return convertToHTTPError(err)
				}

				admin, adminPass := couchdbcatalog.GetAdminFromServiceConfigs(cfgfile.Spec.Content)

				s.addCouchDBInitTask(ctx, req.Service, attr.ServiceUUID, attr.Spec.Replicas, admin, adminPass, requuid)

			default:
				return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
			}

		default:
			glog.Errorln("service is not at active or creating status", attr, "requuid", requuid)
			return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
		}
	}

	resp := &catalog.CatalogCheckServiceInitResponse{
		Initialized:   initialized,
		StatusMessage: statusMsg,
	}

	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal error", err, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) createPGService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &catalog.CatalogCreatePostgreSQLRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreatePostgreSQLRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreatePostgreSQLRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	err = pgcatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("invalid request", err, "requuid", requuid, req.Service, req.Options)
		return err.Error(), http.StatusBadRequest
	}

	// create the service in the control plane and the container platform
	crReq := pgcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster, req.Service.ServiceName, req.Resource, req.Options)

	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("created postgresql service", serviceUUID, "requuid", requuid, req.Service)

	// PG does not require additional init work. set PG initialized
	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
}

func (s *ManageHTTPServer) createCouchDBService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &catalog.CatalogCreateCouchDBRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateCouchDBRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateCouchDBRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	err = couchdbcatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("CatalogCreateCouchDBRequest parameters are not valid, requuid", requuid, req)
		return err.Error(), http.StatusBadRequest
	}

	// create the service in the control plane and the container platform
	crReq := couchdbcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Resource, req.Options)

	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	// add the init task
	s.addCouchDBInitTask(ctx, crReq.Service, serviceUUID, req.Options.Replicas, req.Options.Admin, req.Options.AdminPasswd, requuid)

	glog.Infoln("created CouchDB service", serviceUUID, "requuid", requuid, req.Service, req.Options)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) addCouchDBInitTask(ctx context.Context, req *manage.ServiceCommonRequest,
	serviceUUID string, replicas int64, admin string, adminPass string, requuid string) {
	logCfg := s.logIns.CreateStreamLogConfig(ctx, s.cluster, req.ServiceName, serviceUUID, common.TaskTypeInit)
	taskOpts := couchdbcatalog.GenDefaultInitTaskRequest(req, logCfg, s.azs, serviceUUID, replicas, s.manageurl, admin, adminPass)

	task := &serviceTask{
		serviceUUID: serviceUUID,
		serviceName: req.ServiceName,
		serviceType: common.CatalogService_CouchDB,
		opts:        taskOpts,
	}

	s.catalogSvcInit.addInitTask(ctx, task)

	glog.Infoln("add init task for CouchDB service", serviceUUID, "requuid", requuid, req)
}

func (s *ManageHTTPServer) createConsulService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &catalog.CatalogCreateConsulRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateConsulRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateConsulRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	err = consulcatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("CatalogCreateConsulRequest parameters are not valid, requuid", requuid, req)
		return err.Error(), http.StatusBadRequest
	}

	// create the service in the control plane and the container platform
	crReq := consulcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Resource, req.Options)

	// create the service in the control plane
	serviceUUID, err := s.svc.CreateService(ctx, crReq, s.domain, s.vpcID)
	if err != nil {
		glog.Errorln("create service error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("create consul service in the control plane", serviceUUID, "requuid", requuid, req.Service, req.Options)

	serverips, err := s.updateConsulConfigs(ctx, serviceUUID, requuid)
	if err != nil {
		glog.Errorln("updateConsulConfigs error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	err = s.createContainerService(ctx, crReq, serviceUUID, requuid)
	if err != nil {
		glog.Errorln("createContainerService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("created Consul service", serviceUUID, "server ips", serverips, "requuid", requuid, req.Service, req.Options)

	// consul does not require additional init work. set service initialized
	errmsg, errcode = s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
	if len(errmsg) != 0 {
		glog.Errorln("setServiceInitialized error", errcode, errmsg, "requuid", requuid, req.Service, req.Options)
		return errmsg, errcode
	}

	resp := &catalog.CatalogCreateConsulResponse{ConsulServerIPs: serverips}
	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal CatalogCreateConsulResponse error", err, "requuid", requuid, req.Service, req.Options)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) createElasticSearchService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &catalog.CatalogCreateElasticSearchRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateElasticSearchRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateElasticSearchRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	err = escatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("invalid elasticsearch create request", err, "requuid", requuid, req)
		return err.Error(), http.StatusBadRequest
	}

	// create the service in the control plane and the container platform
	crReq := escatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Resource, req.Options)

	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("created elasticsearch service", serviceUUID, "requuid", requuid, req.Service)

	// elasticsearch does not require additional init work. set service initialized
	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
}

func (s *ManageHTTPServer) createKibanaService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &catalog.CatalogCreateKibanaRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateKibanaRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateKibanaRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	err = kibanacatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("invalid kibana create request", err, "requuid", requuid, req.Options)
		return err.Error(), http.StatusBadRequest
	}

	// get the dedicated master nodes of the elasticsearch service
	// get the elasticsearch service uuid
	service, err := s.dbIns.GetService(ctx, s.cluster, req.Options.ESServiceName)
	if err != nil {
		glog.Errorln("get the elasticsearch service", req.Options.ESServiceName, "error", err, "requuid", requuid, req.Options)
		return convertToHTTPError(err)
	}

	// get the elasticsearch service attr
	attr, err := s.dbIns.GetServiceAttr(ctx, service.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, service)
		return convertToHTTPError(err)
	}

	esNodeURI := escatalog.GetFirstMemberURI(attr.Spec.DomainName, attr.Meta.ServiceName)

	// create the service in the control plane and the container platform
	crReq := kibanacatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Resource, req.Options, esNodeURI)

	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("created kibana service", serviceUUID, "requuid", requuid, req.Service)

	// kibana does not require additional init work. set service initialized
	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
}

func (s *ManageHTTPServer) createLogstashService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &catalog.CatalogCreateLogstashRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateLogstashRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateLogstashRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	err = logstashcatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("invalid logstash create request", err, "requuid", requuid, req.Options)
		return err.Error(), http.StatusBadRequest
	}

	// create the service in the control plane and the container platform
	crReq := logstashcatalog.GenDefaultCreateServiceRequest(s.platform, s.region,
		s.azs, s.cluster, req.Service.ServiceName, req.Resource, req.Options)

	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("created logstash service", serviceUUID, "requuid", requuid, req.Service)

	// logstash does not require additional init work. set service initialized
	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
}

func (s *ManageHTTPServer) createTelegrafService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &catalog.CatalogCreateTelegrafRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateTelegrafRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = telcatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("CatalogCreateTelegrafRequest parameters are not valid, requuid", requuid, req)
		return err.Error(), http.StatusBadRequest
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateTelegrafRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	// get the monitor service
	svc, err := s.dbIns.GetService(ctx, s.cluster, req.Options.MonitorServiceName)
	if err != nil {
		glog.Errorln("get service", req.Options.MonitorServiceName, "error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("get service", svc, "requuid", requuid)

	attr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, svc.ServiceUUID, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	members, err := s.dbIns.ListServiceMembers(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("ListServiceMembers error", err, svc.ServiceUUID, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	// create the service in the control plane and the container platform
	crReq := telcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.cluster,
		req.Service.ServiceName, attr, members, req.Options, req.Resource)

	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createContainerService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("created telegraf service", serviceUUID, "to monitor service", req.Options, "requuid", requuid, req.Service)

	// telegraf does not require additional init work. set service initialized
	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
}

func (s *ManageHTTPServer) catalogSetServiceInit(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &catalog.CatalogSetServiceInitRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogSetServiceInitRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Cluster != s.cluster || req.Region != s.region {
		glog.Errorln("CatalogSetServiceInitRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	switch req.ServiceType {
	case common.CatalogService_MongoDB:
		return s.setMongoDBInit(ctx, req, requuid)

	case common.CatalogService_Cassandra:
		glog.Infoln("set cassandra service initialized, requuid", requuid, req)
		return s.setServiceInitialized(ctx, req.ServiceName, requuid)

	case common.CatalogService_CouchDB:
		glog.Infoln("set couchdb service initialized, requuid", requuid, req)
		return s.setServiceInitialized(ctx, req.ServiceName, requuid)

	case common.CatalogService_KafkaSinkES:
		glog.Infoln("set kafka sink elasticsearch service initialized, requuid", requuid, req)
		return s.setServiceInitialized(ctx, req.ServiceName, requuid)

	// other services do not require the init task.
	default:
		glog.Errorln("unknown service type", req)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}
}

func (s *ManageHTTPServer) updateConsulConfigs(ctx context.Context, serviceUUID string, requuid string) (serverips []string, err error) {
	attr, err := s.dbIns.GetServiceAttr(ctx, serviceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "serviceUUID", serviceUUID, "requuid", requuid)
		return nil, err
	}

	// update the consul member address to the assigned static ip in the basic_config.json file
	members, err := s.dbIns.ListServiceMembers(ctx, serviceUUID)
	if err != nil {
		glog.Errorln("ListServiceMembers failed", err, "serviceUUID", serviceUUID, "requuid", requuid)
		return nil, err
	}

	serverips = make([]string, len(members))
	memberips := make(map[string]string)
	for i, m := range members {
		memberdns := dns.GenDNSName(m.MemberName, attr.Spec.DomainName)
		memberips[memberdns] = m.Spec.StaticIP
		serverips[i] = m.Spec.StaticIP

		// update member config with static ip
		find := false
		for cfgIndex, cfg := range m.Spec.Configs {
			if catalog.IsMemberConfigFile(cfg.FileName) {
				cfgfile, err := s.dbIns.GetConfigFile(ctx, attr.ServiceUUID, cfg.FileID)
				if err != nil {
					glog.Errorln("get member config file error", err, cfg, "requuid", requuid, m)
					return nil, err
				}

				newContent := consulcatalog.SetMemberStaticIP(cfgfile.Spec.Content, memberdns, m.Spec.StaticIP)

				_, err = s.updateMemberConfig(ctx, m, cfgfile, cfgIndex, newContent, requuid)
				if err != nil {
					glog.Errorln("updateMemberConfig error", err, cfg, "requuid", requuid, m)
					return nil, err
				}

				find = true
				break
			}
		}
		if !find {
			glog.Errorln("member config file not found, requuid", requuid, m)
			return nil, errors.New("member config file not found")
		}
	}

	// update basic_config.json with static ips
	for i, cfg := range attr.Spec.ServiceConfigs {
		if consulcatalog.IsBasicConfigFile(cfg.FileName) {
			// get the detail content
			cfgfile, err := s.dbIns.GetConfigFile(ctx, serviceUUID, cfg.FileID)
			if err != nil {
				glog.Errorln("GetConfigFile error", err, serviceUUID, "requuid", requuid, cfg)
				return nil, err
			}

			newContent := consulcatalog.UpdateBasicConfigsWithIPs(cfgfile.Spec.Content, memberips)

			err = s.updateServiceConfigFile(ctx, attr, i, cfgfile, newContent, requuid)
			if err != nil {
				glog.Errorln("updateServiceConfigFile error", err, cfgfile, "requuid", requuid, attr.Meta)
				return nil, err
			}

			glog.Infoln("updated basic config, requuid", requuid, attr.Meta)
			return serverips, nil
		}
	}

	errmsg := fmt.Sprintf("consul basic config file not found, requuid %s", requuid)
	glog.Errorln(errmsg, attr)
	return nil, errors.New(errmsg)
}

func (s *ManageHTTPServer) getServiceConfigFile(ctx context.Context, attr *common.ServiceAttr) (*common.ConfigFile, error) {
	for _, cfg := range attr.Spec.ServiceConfigs {
		if catalog.IsServiceConfigFile(cfg.FileName) {
			return s.dbIns.GetConfigFile(ctx, attr.ServiceUUID, cfg.FileID)
		}
	}
	return nil, common.ErrNotFound
}

func (s *ManageHTTPServer) updateMemberConfig(ctx context.Context, member *common.ServiceMember,
	cfgfile *common.ConfigFile, cfgIndex int, newContent string, requuid string) (newMember *common.ServiceMember, err error) {
	// create a new config file
	version, err := utils.GetConfigFileVersion(cfgfile.FileID)
	if err != nil {
		glog.Errorln("GetConfigFileVersion error", err, "requuid", requuid, cfgfile)
		return nil, err
	}

	newFileID := utils.GenConfigFileID(member.MemberName, cfgfile.Meta.FileName, version+1)
	newcfgfile := db.CreateNewConfigFile(cfgfile, newFileID, newContent)

	newcfgfile, err = createConfigFile(ctx, s.dbIns, newcfgfile, requuid)
	if err != nil {
		glog.Errorln("CreateConfigFile error", err, "requuid", requuid, db.PrintConfigFile(newcfgfile), member)
		return nil, err
	}

	glog.Infoln("created new config file, requuid", requuid, db.PrintConfigFile(newcfgfile))

	// update serviceMember to point to the new config file
	newConfigs := db.CopyConfigs(member.Spec.Configs)
	newConfigs[cfgIndex].FileID = newcfgfile.FileID
	newConfigs[cfgIndex].FileMD5 = newcfgfile.Spec.FileMD5

	newMember = db.UpdateServiceMemberConfigs(member, newConfigs)
	err = s.dbIns.UpdateServiceMember(ctx, member, newMember)
	if err != nil {
		glog.Errorln("UpdateServiceMember error", err, "requuid", requuid, member)
		return nil, err
	}

	glog.Infoln("updated member configs in the serviceMember, requuid", requuid, newMember)

	// delete the old config file.
	// TODO add the background gc mechanism to delete the garbage.
	//      the old config file may not be deleted at some conditions.
	//			for example, node crashes right before deleting the config file.
	err = s.dbIns.DeleteConfigFile(ctx, cfgfile.ServiceUUID, cfgfile.FileID)
	if err != nil {
		// simply log an error as this only leaves a garbage
		glog.Errorln("DeleteConfigFile error", err, "requuid", requuid, db.PrintConfigFile(cfgfile))
	} else {
		glog.Infoln("deleted the old config file, requuid", requuid, db.PrintConfigFile(cfgfile))
	}

	return newMember, nil
}
