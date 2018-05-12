package catalogsvc

import (
	"encoding/json"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/catalog"
	"github.com/cloudstax/firecamp/api/manage"
	"github.com/cloudstax/firecamp/api/manage/error"
	"github.com/cloudstax/firecamp/catalog/cassandra"
)

func (s *CatalogHTTPServer) createCasService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogCreateCassandraRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogCreateCassandraRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("CatalogCreateCassandraRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err
	}

	err = cascatalog.ValidateRequest(req)
	if err != nil {
		glog.Errorln("invalid request", err, "requuid", requuid, req.Service, req.Options)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	// create the service in the control plane and the container platform
	crReq, jmxUser, jmxPasswd := cascatalog.GenDefaultCreateServiceRequest(s.platform, s.region, s.azs,
		s.cluster, req.Service.ServiceName, req.Options, req.Resource)

	_, err = s.managecli.CreateService(ctx, crReq)
	if err != nil {
		glog.Errorln("createCommonService error", err, "requuid", requuid, req.Service)
		return err
	}

	glog.Infoln("Cassandra is created, add the init task, requuid", requuid, req.Service)

	if req.Options.Replicas != 1 {
		// run the init task in the background
		s.addCasInitTask(ctx, crReq.Service, requuid)
	} else {
		glog.Infoln("single node Cassandra, skip the init task, requuid", requuid, req.Service, req.Options)

		err = s.managecli.SetServiceInitialized(ctx, req.Service)
		if err != nil {
			return err
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
		return clienterr.New(http.StatusInternalServerError, err.Error())
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return nil
}

func (s *CatalogHTTPServer) addCasInitTask(ctx context.Context, req *manage.ServiceCommonRequest, requuid string) {
	taskReq := cascatalog.GenDefaultInitTaskRequest(req, s.serverurl)

	s.catalogSvcInit.addInitTask(ctx, taskReq)

	glog.Infoln("add init task for service", req, "requuid", requuid)
}

func (s *CatalogHTTPServer) scaleCasService(ctx context.Context, r *http.Request, requuid string) error {
	// parse the request
	req := &catalog.CatalogScaleCassandraRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("CatalogScaleCassandraRequest decode request error", err, "requuid", requuid)
		return clienterr.New(http.StatusBadRequest, err.Error())
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("CatalogScaleCassandraRequest invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err
	}

	attr, err := s.managecli.GetServiceAttr(ctx, req.Service)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, req.Service)
		return err
	}

	if req.Replicas == attr.Spec.Replicas {
		glog.Infoln("service already has", req.Replicas, "replicas, requuid", requuid, attr)
		return nil
	}

	// not allow scaling down, as Cassandra nodes are peers. When one node goes down,
	// Cassandra will automatically recover the failed replica on another node.
	if req.Replicas < attr.Spec.Replicas {
		errmsg := "scale down Cassandra service is not supported"
		glog.Errorln(errmsg, "requuid", requuid, req.Service)
		return clienterr.New(http.StatusBadRequest, errmsg)
	}

	// TODO scaling from 1 node requires to add new seed nodes.
	if attr.Spec.Replicas == 1 {
		errmsg := "not support to scale from 1 node, please have at least 3 nodes"
		glog.Errorln(errmsg, "requuid", requuid, req.Service)
		return clienterr.New(http.StatusBadRequest, errmsg)
	}

	glog.Infoln("scale cassandra service from", attr.Spec.Replicas, "to", req.Replicas, "requuid", requuid, attr)

	// TODO for now, simply create the specs for all members, same for CheckAndCreateServiceMembers.
	// 			optimize to bypass the existing members in the future.
	replicaConfigs := cascatalog.GenReplicaConfigs(s.platform, s.cluster, req.Service.ServiceName, s.azs, req.Replicas)

	scaleReq := &manage.ScaleServiceRequest{
		Service:        req.Service,
		ReplicaConfigs: replicaConfigs,
	}
	err = s.managecli.ScaleService(ctx, scaleReq)
	if err != nil {
		glog.Errorln("create new service members error", err, "requuid", requuid, req.Service)
		return err
	}

	glog.Infoln("scale servie to", req.Replicas, "requuid", requuid, req.Service)

	return nil
}
