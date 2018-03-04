package manageserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/catalog/consul"
	"github.com/cloudstax/firecamp/catalog/couchdb"
	"github.com/cloudstax/firecamp/catalog/elasticsearch"
	"github.com/cloudstax/firecamp/catalog/kibana"
	"github.com/cloudstax/firecamp/catalog/logstash"
	"github.com/cloudstax/firecamp/catalog/postgres"
	"github.com/cloudstax/firecamp/catalog/zookeeper"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

func (s *ManageHTTPServer) putCatalogServiceOp(ctx context.Context, w http.ResponseWriter,
	r *http.Request, trimURL string, requuid string) (errmsg string, errcode int) {
	switch trimURL {
	case manage.CatalogCreateMongoDBOp:
		return s.createMongoDBService(ctx, w, r, requuid)
	case manage.CatalogCreatePostgreSQLOp:
		return s.createPGService(ctx, r, requuid)
	case manage.CatalogCreateCassandraOp:
		return s.createCasService(ctx, w, r, requuid)
	case manage.CatalogCreateZooKeeperOp:
		return s.createZkService(ctx, r, requuid)
	case manage.CatalogCreateKafkaOp:
		return s.createKafkaService(ctx, r, requuid)
	case manage.CatalogCreateKafkaManagerOp:
		return s.createKafkaManagerService(ctx, r, requuid)
	case manage.CatalogCreateRedisOp:
		return s.createRedisService(ctx, r, requuid)
	case manage.CatalogCreateCouchDBOp:
		return s.createCouchDBService(ctx, r, requuid)
	case manage.CatalogCreateConsulOp:
		return s.createConsulService(ctx, w, r, requuid)
	case manage.CatalogCreateElasticSearchOp:
		return s.createElasticSearchService(ctx, r, requuid)
	case manage.CatalogCreateKibanaOp:
		return s.createKibanaService(ctx, r, requuid)
	case manage.CatalogCreateLogstashOp:
		return s.createLogstashService(ctx, r, requuid)
	case manage.CatalogSetServiceInitOp:
		return s.catalogSetServiceInit(ctx, r, requuid)
	case manage.CatalogSetRedisInitOp:
		return s.setRedisInit(ctx, r, requuid)
	case manage.CatalogUpdateRedisOp:
		return s.updateRedisService(ctx, r, requuid)
	case manage.CatalogUpdateCassandraOp:
		return s.updateCasService(ctx, r, requuid)
	case manage.CatalogScaleCassandraOp:
		return s.scaleCasService(ctx, r, requuid)
	case manage.CatalogUpdateKafkaOp:
		return s.updateKafkaService(ctx, r, requuid)
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

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("CatalogCheckServiceInitRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
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
			case common.CatalogService_MongoDB:
				userAttr := &common.MongoDBUserAttr{}
				err = json.Unmarshal(attr.UserAttr.AttrBytes, userAttr)
				if err != nil {
					glog.Errorln("Unmarshal mongodb user attr error", err, "requuid", requuid, attr)
					return manage.ConvertToHTTPError(err)
				}
				opts := &manage.CatalogMongoDBOptions{
					Shards:           userAttr.Shards,
					ReplicasPerShard: userAttr.ReplicasPerShard,
					ReplicaSetOnly:   userAttr.ReplicaSetOnly,
					ConfigServers:    userAttr.ConfigServers,
					Admin:            req.Admin,
					AdminPasswd:      req.AdminPasswd,
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

			case common.CatalogService_Redis:
				redisUserAttr := &common.RedisUserAttr{}
				err = json.Unmarshal(attr.UserAttr.AttrBytes, redisUserAttr)
				if err != nil {
					glog.Errorln("Unmarshal redis user attr error", err, "requuid", requuid, attr)
					return manage.ConvertToHTTPError(err)
				}
				err = s.addRedisInitTask(ctx, req.Service, attr.ServiceUUID, redisUserAttr.Shards, redisUserAttr.ReplicasPerShard, requuid)
				if err != nil {
					glog.Errorln("addRedisInitTask error", err, "requuid", requuid, req.Service)
					return manage.ConvertToHTTPError(err)
				}

			case common.CatalogService_CouchDB:
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

func (s *ManageHTTPServer) createPGService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreatePostgreSQLRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreatePostgreSQLRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
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
	crReq, err := pgcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster, req.Service.ServiceName, req.Resource, req.Options)
	if err != nil {
		glog.Errorln("GenDefaultCreateServiceRequest error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}
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

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("CatalogCreateZooKeeperRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
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
	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
}

func (s *ManageHTTPServer) updateJmxConfigFile(ctx context.Context, serviceUUID string, members []*common.ServiceMember, jmxUser string, jmxPasswd string, requuid string) error {
	for _, member := range members {
		var cfg *common.MemberConfig
		cfgIndex := -1
		for i, c := range member.Configs {
			if catalog.IsJmxConfFile(c.FileName) {
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

		// replace the original member jmx conf file content
		// TODO if there are like 100 nodes, it may be worth for all members to use the same config file.
		newContent := catalog.CreateJmxConfFileContent(jmxUser, jmxPasswd)
		err = s.updateMemberConfig(ctx, member, cfgfile, cfgIndex, newContent, requuid)
		if err != nil {
			glog.Errorln("updateMemberConfig error", err, "requuid", requuid, cfg, member)
			return err
		}

		glog.Infoln("updated jmx user & password", jmxUser, "for member", member, "requuid", requuid)
	}

	glog.Infoln("updated jmx user & password for service", serviceUUID, "requuid", requuid)
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

	err = s.checkCommonRequest(req.Service)
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

	existingEncryptPasswd, err := s.getCouchDBExistingEncryptPasswd(ctx, req, requuid)
	if err != nil {
		glog.Errorln("getCouchDBExistingEncryptPasswd error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	// create the service in the control plane and the container platform
	crReq, err := couchdbcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Resource, req.Options, existingEncryptPasswd)
	if err != nil {
		glog.Errorln("GenDefaultCreateServiceRequest error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}
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

func (s *ManageHTTPServer) getCouchDBExistingEncryptPasswd(ctx context.Context, req *manage.CatalogCreateCouchDBRequest, requuid string) (existingEncryptPasswd string, err error) {
	// get the possible existing service attr.
	// the couchdbcatalog.encryptPasswd generates a uuid as salt to encrypt the password.
	// the uuid will be different for the retry request and causes the retry failed.
	svc, err := s.dbIns.GetService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		if err == db.ErrDBRecordNotFound {
			// service not exist
			return "", nil
		}

		glog.Errorln("GetService error", err, "requuid", requuid, req.Service)
		return "", err
	}

	// service exists, get attr
	attr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		if err == db.ErrDBRecordNotFound {
			// service attr not exists
			return "", nil
		}

		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, req.Service)
		return "", err
	}

	// service attr exists, get the existing encryptedPasswd
	if attr.UserAttr.ServiceType != common.CatalogService_CouchDB {
		glog.Errorln("existing service is not couchdb", attr.UserAttr.ServiceType, attr, "requuid", requuid, req.Service)
		return "", common.ErrServiceExist
	}

	ua := &common.CouchDBUserAttr{}
	err = json.Unmarshal(attr.UserAttr.AttrBytes, ua)
	if err != nil {
		glog.Errorln("Unmarshal user attr error", err, "requuid", requuid, req.Service)
		return "", err
	}

	glog.Infoln("get existing encryptPasswd, requuid", requuid, req.Service)
	return ua.EncryptedPasswd, nil
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
	req := &manage.CatalogCreateConsulRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateConsulRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
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
	crReq, err := consulcatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Resource, req.Options)
	if err != nil {
		glog.Errorln("GenDefaultCreateServiceRequest error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	// create the service in the control plane
	serviceUUID, err := s.svc.CreateService(ctx, crReq, s.domain, s.vpcID)
	if err != nil {
		glog.Errorln("create service error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("create consul service in the control plane", serviceUUID, "requuid", requuid, req.Service, req.Options)

	serverips, err := s.updateConsulConfigs(ctx, serviceUUID, requuid)
	if err != nil {
		glog.Errorln("updateConsulConfigs error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	err = s.createContainerService(ctx, crReq, serviceUUID, requuid)
	if err != nil {
		glog.Errorln("createContainerService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("created Consul service", serviceUUID, "server ips", serverips, "requuid", requuid, req.Service, req.Options)

	// consul does not require additional init work. set service initialized
	errmsg, errcode = s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
	if len(errmsg) != 0 {
		glog.Errorln("setServiceInitialized error", errcode, errmsg, "requuid", requuid, req.Service, req.Options)
		return errmsg, errcode
	}

	resp := &manage.CatalogCreateConsulResponse{ConsulServerIPs: serverips}
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
	req := &manage.CatalogCreateElasticSearchRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateElasticSearchRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
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
	crReq, err := escatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Resource, req.Options)
	if err != nil {
		glog.Errorln("GenDefaultCreateServiceRequest error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}
	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("created elasticsearch service", serviceUUID, "requuid", requuid, req.Service)

	// elasticsearch does not require additional init work. set service initialized
	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
}

func (s *ManageHTTPServer) createKibanaService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateKibanaRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateKibanaRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
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
		return manage.ConvertToHTTPError(err)
	}

	// get the elasticsearch service attr
	attr, err := s.dbIns.GetServiceAttr(ctx, service.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, service)
		return manage.ConvertToHTTPError(err)
	}

	esNode := escatalog.GetFirstMemberHost(attr.DomainName, attr.ServiceName)

	// create the service in the control plane and the container platform
	crReq, err := kibanacatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs, s.cluster,
		req.Service.ServiceName, req.Resource, req.Options, esNode)
	if err != nil {
		glog.Errorln("GenDefaultCreateServiceRequest error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}
	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("created kibana service", serviceUUID, "requuid", requuid, req.Service)

	// kibana does not require additional init work. set service initialized
	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
}

func (s *ManageHTTPServer) createLogstashService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CatalogCreateLogstashRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateLogstashRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
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
	crReq, err := logstashcatalog.GenDefaultCreateServiceRequest(s.platform, s.region,
		s.azs, s.cluster, req.Service.ServiceName, req.Resource, req.Options)
	if err != nil {
		glog.Errorln("GenDefaultCreateServiceRequest error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}
	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("created logstash service", serviceUUID, "requuid", requuid, req.Service)

	// logstash does not require additional init work. set service initialized
	return s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
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
	case common.CatalogService_MongoDB:
		return s.setMongoDBInit(ctx, req, requuid)

	case common.CatalogService_Cassandra:
		glog.Infoln("set cassandra service initialized, requuid", requuid, req)
		return s.setServiceInitialized(ctx, req.ServiceName, requuid)

	case common.CatalogService_CouchDB:
		glog.Infoln("set couchdb service initialized, requuid", requuid, req)
		return s.setServiceInitialized(ctx, req.ServiceName, requuid)

	// other services do not require the init task.
	default:
		glog.Errorln("unknown service type", req)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}
}

func (s *ManageHTTPServer) updateConsulConfigs(ctx context.Context, serviceUUID string, requuid string) (serverips []string, err error) {
	// update the consul member address to the assigned static ip in the basic_config.json file
	members, err := s.dbIns.ListServiceMembers(ctx, serviceUUID)
	if err != nil {
		glog.Errorln("ListServiceMembers failed", err, "serviceUUID", serviceUUID, "requuid", requuid)
		return nil, err
	}

	serverips = make([]string, len(members))
	memberips := make(map[string]string)
	for i, m := range members {
		memberdns := dns.GenDNSName(m.MemberName, s.domain)
		memberips[memberdns] = m.StaticIP
		serverips[i] = m.StaticIP
	}

	for _, m := range members {
		err = s.updateConsulMemberConfig(ctx, m, memberips, requuid)
		if err != nil {
			return nil, err
		}
	}

	glog.Infoln("updated ip to consul configs", serviceUUID, "requuid", requuid)
	return serverips, nil
}

func (s *ManageHTTPServer) updateConsulMemberConfig(ctx context.Context, member *common.ServiceMember, memberips map[string]string, requuid string) error {
	var cfg *common.MemberConfig
	cfgIndex := -1
	for i, c := range member.Configs {
		if consulcatalog.IsBasicConfigFile(c.FileName) {
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

	// replace the original member dns name by member ip
	newContent := consulcatalog.ReplaceMemberName(cfgfile.Content, memberips)

	return s.updateMemberConfig(ctx, member, cfgfile, cfgIndex, newContent, requuid)
}

func (s *ManageHTTPServer) updateMemberConfig(ctx context.Context, member *common.ServiceMember,
	cfgfile *common.ConfigFile, cfgIndex int, newContent string, requuid string) error {
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

func (s *ManageHTTPServer) getExistingJmxPasswd(ctx context.Context, req *manage.ServiceCommonRequest, requuid string) (jmxUser string, jmxPasswd string, err error) {
	// check if service attributes exist. If yes, this would be a retry request, use the existing JmxRemoteUser & JmxRemotePasswd.
	svc, err := s.dbIns.GetService(ctx, s.cluster, req.ServiceName)
	if err != nil {
		if err != db.ErrDBRecordNotFound {
			glog.Errorln("GetService error", err, "requuid", requuid, req)
			return "", "", err
		}
		// service not exist
		return "", "", nil
	}

	// service exists
	attr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		if err != db.ErrDBRecordNotFound {
			glog.Errorln("GetServiceAttr error", err, "requuid", requuid, req)
			return "", "", err
		}
		// service attr not exist
		return "", "", nil
	}

	// service attributes exist, use the existing JmxRemoteUser & JmxRemotePasswd
	switch attr.UserAttr.ServiceType {
	case common.CatalogService_Cassandra:
		userAttr := &common.CasUserAttr{}
		err = json.Unmarshal(attr.UserAttr.AttrBytes, userAttr)
		if err != nil {
			glog.Errorln("Unmarshal CasUserAttr error", err, "requuid", requuid, req)
			return "", "", err
		}

		glog.Infoln("get existing jmx user and passwd for cassandra, requuid", requuid, req)
		return userAttr.JmxRemoteUser, userAttr.JmxRemotePasswd, nil

	case common.CatalogService_Kafka:
		userAttr := &common.KafkaUserAttr{}
		err = json.Unmarshal(attr.UserAttr.AttrBytes, userAttr)
		if err != nil {
			glog.Errorln("Unmarshal KafkaUserAttr error", err, "requuid", requuid, req)
			return "", "", err
		}

		glog.Infoln("get existing jmx user and passwd for kafka, requuid", requuid, req)
		return userAttr.JmxRemoteUser, userAttr.JmxRemotePasswd, nil

	default:
		errmsg := fmt.Sprintf("service %s does not support jmx user, requuid %s", req, requuid)
		glog.Errorln(errmsg)
		return "", "", errors.New(errmsg)
	}
}
