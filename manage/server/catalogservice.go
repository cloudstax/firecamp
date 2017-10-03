package manageserver

import (
	"encoding/json"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/catalog/cassandra"
	"github.com/cloudstax/firecamp/catalog/couchdb"
	"github.com/cloudstax/firecamp/catalog/kafka"
	"github.com/cloudstax/firecamp/catalog/mongodb"
	"github.com/cloudstax/firecamp/catalog/postgres"
	"github.com/cloudstax/firecamp/catalog/redis"
	"github.com/cloudstax/firecamp/catalog/zookeeper"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

func (s *ManageHTTPServer) putCatalogServiceOp(ctx context.Context, w http.ResponseWriter,
	r *http.Request, trimURL string, requuid string) (errmsg string, errcode int) {
	switch trimURL {
	case manage.CatalogCreateMongoDBOp:
		return s.createMongoDBService(ctx, r, requuid)
	case manage.CatalogCreatePostgreSQLOp:
		return s.createPGService(ctx, r, requuid)
	case manage.CatalogCreateCassandraOp:
		return s.createCasService(ctx, r, requuid)
	case manage.CatalogCreateZooKeeperOp:
		return s.createZkService(ctx, r, requuid)
	case manage.CatalogCreateKafkaOp:
		return s.createKafkaService(ctx, r, requuid)
	case manage.CatalogCreateRedisOp:
		return s.createRedisService(ctx, r, requuid)
	case manage.CatalogCreateCouchDBOp:
		return s.createCouchDBService(ctx, r, requuid)
	case manage.CatalogSetServiceInitOp:
		return s.catalogSetServiceInit(ctx, r, requuid)
	case manage.CatalogSetRedisInitOp:
		return s.setRedisInit(ctx, r, requuid)
	default:
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}
}

func (s *ManageHTTPServer) getCatalogServiceOp(ctx context.Context,
	w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCheckServiceInitRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCheckServiceInitRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Service.Cluster != s.cluster || req.Service.Region != s.region {
		glog.Errorln("CatalogCheckServiceInitRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	// get service uuid
	service, err := s.dbIns.GetService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("GetService", req.Service.ServiceName, req.ServiceType, "error", err, "requuid", requuid)
		return manage.ConvertToHTTPError(err)
	}

	// check if the init task is running
	initialized := false
	hasTask, statusMsg := s.catalogSvcInit.hasInitTask(ctx, service.ServiceUUID)
	if hasTask {
		glog.Infoln("The service", req.Service.ServiceName, req.ServiceType,
			"is under initialization, requuid", requuid)
	} else {
		// no init task is running, check if the service is initialized
		glog.Infoln("No init task for service", req.Service.ServiceName, req.ServiceType, "requuid", requuid)

		attr, err := s.dbIns.GetServiceAttr(ctx, service.ServiceUUID)
		if err != nil {
			glog.Errorln("GetServiceAttr error", err, service, "requuid", requuid)
			return manage.ConvertToHTTPError(err)
		}

		glog.Infoln("service attribute", attr, "requuid", requuid)

		switch attr.ServiceStatus {
		case common.ServiceStatusActive:
			initialized = true

		case common.ServiceStatusInitializing:
			// service is not initialized, and no init task is running.
			// This is possible. For example, the manage service node crashes and all in-memory
			// init tasks will be lost. Currently rely on the customer to query service status
			// to trigger the init task again.
			// TODO scan the pending init catalog service at start.

			// trigger the init task.
			switch req.ServiceType {
			case catalog.CatalogService_MongoDB:
				s.addMongoDBInitTask(ctx, req.Service, attr.ServiceUUID, attr.Replicas, req.Admin, req.AdminPasswd, requuid)

			case catalog.CatalogService_PostgreSQL:
				// PG does not require additional init work. set PG initialized
				errmsg, errcode := s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
				if errcode != http.StatusOK {
					return errmsg, errcode
				}
				initialized = true

			case catalog.CatalogService_Cassandra:
				s.addCasInitTask(ctx, req.Service, attr.ServiceUUID, requuid)

			case catalog.CatalogService_ZooKeeper:
				// zookeeper does not require additional init work. set initialized
				errmsg, errcode := s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
				if errcode != http.StatusOK {
					return errmsg, errcode
				}
				initialized = true

			case catalog.CatalogService_Kafka:
				// Kafka does not require additional init work. set initialized
				errmsg, errcode := s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
				if errcode != http.StatusOK {
					return errmsg, errcode
				}
				initialized = true

			case catalog.CatalogService_Redis:
				err = s.addRedisInitTask(ctx, req.Service, attr.ServiceUUID, req.Shards, req.ReplicasPerShard, req.AdminPasswd, requuid)
				if err != nil {
					glog.Errorln("addRedisInitTask error", err, "requuid", requuid, req.Service)
					return manage.ConvertToHTTPError(err)
				}

			case catalog.CatalogService_CouchDB:
				s.addCouchDBInitTask(ctx, req.Service, attr.ServiceUUID, attr.Replicas, req.Admin, req.AdminPasswd, requuid)

			default:
				return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
			}

		default:
			glog.Errorln("service is not at active or creating status", attr, "requuid", requuid)
			return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
		}
	}

	resp := &manage.CatalogCheckServiceInitResponse{
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

func (s *ManageHTTPServer) createMongoDBService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateMongoDBRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateMongoDBRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Service.Cluster != s.cluster || req.Service.Region != s.region {
		glog.Errorln("CatalogCreateMongoDBRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	// create the service in the control plane and the container platform
	crReq, err := mongodbcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Replicas, req.VolumeSizeGB, req.Resource)
	if err != nil {
		glog.Errorln("mongodbcatalog GenDefaultCreateServiceRequest error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("MongoDBService is created, add the init task, requuid", requuid, req.Service, req.Admin)

	// run the init task in the background
	s.addMongoDBInitTask(ctx, crReq.Service, serviceUUID, req.Replicas, req.Admin, req.AdminPasswd, requuid)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) addMongoDBInitTask(ctx context.Context, req *manage.ServiceCommonRequest,
	serviceUUID string, replicas int64, admin string, adminPasswd string, requuid string) {
	logCfg := s.logIns.CreateLogConfigForStream(ctx, s.cluster, req.ServiceName, serviceUUID, common.TaskTypeInit)
	taskOpts := mongodbcatalog.GenDefaultInitTaskRequest(req, logCfg, serviceUUID, replicas, s.manageurl, admin, adminPasswd)

	task := &serviceTask{
		serviceUUID: serviceUUID,
		serviceName: req.ServiceName,
		serviceType: catalog.CatalogService_MongoDB,
		opts:        taskOpts,
	}

	s.catalogSvcInit.addInitTask(ctx, task)

	glog.Infoln("add init task for service", serviceUUID, "requuid", requuid, req)
}

func (s *ManageHTTPServer) createPGService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreatePostgreSQLRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreatePostgreSQLRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Service.Cluster != s.cluster || req.Service.Region != s.region {
		glog.Errorln("CatalogCreatePostgreSQLRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	// create the service in the control plane and the container platform
	crReq := pgcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster, req.Service.ServiceName,
		req.Replicas, req.VolumeSizeGB, req.AdminPasswd, req.ReplUser, req.ReplUserPasswd, req.Resource)
	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("created postgresql service", serviceUUID, "requuid", requuid, req.Service)

	// PG does not require additional init work. set PG initialized
	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
}

func (s *ManageHTTPServer) createZkService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateZooKeeperRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateZooKeeperRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Service.Cluster != s.cluster || req.Service.Region != s.region {
		glog.Errorln("CatalogCreateZooKeeperRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	// create the service in the control plane and the container platform
	crReq := zkcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Replicas, req.VolumeSizeGB, req.Resource)
	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("created zookeeper service", serviceUUID, "requuid", requuid, req.Service)

	// zookeeper does not require additional init work. set service initialized
	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
}

func (s *ManageHTTPServer) createKafkaService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateKafkaRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateKafkaRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Service.Cluster != s.cluster || req.Service.Region != s.region {
		glog.Errorln("CatalogCreateKafkaRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	// get the zk service
	zksvc, err := s.dbIns.GetService(ctx, s.cluster, req.ZkServiceName)
	if err != nil {
		glog.Errorln("get zk service", req.ZkServiceName, "error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("get zk service", zksvc, "requuid", requuid)

	zkattr, err := s.dbIns.GetServiceAttr(ctx, zksvc.ServiceUUID)
	if err != nil {
		glog.Errorln("get zk service attr", zksvc.ServiceUUID, "error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	// create the service in the control plane and the container platform
	crReq := kafkacatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Replicas, req.VolumeSizeGB, req.Resource,
		req.AllowTopicDel, req.RetentionHours, zkattr)
	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("created kafka service", serviceUUID, "requuid", requuid, req.Service)

	// kafka does not require additional init work. set service initialized
	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
}

func (s *ManageHTTPServer) createRedisService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateRedisRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateRedisRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Service.Cluster != s.cluster || req.Service.Region != s.region {
		glog.Errorln("CatalogCreateRedisRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = rediscatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("CatalogCreateRedisRequest parameters are not valid, requuid", requuid, req)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	// create the service in the control plane and the container platform
	crReq := rediscatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Resource, req.Options)
	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	if rediscatalog.IsClusterMode(req.Options.Shards) {
		glog.Infoln("The cluster mode Redis is created, add the init task, requuid", requuid, req.Service, req.Options)

		// for Redis cluster mode, run the init task in the background
		err = s.addRedisInitTask(ctx, crReq.Service, serviceUUID, req.Options.Shards, req.Options.ReplicasPerShard, req.Options.AuthPass, requuid)
		if err != nil {
			glog.Errorln("addRedisInitTask error", err, "requuid", requuid, req.Service)
			return manage.ConvertToHTTPError(err)
		}

		return "", http.StatusOK
	}

	// redis single instance or master-slave mode does not require additional init work. set service initialized
	glog.Infoln("created Redis service", serviceUUID, "requuid", requuid, req.Service, req.Options)

	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
}

func (s *ManageHTTPServer) addRedisInitTask(ctx context.Context, req *manage.ServiceCommonRequest,
	serviceUUID string, shards int64, replicasPerShard int64, authPass string, requuid string) error {
	enableAuth := false
	if len(authPass) != 0 {
		enableAuth = true
	}

	logCfg := s.logIns.CreateLogConfigForStream(ctx, s.cluster, req.ServiceName, serviceUUID, common.TaskTypeInit)

	taskOpts, err := rediscatalog.GenDefaultInitTaskRequest(req, logCfg, shards, replicasPerShard, enableAuth, serviceUUID, s.manageurl)
	if err != nil {
		return err
	}

	task := &serviceTask{
		serviceUUID: serviceUUID,
		serviceName: req.ServiceName,
		serviceType: catalog.CatalogService_Redis,
		opts:        taskOpts,
	}

	s.catalogSvcInit.addInitTask(ctx, task)

	glog.Infoln("add init task for Redis service", serviceUUID, "requuid", requuid, req)
	return nil
}

func (s *ManageHTTPServer) createCouchDBService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateCouchDBRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateCouchDBRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Service.Cluster != s.cluster || req.Service.Region != s.region {
		glog.Errorln("CatalogCreateCouchDBRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = couchdbcatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("CatalogCreateCouchDBRequest parameters are not valid, requuid", requuid, req)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	// create the service in the control plane and the container platform
	crReq := couchdbcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Resource, req.Options)
	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	// add the init task
	s.addCouchDBInitTask(ctx, crReq.Service, serviceUUID, req.Options.Replicas, req.Options.Admin, req.Options.AdminPasswd, requuid)

	glog.Infoln("created CouchDB service", serviceUUID, "requuid", requuid, req.Service, req.Options)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) addCouchDBInitTask(ctx context.Context, req *manage.ServiceCommonRequest,
	serviceUUID string, replicas int64, admin string, adminPass string, requuid string) {
	logCfg := s.logIns.CreateLogConfigForStream(ctx, s.cluster, req.ServiceName, serviceUUID, common.TaskTypeInit)
	taskOpts := couchdbcatalog.GenDefaultInitTaskRequest(req, logCfg, s.azs, serviceUUID, replicas, s.manageurl, admin, adminPass)

	task := &serviceTask{
		serviceUUID: serviceUUID,
		serviceName: req.ServiceName,
		serviceType: catalog.CatalogService_CouchDB,
		opts:        taskOpts,
	}

	s.catalogSvcInit.addInitTask(ctx, task)

	glog.Infoln("add init task for CouchDB service", serviceUUID, "requuid", requuid, req)
}

func (s *ManageHTTPServer) createCasService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateCassandraRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateCassandraRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Service.Cluster != s.cluster || req.Service.Region != s.region {
		glog.Errorln("CatalogCreateCassandraRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	// create the service in the control plane and the container platform
	crReq := cascatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs,
		s.cluster, req.Service.ServiceName, req.Replicas, req.VolumeSizeGB, req.Resource)
	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("Cassandra is created, add the init task, requuid", requuid, req.Service)

	// run the init task in the background
	s.addCasInitTask(ctx, crReq.Service, serviceUUID, requuid)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) addCasInitTask(ctx context.Context,
	req *manage.ServiceCommonRequest, serviceUUID string, requuid string) {
	logCfg := s.logIns.CreateLogConfigForStream(ctx, s.cluster, req.ServiceName, serviceUUID, common.TaskTypeInit)
	taskOpts := cascatalog.GenDefaultInitTaskRequest(req, logCfg, serviceUUID, s.manageurl)

	task := &serviceTask{
		serviceUUID: serviceUUID,
		serviceName: req.ServiceName,
		serviceType: catalog.CatalogService_Cassandra,
		opts:        taskOpts,
	}

	s.catalogSvcInit.addInitTask(ctx, task)

	glog.Infoln("add init task for service", serviceUUID, "requuid", requuid, req)
}

func (s *ManageHTTPServer) catalogSetServiceInit(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogSetServiceInitRequest{}
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
	case catalog.CatalogService_MongoDB:
		return s.setMongoDBInit(ctx, req, requuid)

	case catalog.CatalogService_Cassandra:
		glog.Infoln("set cassandra service initialized, requuid", requuid, req)
		return s.setServiceInitialized(ctx, req.ServiceName, requuid)

	case catalog.CatalogService_CouchDB:
		glog.Infoln("set couchdb service initialized, requuid", requuid, req)
		return s.setServiceInitialized(ctx, req.ServiceName, requuid)

	// other services do not require the init task.
	default:
		glog.Errorln("unknown service type", req)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}
}

func (s *ManageHTTPServer) setMongoDBInit(ctx context.Context, req *manage.CatalogSetServiceInitRequest, requuid string) (errmsg string, errcode int) {
	// get service uuid
	service, err := s.dbIns.GetService(ctx, s.cluster, req.ServiceName)
	if err != nil {
		glog.Errorln("GetService", req.ServiceName, req.ServiceType, "error", err, "requuid", requuid)
		return manage.ConvertToHTTPError(err)
	}

	// get service attr
	attr, err := s.dbIns.GetServiceAttr(ctx, service.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, service)
		return manage.ConvertToHTTPError(err)
	}

	// list all serviceMembers
	members, err := s.dbIns.ListServiceMembers(ctx, service.ServiceUUID)
	if err != nil {
		glog.Errorln("ListServiceMembers failed", err, "serviceUUID", service.ServiceUUID, "requuid", requuid)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("get service", service, "has", len(members), "replicas, requuid", requuid)

	// update the init task status message
	statusMsg := "enable auth for MongoDB"
	s.catalogSvcInit.UpdateTaskStatusMsg(service.ServiceUUID, statusMsg)

	// update the replica (serviceMember) mongod.conf file
	for _, member := range members {
		for i, cfg := range member.Configs {
			if !mongodbcatalog.IsMongoDBConfFile(cfg.FileName) {
				glog.V(5).Infoln("not mongod.conf file, skip the config, requuid", requuid, cfg)
				continue
			}

			glog.Infoln("enable auth on mongod.conf, requuid", requuid, cfg)

			err = s.enableMongoDBAuth(ctx, cfg, i, member, requuid)
			if err != nil {
				glog.Errorln("enableMongoDBAuth error", err, "requuid", requuid, cfg, member)
				return manage.ConvertToHTTPError(err)
			}

			glog.Infoln("enabled auth for replia, requuid", requuid, member)
			break
		}
	}

	// the config files of all replicas are updated, restart all containers
	glog.Infoln("all replicas are updated, restart all containers, requuid", requuid, req)

	// update the init task status message
	statusMsg = "restarting all MongoDB containers"
	s.catalogSvcInit.UpdateTaskStatusMsg(service.ServiceUUID, statusMsg)

	err = s.containersvcIns.RestartService(ctx, s.cluster, req.ServiceName, attr.Replicas)
	if err != nil {
		glog.Errorln("RestartService error", err, "requuid", requuid, req)
		return manage.ConvertToHTTPError(err)
	}

	// set service initialized
	glog.Infoln("all containers restarted, set service initialized, requuid", requuid, req)

	return s.setServiceInitialized(ctx, req.ServiceName, requuid)
}

func (s *ManageHTTPServer) enableMongoDBAuth(ctx context.Context,
	cfg *common.MemberConfig, cfgIndex int, member *common.ServiceMember, requuid string) error {
	// fetch the config file
	cfgfile, err := s.dbIns.GetConfigFile(ctx, member.ServiceUUID, cfg.FileID)
	if err != nil {
		glog.Errorln("GetConfigFile error", err, "requuid", requuid, cfg, member)
		return err
	}

	// if auth is enabled, return
	if mongodbcatalog.IsAuthEnabled(cfgfile.Content) {
		glog.Infoln("auth is already enabled in the config file", db.PrintConfigFile(cfgfile), "requuid", requuid, member)
		return nil
	}

	// auth is not enabled, enable it
	newContent := mongodbcatalog.EnableMongoDBAuth(cfgfile.Content)

	// create a new config file
	version, err := utils.GetConfigFileVersion(cfgfile.FileID)
	if err != nil {
		glog.Errorln("GetConfigFileVersion error", err, "requuid", requuid, cfgfile)
		return err
	}

	newFileID := utils.GenMemberConfigFileID(member.MemberName, cfgfile.FileName, version+1)
	newcfgfile := db.UpdateConfigFile(cfgfile, newFileID, newContent)

	newcfgfile, err = manage.CreateConfigFile(ctx, s.dbIns, newcfgfile, requuid)
	if err != nil {
		glog.Errorln("CreateConfigFile error", err, "requuid", requuid, db.PrintConfigFile(newcfgfile), member)
		return err
	}

	glog.Infoln("created new config file, requuid", requuid, db.PrintConfigFile(newcfgfile))

	// update serviceMember to point to the new config file
	newConfigs := db.CopyMemberConfigs(member.Configs)
	newConfigs[cfgIndex].FileID = newcfgfile.FileID
	newConfigs[cfgIndex].FileMD5 = newcfgfile.FileMD5

	newMember := db.UpdateServiceMemberConfigs(member, newConfigs)
	err = s.dbIns.UpdateServiceMember(ctx, member, newMember)
	if err != nil {
		glog.Errorln("UpdateServiceMember error", err, "requuid", requuid, member)
		return err
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

	return nil
}

func (s *ManageHTTPServer) setRedisInit(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogSetRedisInitRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogSetRedisInitRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Cluster != s.cluster || req.Region != s.region {
		glog.Errorln("CatalogSetRedisInitRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	glog.Infoln("setRedisInit", req.ServiceName, "first node id mapping", req.NodeIds[0], "total", len(req.NodeIds), "requuid", requuid)

	// get service uuid
	service, err := s.dbIns.GetService(ctx, s.cluster, req.ServiceName)
	if err != nil {
		glog.Errorln("GetService", req, "error", err, "requuid", requuid)
		return manage.ConvertToHTTPError(err)
	}

	// get service attr
	attr, err := s.dbIns.GetServiceAttr(ctx, service.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, service)
		return manage.ConvertToHTTPError(err)
	}

	// list all serviceMembers
	members, err := s.dbIns.ListServiceMembers(ctx, service.ServiceUUID)
	if err != nil {
		glog.Errorln("ListServiceMembers failed", err, "serviceUUID", service.ServiceUUID, "requuid", requuid)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("get service", service, "has", len(members), "replicas, requuid", requuid)

	// update the init task status message
	statusMsg := "create the member to Redis nodeID mapping for the Redis cluster"
	s.catalogSvcInit.UpdateTaskStatusMsg(service.ServiceUUID, statusMsg)

	// gengerate cluster info config file
	clusterInfoCfg := rediscatalog.CreateClusterInfoFile(req.NodeIds)

	// create the cluster.info file for every member
	for _, member := range members {
		newMember, err := s.createRedisClusterFile(ctx, member, clusterInfoCfg, requuid)
		if err != nil {
			glog.Errorln("updateRedisConfigs error", err, "requuid", requuid, member)
			return manage.ConvertToHTTPError(err)
		}

		if !req.EnableAuth {
			continue
		}

		// enable auth in redis.conf file
		for i, cfg := range newMember.Configs {
			if !rediscatalog.IsRedisConfFile(cfg.FileName) {
				glog.V(5).Infoln("not redis.conf file, skip the config, requuid", requuid, cfg)
				continue
			}

			glog.Infoln("enable auth on redis.conf, requuid", requuid, cfg, newMember)

			err = s.enableRedisAuth(ctx, cfg, i, newMember, requuid)
			if err != nil {
				glog.Errorln("enableRedisAuth error", err, "requuid", requuid, cfg, newMember)
				return manage.ConvertToHTTPError(err)
			}

			glog.Infoln("enabled auth for replia, requuid", requuid, newMember)
			break
		}
	}

	// the config files of all replicas are updated, restart all containers
	glog.Infoln("all replicas are updated, restart all containers, requuid", requuid, req)

	// update the init task status message
	statusMsg = "restarting all containers"
	s.catalogSvcInit.UpdateTaskStatusMsg(service.ServiceUUID, statusMsg)

	err = s.containersvcIns.RestartService(ctx, s.cluster, req.ServiceName, attr.Replicas)
	if err != nil {
		glog.Errorln("RestartService error", err, "requuid", requuid, req)
		return manage.ConvertToHTTPError(err)
	}

	// set service initialized
	glog.Infoln("all containers restarted, set service initialized, requuid", requuid, req)

	return s.setServiceInitialized(ctx, req.ServiceName, requuid)
}

func (s *ManageHTTPServer) createRedisClusterFile(ctx context.Context, member *common.ServiceMember,
	cfg *manage.ReplicaConfigFile, requuid string) (newMember *common.ServiceMember, err error) {

	// check if member has the cluster info file, as failure could happen at any time and init task will be retried.
	for _, c := range member.Configs {
		if rediscatalog.IsClusterInfoFile(c.FileName) {
			chksum := utils.GenMD5(cfg.Content)
			if c.FileMD5 != chksum {
				// this is an unknown internal error. the cluster info content should be the same between retries.
				glog.Errorln("Redis cluster file exist but content not match, new content", cfg.Content, chksum,
					"existing config", c, "requuid", requuid, member)
				return nil, common.ErrConfigMismatch
			}

			glog.Infoln("Redis cluster file is already created for member", member.MemberName,
				"service", member.ServiceUUID, "requuid", requuid)
			return member, nil
		}
	}

	// the cluster info file not exist, create it
	version := int64(0)
	fileID := utils.GenMemberConfigFileID(member.MemberName, cfg.FileName, version)
	initcfgfile := db.CreateInitialConfigFile(member.ServiceUUID, fileID, cfg.FileName, cfg.FileMode, cfg.Content)
	cfgfile, err := manage.CreateConfigFile(ctx, s.dbIns, initcfgfile, requuid)
	if err != nil {
		glog.Errorln("createConfigFile error", err, "fileID", fileID,
			"service", member.ServiceUUID, "member", member.MemberName, "requuid", requuid)
		return nil, err
	}

	glog.Infoln("created the Redis cluster config file, requuid", requuid, db.PrintConfigFile(cfgfile))

	// add the new config file to ServiceMember
	config := &common.MemberConfig{FileName: cfg.FileName, FileID: fileID, FileMD5: cfgfile.FileMD5}

	newConfigs := db.CopyMemberConfigs(member.Configs)
	newConfigs = append(newConfigs, config)

	newMember = db.UpdateServiceMemberConfigs(member, newConfigs)
	err = s.dbIns.UpdateServiceMember(ctx, member, newMember)
	if err != nil {
		glog.Errorln("UpdateServiceMember error", err, "requuid", requuid, member)
		return nil, err
	}

	glog.Infoln("added the cluster config to service member", member.MemberName, member.ServiceUUID, "requuid", requuid)
	return newMember, nil
}

// TODO most code is the same with enableMongoDBAuth, unify it to avoid duplicate code.
func (s *ManageHTTPServer) enableRedisAuth(ctx context.Context,
	cfg *common.MemberConfig, cfgIndex int, member *common.ServiceMember, requuid string) error {
	// fetch the config file
	cfgfile, err := s.dbIns.GetConfigFile(ctx, member.ServiceUUID, cfg.FileID)
	if err != nil {
		glog.Errorln("GetConfigFile error", err, "requuid", requuid, cfg, member)
		return err
	}

	// if auth is enabled, return
	if rediscatalog.IsAuthEnabled(cfgfile.Content) {
		glog.Infoln("auth is already enabled in the config file", db.PrintConfigFile(cfgfile), "requuid", requuid, member)
		return nil
	}

	// auth is not enabled, enable it
	newContent := rediscatalog.EnableRedisAuth(cfgfile.Content)

	// create a new config file
	version, err := utils.GetConfigFileVersion(cfgfile.FileID)
	if err != nil {
		glog.Errorln("GetConfigFileVersion error", err, "requuid", requuid, cfgfile)
		return err
	}

	newFileID := utils.GenMemberConfigFileID(member.MemberName, cfgfile.FileName, version+1)
	newcfgfile := db.UpdateConfigFile(cfgfile, newFileID, newContent)

	newcfgfile, err = manage.CreateConfigFile(ctx, s.dbIns, newcfgfile, requuid)
	if err != nil {
		glog.Errorln("CreateConfigFile error", err, "requuid", requuid, db.PrintConfigFile(newcfgfile), member)
		return err
	}

	glog.Infoln("created new config file, requuid", requuid, db.PrintConfigFile(newcfgfile))

	// update serviceMember to point to the new config file
	newConfigs := db.CopyMemberConfigs(member.Configs)
	newConfigs[cfgIndex].FileID = newcfgfile.FileID
	newConfigs[cfgIndex].FileMD5 = newcfgfile.FileMD5

	newMember := db.UpdateServiceMemberConfigs(member, newConfigs)
	err = s.dbIns.UpdateServiceMember(ctx, member, newMember)
	if err != nil {
		glog.Errorln("UpdateServiceMember error", err, "requuid", requuid, member)
		return err
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

	return nil
}
