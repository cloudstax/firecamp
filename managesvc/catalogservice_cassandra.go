package managesvc

import (
	"encoding/json"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/catalog"
	"github.com/cloudstax/firecamp/api/manage"
	"github.com/cloudstax/firecamp/catalog/cassandra"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
)

func (s *ManageHTTPServer) createCasService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &catalog.CatalogCreateCassandraRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateCassandraRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateCassandraRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	err = cascatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("invalid request", err, "requuid", requuid, req.Service, req.Options)
		return err.Error(), http.StatusBadRequest
	}

	// create the service in the control plane and the container platform
	crReq, jmxUser, jmxPasswd := cascatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs,
		s.cluster, req.Service.ServiceName, req.Options, req.Resource)

	serviceUUID, err := s.createCommonService(ctx, crReq, requuid)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("Cassandra is created, add the init task, requuid", requuid, req.Service)

	if req.Options.Replicas != 1 {
		// run the init task in the background
		s.addCasInitTask(ctx, crReq.Service, serviceUUID, requuid)
	} else {
		glog.Infoln("single node Cassandra, skip the init task, requuid", requuid, req.Service, req.Options)

		errmsg, errcode := s.setServiceInitialized(ctx, req.Service.ServiceName, requuid)
		if errcode != http.StatusOK {
			return errmsg, errcode
		}
	}

	// send back the jmx remote user & passwd
	resp := &catalog.CatalogCreateCassandraResponse{
		JmxRemoteUser:   jmxUser,
		JmxRemotePasswd: jmxPasswd,
	}
	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal CatalogCreateCassandraResponse error", err, "requuid", requuid, req.Service, req.Options)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) addCasInitTask(ctx context.Context,
	req *manage.ServiceCommonRequest, serviceUUID string, requuid string) {
	logCfg := s.logIns.CreateStreamLogConfig(ctx, s.cluster, req.ServiceName, serviceUUID, common.TaskTypeInit)
	taskOpts := cascatalog.GenDefaultInitTaskRequest(req, logCfg, serviceUUID, s.manageurl)

	task := &serviceTask{
		serviceUUID: serviceUUID,
		serviceName: req.ServiceName,
		serviceType: common.CatalogService_Cassandra,
		opts:        taskOpts,
	}

	s.catalogSvcInit.addInitTask(ctx, task)

	glog.Infoln("add init task for service", serviceUUID, "requuid", requuid, req)
}

func (s *ManageHTTPServer) scaleCasService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &catalog.CatalogScaleCassandraRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogScaleCassandraRequest decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("CatalogScaleCassandraRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	svc, err := s.dbIns.GetService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	attr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	if req.Replicas == attr.Spec.Replicas {
		glog.Infoln("service already has", req.Replicas, "replicas, requuid", requuid, attr)
		return "", http.StatusOK
	}

	// not allow scaling down, as Cassandra nodes are peers. When one node goes down,
	// Cassandra will automatically recover the failed replica on another node.
	if req.Replicas < attr.Spec.Replicas {
		errmsg := "scale down Cassandra service is not supported"
		glog.Errorln(errmsg, "requuid", requuid, req.Service)
		return errmsg, http.StatusBadRequest
	}

	// TODO scaling from 1 node requires to add new seed nodes.
	if attr.Spec.Replicas == 1 {
		errmsg := "not support to scale from 1 node, please have at least 3 nodes"
		glog.Errorln(errmsg, "requuid", requuid, req.Service)
		return errmsg, http.StatusBadRequest
	}

	glog.Infoln("scale cassandra service from", attr.Spec.Replicas, "to", req.Replicas, "requuid", requuid, attr)

	// TODO for now, simply create the specs for all members, same for CheckAndCreateServiceMembers.
	// 			optimize to bypass the existing members in the future.
	newAttr := db.UpdateServiceReplicas(attr, req.Replicas)
	replicaConfigs := cascatalog.GenReplicaConfigs(s.platform, s.cluster, req.Service.ServiceName, s.azs, req.Replicas)

	err = s.svc.CheckAndCreateServiceMembers(ctx, req.Replicas, newAttr, replicaConfigs)
	if err != nil {
		glog.Errorln("create new service members error", err, "requuid", requuid, req.Service, req.Replicas)
		return convertToHTTPError(err)
	}

	// scale the container service
	glog.Infoln("scale container servie to", req.Replicas, "requuid", requuid, attr)

	err = s.containersvcIns.ScaleService(ctx, s.cluster, req.Service.ServiceName, req.Replicas)
	if err != nil {
		glog.Errorln("ScaleService new service members error", err, "requuid", requuid, req.Service, req.Replicas)
		return convertToHTTPError(err)
	}

	// update service attr
	glog.Infoln("update service attr to", req.Replicas, "requuid", requuid, attr)

	err = s.dbIns.UpdateServiceAttr(ctx, attr, newAttr)
	if err != nil {
		glog.Errorln("UpdateServiceAttr error", err, "requuid", requuid, req.Service, req.Replicas)
		return convertToHTTPError(err)
	}

	glog.Infoln("service scaled to", req.Replicas, "requuid", requuid, newAttr)

	return "", http.StatusOK
}
