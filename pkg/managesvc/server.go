package managesvc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/catalog"
	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/api/manage"
	"github.com/cloudstax/firecamp/pkg/containersvc"
	"github.com/cloudstax/firecamp/pkg/containersvc/k8s"
	"github.com/cloudstax/firecamp/pkg/db"
	"github.com/cloudstax/firecamp/pkg/dns"
	"github.com/cloudstax/firecamp/pkg/log"
	"github.com/cloudstax/firecamp/pkg/server"
	"github.com/cloudstax/firecamp/pkg/utils"
)

const (
	// The max concurrent service tasks. 100 would be enough.
	maxTaskCounts = 100
)

// The ManageHTTPServer is the management http server for the service management.
// It will run in a container, publish a well-known DNS name, which could be accessed
// publicly or privately depend on the customer.
//
// The service creation needs to talk to DB (dynamodb, etc), which is
// accessable inside the cloud platform (aws, etc).
// The ManageHTTPServer will accept the calls from the admin, and talk with DB. This also
// enhance the security. The ManageHTTPServer REST APIs are the only exposed access to
// the cluster.
//
// AWS VPC is the region wide concept. One VPC could cross all AZs of the region.
// The Route53 HostedZone is global concept, one VPC could associate with multiple VPCs.
//
// For the stateful application across multiple Regions, we will have the federation mode.
// Each Region has its own container cluster. Each cluster has its own DB service and
// hosted zone. One federation HostedZone is created for all clusters.
// Note: the federation HostedZone could include multiple VPCs at multiple Regions.
type ManageHTTPServer struct {
	// container platform
	platform  string
	region    string
	cluster   string
	manageurl string
	azs       []string
	domain    string
	vpcID     string

	// the catalog manage service url to forward catalog request to
	// default: http://firecamp-catalogserver.cluster-firecamp.com:27041/
	catalogServerURL string

	validName *regexp.Regexp

	dbIns           db.DB
	serverInfo      server.Info
	logIns          cloudlog.CloudLog
	containersvcIns containersvc.ContainerSvc
	svc             *ManageService
	taskSvc         *manageTaskService
}

// NewManageHTTPServer creates a ManageHTTPServer instance
func NewManageHTTPServer(platform string, cluster string, azs []string, managedns string,
	dbIns db.DB, dnsIns dns.DNS, logIns cloudlog.CloudLog, serverIns server.Server,
	serverInfo server.Info, containersvcIns containersvc.ContainerSvc) *ManageHTTPServer {
	catalogServerURL := dns.GetDefaultCatalogServiceURL(cluster, false)
	svc := NewManageService(dbIns, serverInfo, serverIns, dnsIns, containersvcIns)
	s := &ManageHTTPServer{
		platform:         platform,
		region:           serverInfo.GetLocalRegion(),
		cluster:          cluster,
		manageurl:        dns.GetManageServiceURL(managedns, false),
		azs:              azs,
		domain:           dns.GenDefaultDomainName(cluster),
		vpcID:            serverInfo.GetLocalVpcID(),
		validName:        regexp.MustCompile(common.ServiceNamePattern),
		catalogServerURL: catalogServerURL,
		dbIns:            dbIns,
		logIns:           logIns,
		serverInfo:       serverInfo,
		containersvcIns:  containersvcIns,
		svc:              svc,
		taskSvc:          newManageTaskService(cluster, containersvcIns),
	}
	return s
}

func (s *ManageHTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// generate uuid as request id
	requuid := utils.GenRequestUUID()

	w.Header().Set(manage.RequestID, requuid)
	w.Header().Set(manage.Server, common.SystemName)
	w.Header().Set(manage.ContentType, manage.JsonContentType)

	unescapedURL, err := url.QueryUnescape(r.RequestURI)
	if err != nil {
		glog.Errorln("url.QueryUnescape error", err, r.RequestURI, "requuid", requuid, r)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	op := strings.TrimLeft(unescapedURL, "/")

	glog.Infoln("request Method", r.Method, "URL", r.URL, op, "Host", r.Host, "requuid", requuid, "headers", r.Header)

	// make sure body is closed
	defer s.closeBody(r)

	ctx, cancel := context.WithCancel(context.Background())
	ctx = utils.NewRequestContext(ctx, requuid)
	// call cancel before return. This is to ensure any resource derived
	// from the context will be canceled.
	defer cancel()

	// TODO currently the catalog service operations are included in the same management
	//      container. would be better to separate the catalog services into another container.
	errmsg := ""
	errcode := http.StatusOK
	switch r.Method {
	case http.MethodPost:
		errmsg, errcode = s.putOp(ctx, w, r, op, requuid)
	case http.MethodPut:
		errmsg, errcode = s.putOp(ctx, w, r, op, requuid)
	case http.MethodGet:
		errmsg, errcode = s.getOp(ctx, w, r, op, requuid)
	case http.MethodDelete:
		errmsg, errcode = s.delOp(ctx, w, r, op, requuid)
	default:
		errmsg = http.StatusText(http.StatusNotImplemented)
		errcode = http.StatusNotImplemented
	}

	glog.Infoln("request done, errcode", errcode, "errmsg", errmsg, "URL", r.URL, "requuid", requuid)

	if errcode != http.StatusOK {
		http.Error(w, errmsg, errcode)
	}
}

// PUT/POST to do the service related operations. The necessary parameters should be
// passed in the http headers.
// Example:
//   PUT /servicename, create a service.
//   PUT /?SetServiceInitialized, mark a service initialized.
func (s *ManageHTTPServer) putOp(ctx context.Context, w http.ResponseWriter, r *http.Request, op string, requuid string) (errmsg string, errcode int) {
	// check if the request is the catalog service request.
	if strings.HasPrefix(op, catalog.CatalogOpPrefix) {
		return s.forwardCatalogRequest(ctx, w, r, op, requuid)
	}

	// check if the request is other special request.
	if strings.HasPrefix(op, manage.SpecialOpPrefix) {
		switch op {
		case manage.CreateServiceOp:
			return s.createService(ctx, w, r, true, true, requuid)
		case manage.CreateManageServiceOp:
			return s.createService(ctx, w, r, true, false, requuid)
		case manage.CreateContainerServiceOp:
			return s.createService(ctx, w, r, false, true, requuid)
		case manage.ScaleServiceOp:
			return s.scaleService(ctx, w, r, requuid)
		case manage.UpdateServiceConfigOp:
			return s.updateServiceConfig(ctx, w, r, requuid)
		case manage.UpdateMemberConfigOp:
			return s.updateMemberConfig(ctx, w, r, requuid)
		case manage.ServiceInitializedOp:
			return s.putServiceInitialized(ctx, w, r, requuid)
		case manage.RunTaskOp:
			return s.runTask(ctx, w, r, requuid)
		case manage.StopServiceOp:
			return s.stopService(ctx, w, r, requuid)
		case manage.StartServiceOp:
			return s.startService(ctx, w, r, requuid)
		case manage.RollingRestartServiceOp:
			return s.rollingRestartService(ctx, w, r, requuid)
		case manage.UpdateServiceResourceOp:
			return s.updateServiceResource(ctx, w, r, requuid)
		case manage.UpgradeServiceOp:
			return s.upgradeService(ctx, r, requuid)
		default:
			return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
		}
	}

	return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
}

func (s *ManageHTTPServer) forwardCatalogRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, op string, requuid string) (errmsg string, errcode int) {
	newurl := s.catalogServerURL + op

	newReq, err := http.NewRequest(r.Method, newurl, r.Body)
	if err != nil {
		glog.Errorln("new catalog server request error", err, newurl, "requuid", requuid)
		return convertToHTTPError(err)
	}

	// shallow copy header
	newReq.Header = r.Header

	cli := &http.Client{}
	resp, err := cli.Do(newReq)
	if err != nil {
		glog.Errorln("catalog request error", err, newurl, "requuid", requuid)
		return convertToHTTPError(err)
	}

	defer s.closeRespBody(resp)

	glog.Infoln("catalog request done", newurl, "requuid", requuid, resp)

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		glog.Errorln("catalog request, read response error", err, newurl, "requuid", requuid)
		return convertToHTTPError(err)
	}

	if resp.StatusCode != http.StatusOK {
		glog.Errorln("do catalog request error", string(body), resp.StatusCode, newurl, "requuid", requuid)
		return string(body), resp.StatusCode
	}

	// success
	w.WriteHeader(http.StatusOK)
	w.Write(body)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) putServiceInitialized(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		glog.Errorln("putServiceInitialized read body error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	req := &manage.ServiceCommonRequest{}
	err = json.Unmarshal(b, req)
	if err != nil {
		glog.Errorln("putServiceInitialized decode request error", err, "requuid", requuid, string(b[:]))
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Cluster != s.cluster || req.Region != s.region {
		glog.Errorln("putServiceInitialized invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	return s.setServiceInitialized(ctx, req.ServiceName, requuid)
}

func (s *ManageHTTPServer) setServiceInitialized(ctx context.Context, service string, requuid string) (errmsg string, errcode int) {
	err := s.svc.SetServiceInitialized(ctx, s.cluster, service)
	if err != nil {
		glog.Errorln("setServiceInitialized error", err, "service", service, "requuid", requuid)
		return convertToHTTPError(err)
	}

	glog.Infoln("set service", service, "initialized, requuid", requuid)
	return "", http.StatusOK
}

func (s *ManageHTTPServer) checkRequest(service *manage.ServiceCommonRequest, res *common.Resources) error {
	if res.MaxMemMB != common.DefaultMaxMemoryMB && res.MaxMemMB < res.ReserveMemMB {
		return errors.New("Invalid request, max-memory should be larger than reserve-memory")
	}

	if res.MaxCPUUnits != common.DefaultMaxCPUUnits && res.MaxCPUUnits < res.ReserveCPUUnits {
		return errors.New("Invalid request, max-cpuunits should be larger than reserve-cpuunits")
	}

	return s.checkCommonRequest(service)
}

func (s *ManageHTTPServer) checkCommonRequest(service *manage.ServiceCommonRequest) error {
	if !s.validName.MatchString(service.ServiceName) {
		return errors.New("Invalid request, service name is not valid")
	}

	if len(service.ServiceName) > common.MaxServiceNameLength {
		return fmt.Errorf("Invalid Request, service name max length is %d", common.MaxServiceNameLength)
	}

	if service.Cluster != s.cluster || service.Region != s.region {
		return errors.New("Invalid request, cluster or region not match")
	}

	return nil
}

func (s *ManageHTTPServer) updateServiceResource(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.UpdateServiceResourceRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("decode UpdateServiceResourceRequest error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("invalid request, local cluster", s.cluster, "region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	svc, err := s.dbIns.GetService(ctx, req.Service.Cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	attr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, svc)
		return convertToHTTPError(err)
	}

	res := db.CopyResources(&attr.Spec.Resource)
	if req.MaxCPUUnits != nil {
		res.MaxCPUUnits = *req.MaxCPUUnits
	}
	if req.ReserveCPUUnits != nil {
		res.ReserveCPUUnits = *req.ReserveCPUUnits
	}
	if req.MaxMemMB != nil {
		res.MaxMemMB = *req.MaxMemMB
	}
	if req.ReserveMemMB != nil {
		res.ReserveMemMB = *req.ReserveMemMB
	}

	err = utils.CheckResource(res)
	if err != nil {
		glog.Errorln("invalid resource", err, res, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	// update container service to use the new resource
	opts := &containersvc.UpdateServiceOptions{
		Cluster:         s.cluster,
		ServiceName:     req.Service.ServiceName,
		MaxCPUUnits:     req.MaxCPUUnits,
		ReserveCPUUnits: req.ReserveCPUUnits,
		MaxMemMB:        req.MaxMemMB,
		ReserveMemMB:    req.ReserveMemMB,
	}

	err = s.containersvcIns.UpdateService(ctx, opts)
	if err != nil {
		glog.Errorln("update container service error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	// update service attr
	newAttr := db.UpdateServiceResources(attr, res)
	err = s.dbIns.UpdateServiceAttr(ctx, attr, newAttr)
	if err != nil {
		glog.Errorln("UpdateServiceAttr error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("updated container service", req.Service, "requuid", requuid)
	return "", http.StatusOK
}

func (s *ManageHTTPServer) scaleService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.ScaleServiceRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("scaleService decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("invalid request, local cluster", s.cluster, "region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	svc, err := s.dbIns.GetService(ctx, req.Service.Cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	attr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	replicas := int64(len(req.ReplicaConfigs))

	if replicas == attr.Spec.Replicas {
		glog.Infoln("service already has", replicas, "replicas, requuid", requuid, req.Service)
		return "", http.StatusOK
	}

	// Not allow scaling down. The service members may be peers, such as Cassandra.
	// When one node goes down, Cassandra will automatically recover the failed
	// replica on another node.
	if replicas < attr.Spec.Replicas {
		errmsg := "scale down service is not supported"
		glog.Errorln(errmsg, "requuid", requuid, req.Service)
		return errmsg, http.StatusBadRequest
	}

	glog.Infoln("scale service from", attr.Spec.Replicas, "to", replicas, "requuid", requuid, req.Service)

	newAttr := db.UpdateServiceReplicas(attr, replicas)
	err = s.svc.CheckAndCreateServiceMembers(ctx, replicas, newAttr, req.ReplicaConfigs)
	if err != nil {
		glog.Errorln("create new service members error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	err = s.containersvcIns.ScaleService(ctx, s.cluster, req.Service.ServiceName, replicas)
	if err != nil {
		glog.Errorln("start container service error", err, "requuid", requuid, req)
		return convertToHTTPError(err)
	}

	// update service attr
	glog.Infoln("update service attr, requuid", requuid, req.Service)

	err = s.dbIns.UpdateServiceAttr(ctx, attr, newAttr)
	if err != nil {
		glog.Errorln("UpdateServiceAttr error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("service scaled to", replicas, "requuid", requuid, req.Service)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) stopService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.ServiceCommonRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("stopService decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req)
	if err != nil {
		glog.Errorln("stopService invalid request, local cluster", s.cluster, "region",
			s.region, "requuid", requuid, req, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	err = s.containersvcIns.StopService(ctx, s.cluster, req.ServiceName)
	if err != nil {
		glog.Errorln("Stop container service error", err, "requuid", requuid, req)
		return convertToHTTPError(err)
	}

	glog.Infoln("stopped container service", req, "requuid", requuid)
	return "", http.StatusOK
}

func (s *ManageHTTPServer) startService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.ServiceCommonRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("startService decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req)
	if err != nil {
		glog.Errorln("startService invalid request, local cluster", s.cluster, "region",
			s.region, "requuid", requuid, req, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	svc, err := s.dbIns.GetService(ctx, req.Cluster, req.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req)
		return convertToHTTPError(err)
	}

	attr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, req)
		return convertToHTTPError(err)
	}

	err = s.containersvcIns.ScaleService(ctx, s.cluster, req.ServiceName, attr.Spec.Replicas)
	if err != nil {
		glog.Errorln("start container service error", err, "requuid", requuid, req)
		return convertToHTTPError(err)
	}

	glog.Infoln("started container service", req, "requuid", requuid)
	return "", http.StatusOK
}

func (s *ManageHTTPServer) rollingRestartService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.ServiceCommonRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("rollingRestartService decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req)
	if err != nil {
		glog.Errorln("rollingRestartService invalid request, local cluster", s.cluster, "region",
			s.region, "requuid", requuid, req, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	svc, err := s.dbIns.GetService(ctx, req.Cluster, req.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req)
		return convertToHTTPError(err)
	}

	// check whether the rolling restart task is running
	has, statusMsg, _ := s.taskSvc.hasTask(svc.ServiceUUID)
	if has {
		glog.Infoln("service rolling restart task is already running, statusmsg", statusMsg, "requuid", requuid, req)
		return "", http.StatusOK
	}

	attr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, req)
		return convertToHTTPError(err)
	}

	// simply rolling restart the service members in the backward order.
	// TODO make service aware. For example, for MongoDB ReplicaSet, should gracefully stop
	// and upgrade the primary member first, and then upgrade other members one by one.
	var serviceTasks []string
	if s.platform == common.ContainerPlatformK8s {
		backward := true
		serviceTasks = k8ssvc.GetStatefulSetPodNames(req.ServiceName, attr.Spec.Replicas, backward)
	} else {
		// AWS ECS or Docker Swarm
		members, err := s.dbIns.ListServiceMembers(ctx, svc.ServiceUUID)
		if err != nil {
			glog.Errorln("ListServiceMembers error", err, "requuid", requuid, req)
			return convertToHTTPError(err)
		}

		serviceTasks = make([]string, attr.Spec.Replicas)
		for i := 0; i < len(members); i++ {
			idx := len(members) - i - 1
			serviceTasks[i] = members[idx].Spec.TaskID
		}
	}

	task := &manageTask{
		serviceUUID: attr.ServiceUUID,
		serviceName: attr.Meta.ServiceName,
		restartOpts: &containersvc.RollingRestartOptions{
			Replicas:     attr.Spec.Replicas,
			ServiceTasks: serviceTasks,
		},
	}

	err = s.taskSvc.addTask(attr.ServiceUUID, task, requuid)
	if err != nil {
		glog.Errorln("add rolling restart task error", err, "requuid", requuid, req)
		return convertToHTTPError(err)
	}

	glog.Infoln("rolling restarted service", req, "requuid", requuid)
	return "", http.StatusOK
}

func (s *ManageHTTPServer) getServiceTaskStatus(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.ServiceCommonRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req)
	if err != nil {
		glog.Errorln("getServiceTaskStatus invalid request, local cluster", s.cluster, "region",
			s.region, "requuid", requuid, req, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	svc, err := s.dbIns.GetService(ctx, req.Cluster, req.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req)
		return convertToHTTPError(err)
	}

	// check whether the management task is running
	has, statusMsg, complete := s.taskSvc.hasTask(svc.ServiceUUID)
	if !has {
		glog.Errorln("no manage task for service, requuid", requuid, req)
		statusMsg = "no manage task for service is running"
		complete = true
	} else {
		glog.Infoln("service manage task is running, statusmsg", statusMsg, "requuid", requuid, req)
	}

	resp := &manage.GetServiceTaskStatusResponse{
		Complete:      complete,
		StatusMessage: statusMsg,
	}

	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal error", err, "requuid", requuid, req)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) createService(ctx context.Context, w http.ResponseWriter, r *http.Request,
	createManageService bool, createContainerService bool, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CreateServiceRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("createService decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkRequest(req.Service, req.Resource)
	if err != nil {
		glog.Errorln("createService invalid request, local cluster", s.cluster, "region",
			s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	serviceUUID := ""

	if createManageService {
		// create the manage service
		serviceUUID, err = s.svc.CreateService(ctx, req, s.domain, s.vpcID)
		if err != nil {
			glog.Errorln("create service error", err, "requuid", requuid, req.Service)
			return convertToHTTPError(err)
		}
	}

	if createContainerService {
		if serviceUUID == "" {
			// fetch service uuid
			svc, err := s.dbIns.GetService(ctx, req.Service.Cluster, req.Service.ServiceName)
			if err != nil {
				glog.Errorln("GetService error", err, "requuid", requuid, req.Service)
				return convertToHTTPError(err)
			}
			serviceUUID = svc.ServiceUUID
		}

		// create the container service
		err = s.createContainerService(ctx, req, serviceUUID, requuid)
		if err != nil {
			return convertToHTTPError(err)
		}
	}

	return "", http.StatusOK
}

func (s *ManageHTTPServer) createContainerService(ctx context.Context,
	req *manage.CreateServiceRequest, serviceUUID string, requuid string) error {
	// initialize the cloud log
	err := s.logIns.InitializeServiceLogConfig(ctx, s.cluster, req.Service.ServiceName, serviceUUID)
	if err != nil {
		glog.Errorln("InitializeServiceLogConfig error", err, "requuid", requuid, req.Service)
		return err
	}

	// create the service in the container platform
	exist, err := s.containersvcIns.IsServiceExist(ctx, req.Service.Cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("check container service exist error", err, "requuid", requuid, req.Service)
		return err
	}
	if !exist {
		var logConfig *cloudlog.LogConfig
		if req.ServiceType != common.ServiceTypeStateless {
			logConfig = s.logIns.CreateServiceLogConfig(ctx, s.cluster, req.Service.ServiceName, serviceUUID)
		} else {
			logConfig = s.logIns.CreateStreamLogConfig(ctx, s.cluster, req.Service.ServiceName, serviceUUID, req.Service.ServiceName)
		}
		opts := s.genCreateServiceOptions(req, serviceUUID, logConfig)
		err = s.containersvcIns.CreateService(ctx, opts)
		if err != nil {
			glog.Errorln("create container service error", err, "requuid", requuid, req.Service)
			return err
		}
	}

	glog.Infoln("create container service done, serviceUUID", serviceUUID, "requuid", requuid, req.Service)
	return nil
}

func (s *ManageHTTPServer) updateServiceConfig(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.UpdateServiceConfigRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("updateServiceConfig decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("updateServiceConfig invalid request, local cluster", s.cluster, "region",
			s.region, "requuid", requuid, req.Service, "error", err)
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

	for i, cfg := range attr.Spec.ServiceConfigs {
		if req.ConfigFileName == cfg.FileName {
			// get the current config file
			currCfgFile, err := s.dbIns.GetConfigFile(ctx, attr.ServiceUUID, cfg.FileID)
			if err != nil {
				glog.Errorln("GetConfigFile error", err, "requuid", requuid, cfg)
				return convertToHTTPError(err)
			}

			err = s.updateServiceConfigFile(ctx, attr, i, currCfgFile, req.ConfigFileContent, requuid)
			if err != nil {
				glog.Errorln("updateServiceConfigFile error", err, "requuid", requuid, req.Service)
				return convertToHTTPError(err)
			}

			glog.Infoln("updated service config, requuid", requuid, req.Service)

			return "", http.StatusOK
		}
	}

	errmsg = fmt.Sprintf("config file not exist %s requuid %s %s", req.ConfigFileName, requuid, req.Service)
	glog.Errorln(errmsg)
	return errmsg, http.StatusNotFound
}

func (s *ManageHTTPServer) updateServiceConfigFile(ctx context.Context, attr *common.ServiceAttr,
	serviceCfgIndex int, oldCfg *common.ConfigFile, newContent string, requuid string) error {
	// create the new config file
	version, err := utils.GetConfigFileVersion(oldCfg.FileID)
	if err != nil {
		glog.Errorln("GetConfigFileVersion error", err, "requuid", requuid, db.PrintConfigFile(oldCfg))
		return err
	}

	newFileID := utils.GenConfigFileID(attr.Meta.ServiceName, oldCfg.Meta.FileName, version+1)
	newcfgfile := db.CreateNewConfigFile(oldCfg, newFileID, newContent)
	newcfgfile, err = createConfigFile(ctx, s.dbIns, newcfgfile, requuid)
	if err != nil {
		glog.Errorln("CreateConfigFile error", err, "requuid", requuid, db.PrintConfigFile(newcfgfile))
		return err
	}

	// update ServiceAttr
	newAttr := db.UpdateServiceConfig(attr, serviceCfgIndex, newFileID, newcfgfile.Spec.FileMD5)
	err = s.dbIns.UpdateServiceAttr(ctx, attr, newAttr)
	if err != nil {
		glog.Errorln("UpdateServiceAttr error", err, "requuid", requuid, newAttr)
		return err
	}

	glog.Infoln("updated service config ", oldCfg.Meta.FileName, "requuid", requuid, newAttr)

	// delete the old config file.
	// TODO add the background gc mechanism to delete the garbage.
	//      the old config file may not be deleted at some conditions.
	//      for example, node crashes right before deleting the config file.
	err = s.dbIns.DeleteConfigFile(ctx, attr.ServiceUUID, oldCfg.FileID)
	if err != nil {
		// simply log an error as this only leaves a garbage
		glog.Errorln("DeleteConfigFile error", err, "requuid", requuid, db.PrintConfigFile(oldCfg))
	} else {
		glog.Infoln("deleted the old config file, requuid", requuid, db.PrintConfigFile(oldCfg))
	}

	return nil
}

func (s *ManageHTTPServer) getMemberConfigFile(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.GetMemberConfigFileRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("getMemberConfigFile decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("invalid request, local cluster", s.cluster, "region", s.region, "requuid", requuid, req)
		return err.Error(), http.StatusBadRequest
	}

	svc, err := s.dbIns.GetService(ctx, req.Service.Cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	m, err := s.dbIns.GetServiceMember(ctx, svc.ServiceUUID, req.MemberName)
	if err != nil {
		glog.Errorln("GetServiceMember error", err, req.MemberName, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	for _, cfg := range m.Spec.Configs {
		if req.ConfigFileName == cfg.FileName {
			cfgfile, err := s.dbIns.GetConfigFile(ctx, svc.ServiceUUID, cfg.FileID)
			if err != nil {
				glog.Errorln("GetConfigFile error", err, cfg, "requuid", requuid, req.Service)
				return convertToHTTPError(err)
			}

			resp := &manage.GetConfigFileResponse{
				ConfigFile: cfgfile,
			}

			b, err := json.Marshal(resp)
			if err != nil {
				glog.Errorln("Marshal error", err, "requuid", requuid, req.Service)
				return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
			}

			w.WriteHeader(http.StatusOK)
			w.Write(b)

			glog.Infoln("get config file", cfg, "requuid", requuid, req.Service)

			return "", http.StatusOK
		}
	}

	errmsg = fmt.Sprintf("config file not exist %s requuid %s %s", req.ConfigFileName, requuid, req.Service)
	glog.Errorln(errmsg)
	return errmsg, http.StatusNotFound
}

func (s *ManageHTTPServer) updateMemberConfig(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.UpdateMemberConfigRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("updateMemberConfig decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("updateMemberConfig invalid request, local cluster", s.cluster, "region",
			s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	svc, err := s.dbIns.GetService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	m, err := s.dbIns.GetServiceMember(ctx, svc.ServiceUUID, req.MemberName)
	if err != nil {
		glog.Errorln("GetServiceMember error", err, req.MemberName, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	for i, cfg := range m.Spec.Configs {
		if req.ConfigFileName == cfg.FileName {
			// get the current config file
			currCfgFile, err := s.dbIns.GetConfigFile(ctx, svc.ServiceUUID, cfg.FileID)
			if err != nil {
				glog.Errorln("GetConfigFile error", err, cfg, "requuid", requuid, req.Service)
				return convertToHTTPError(err)
			}

			err = s.updateMemberConfigFile(ctx, m, currCfgFile, i, req.ConfigFileContent, requuid)
			if err != nil {
				glog.Errorln("updateMemberConfigFile error", err, cfg, "requuid", requuid, req.Service)
				return convertToHTTPError(err)
			}

			glog.Infoln("updated service config, requuid", requuid, req.Service)

			return "", http.StatusOK
		}
	}

	errmsg = fmt.Sprintf("config file not exist %s requuid %s %s", req.ConfigFileName, requuid, req.Service)
	glog.Errorln(errmsg)
	return errmsg, http.StatusNotFound
}

func (s *ManageHTTPServer) updateMemberConfigFile(ctx context.Context, member *common.ServiceMember,
	cfgfile *common.ConfigFile, cfgIndex int, newContent string, requuid string) error {
	// create a new config file
	version, err := utils.GetConfigFileVersion(cfgfile.FileID)
	if err != nil {
		glog.Errorln("GetConfigFileVersion error", err, "requuid", requuid, cfgfile)
		return err
	}

	newFileID := utils.GenConfigFileID(member.MemberName, cfgfile.Meta.FileName, version+1)
	newcfgfile := db.CreateNewConfigFile(cfgfile, newFileID, newContent)

	newcfgfile, err = createConfigFile(ctx, s.dbIns, newcfgfile, requuid)
	if err != nil {
		glog.Errorln("CreateConfigFile error", err, "requuid", requuid, db.PrintConfigFile(newcfgfile), member)
		return err
	}

	glog.Infoln("created new config file, requuid", requuid, db.PrintConfigFile(newcfgfile))

	// update serviceMember to point to the new config file
	newConfigs := db.CopyConfigs(member.Spec.Configs)
	newConfigs[cfgIndex].FileID = newcfgfile.FileID
	newConfigs[cfgIndex].FileMD5 = newcfgfile.Spec.FileMD5

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

func (s *ManageHTTPServer) genCreateServiceOptions(req *manage.CreateServiceRequest,
	serviceUUID string, logConfig *cloudlog.LogConfig) *containersvc.CreateServiceOptions {
	commonOpts := &containersvc.CommonOptions{
		Cluster:        req.Service.Cluster,
		ServiceName:    req.Service.ServiceName,
		ServiceUUID:    serviceUUID,
		ServiceType:    req.ServiceType,
		ContainerImage: req.ContainerImage,
		Resource:       req.Resource,
		LogConfig:      logConfig,
	}

	createOpts := &containersvc.CreateServiceOptions{
		Replicas:         req.Replicas,
		Common:           commonOpts,
		PortMappings:     req.PortMappings,
		Envkvs:           req.Envkvs,
		ExternalDNS:      req.RegisterDNS,
		ExternalStaticIP: req.RequireStaticIP,
	}

	if req.Volume != nil {
		createOpts.DataVolume = &containersvc.VolumeOptions{
			MountPath:  req.ContainerPath,
			VolumeType: req.Volume.VolumeType,
			SizeGB:     req.Volume.VolumeSizeGB,
			Iops:       req.Volume.Iops,
			Encrypted:  req.Volume.Encrypted,
		}
	}
	if req.JournalVolume != nil {
		createOpts.JournalVolume = &containersvc.VolumeOptions{
			MountPath:  req.JournalContainerPath,
			VolumeType: req.JournalVolume.VolumeType,
			SizeGB:     req.JournalVolume.VolumeSizeGB,
			Iops:       req.JournalVolume.Iops,
			Encrypted:  req.JournalVolume.Encrypted,
		}
	}

	if req.ServiceType != common.ServiceTypeStateless {
		zones := make(map[string]bool)
		for _, replCfg := range req.ReplicaConfigs {
			zones[replCfg.Zone] = true
		}
		if len(zones) != len(s.azs) {
			placeZones := []string{}
			for k := range zones {
				placeZones = append(placeZones, k)
			}

			sort.Slice(placeZones, func(i, j int) bool {
				return placeZones[i] < placeZones[j]
			})
			createOpts.Place = &containersvc.Placement{
				Zones: placeZones,
			}

			glog.Infoln("deploy to zones", placeZones, "for service", req.Service, "all zones", s.azs)
		}
	}

	return createOpts
}

// Get one service, GET /servicename. Or list services, Get / or /?list-type=1, and additional parameters in headers
func (s *ManageHTTPServer) getOp(ctx context.Context, w http.ResponseWriter,
	r *http.Request, op string, requuid string) (errmsg string, errcode int) {
	// check if the request is the catalog service request.
	if strings.HasPrefix(op, catalog.CatalogOpPrefix) {
		return s.forwardCatalogRequest(ctx, w, r, op, requuid)
	}

	// check if the request is the internal service request.
	if strings.HasPrefix(op, manage.InternalOpPrefix) {
		return s.internalGetOp(ctx, w, r, op, requuid)
	}

	if strings.HasPrefix(op, manage.SpecialOpPrefix) {
		switch op {
		case manage.ListServiceOp:
			return s.listServices(ctx, w, r, requuid)

		case manage.ListServiceMemberOp:
			return s.listServiceMembers(ctx, w, r, requuid)

		case manage.GetServiceStatusOp:
			return s.getServiceStatus(ctx, w, r, requuid)

		case manage.GetServiceConfigFileOp:
			return s.getServiceConfigFile(ctx, w, r, requuid)

		case manage.GetMemberConfigFileOp:
			return s.getMemberConfigFile(ctx, w, r, requuid)

		case manage.GetTaskStatusOp:
			return s.getTaskStatus(ctx, w, r, requuid)

		case manage.GetServiceTaskStatusOp:
			return s.getServiceTaskStatus(ctx, w, r, requuid)

		default:
			return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
		}
	} else {
		// get the detail of one service
		return s.getServiceAttr(ctx, w, r, requuid)
	}
}

func (s *ManageHTTPServer) delOp(ctx context.Context, w http.ResponseWriter, r *http.Request, op string, requuid string) (errmsg string, errcode int) {
	switch op {
	case manage.DeleteServiceOp:
		return s.deleteService(ctx, w, r, requuid)
	case manage.DeleteTaskOp:
		return s.deleteTask(ctx, w, r, requuid)
	default:
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}
}

func (s *ManageHTTPServer) deleteService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	req := &manage.DeleteServiceRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("deleteService decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("deleteService invalid request, local cluster", s.cluster, "region",
			s.region, "requuid", requuid, req, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	// delete the service on the container platform
	err = s.containersvcIns.StopService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("StopService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	err = s.containersvcIns.DeleteService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("delete service from container platform error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	// delete the service cloud log
	svc, err := s.dbIns.GetService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	err = s.logIns.DeleteServiceLogConfig(ctx, s.cluster, req.Service.ServiceName, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("DeleteServiceLogConfig error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	// delete the service on the control plane
	// TODO support the possible concurrent service deletion and creation with the same name.
	volIDs, err := s.svc.DeleteService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("DeleteService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("deleted service", req.Service.ServiceName, "requuid", requuid, "volumes", volIDs)

	resp := &manage.DeleteServiceResponse{VolumeIDs: volIDs}
	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal DeleteServiceResponse error", err, "requuid", requuid, req.Service)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}

// upgrades one service to the current release.
// this is a common upgrade request for all services that do not require specific upgrade options.
func (s *ManageHTTPServer) upgradeService(ctx context.Context, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.ServiceCommonRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("upgradeService decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req)
	if err != nil {
		glog.Errorln("upgradeService invalid request, local cluster", s.cluster, "region",
			s.region, "requuid", requuid, req, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	// upgrade to 0.9.6 requires to recreate the service with the original volumes.

	errmsg = fmt.Sprintf("unsupported upgrade to version %s", common.Version)
	glog.Errorln(errmsg, "requuid", requuid, req)
	return errmsg, http.StatusBadRequest
}

func (s *ManageHTTPServer) generalUpgradeService(ctx context.Context, attr *common.ServiceAttr, req *manage.ServiceCommonRequest, requuid string) (errmsg string, errcode int) {
	// the default container service update request
	opts := &containersvc.UpdateServiceOptions{
		Cluster:        req.Cluster,
		ServiceName:    req.ServiceName,
		ReleaseVersion: common.Version,
	}

	// update the container service
	err := s.containersvcIns.UpdateService(ctx, opts)
	if err != nil {
		glog.Errorln("upgrade service release error", err, req)
		return convertToHTTPError(err)
	}

	glog.Infoln("upgraded service to release", common.Version, "requuid", requuid, req)
	return "", http.StatusOK
}

func (s *ManageHTTPServer) listServices(ctx context.Context, w http.ResponseWriter,
	r *http.Request, requuid string) (errmsg string, errcode int) {
	// no need to support token and MaxKeys, simply returns all as one cluster would
	// not have too many services.
	req := &manage.ListServiceRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("listServices decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Cluster != s.cluster || req.Region != s.region {
		glog.Errorln("listServices invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	glog.Infoln("listServices, prefix", req.Prefix, "requuid", requuid)

	services, err := s.dbIns.ListServices(ctx, s.cluster)
	if err != nil {
		glog.Errorln("ListServices error", err, "prefix", req.Prefix, "requuid", requuid)
		return convertToHTTPError(err)
	}

	var serviceAttrs []*common.ServiceAttr
	for _, service := range services {
		if len(req.Prefix) == 0 || strings.HasPrefix(service.ServiceName, req.Prefix) {
			// fetch the detail service attr
			attr, err := s.dbIns.GetServiceAttr(ctx, service.ServiceUUID)
			if err != nil {
				glog.Errorln("GetServiceAttr error", err, service, "requuid", requuid)
				return convertToHTTPError(err)
			}

			glog.Infoln("GetServiceAttr", attr, "requuid", requuid)
			serviceAttrs = append(serviceAttrs, attr)
		}
	}

	glog.Infoln("list", len(services), "services, prefix", req.Prefix, "requuid", requuid)

	resp := &manage.ListServiceResponse{Services: serviceAttrs}
	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal ListServiceResponse error", err, "requuid", requuid, req)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) listServiceMembers(ctx context.Context, w http.ResponseWriter,
	r *http.Request, requuid string) (errmsg string, errcode int) {
	// TODO support token and MaxKeys if necessary.
	req := &manage.ListServiceMemberRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("listServiceMembers decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("listServiceMembers invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	glog.Infoln("listServiceMembers", req.Service, "requuid", requuid)

	service, err := s.dbIns.GetService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("db GetService error", err, req.Service.ServiceName, "requuid", requuid)
		return convertToHTTPError(err)
	}

	members, err := s.dbIns.ListServiceMembers(ctx, service.ServiceUUID)
	if err != nil {
		glog.Errorln("db ListServiceMembers error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("list", len(members), "serviceMembers, requuid", requuid, req.Service)

	resp := &manage.ListServiceMemberResponse{ServiceMembers: members}
	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal ListServiceMemberResponse error", err, "requuid", requuid, req)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) getServiceAttr(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// no need to support token and MaxKeys, simply returns all serviceMembers. Assume one serviceMember
	// attribute is 1KB. If the service has 1000 serviceMembers, the whole list would be 1MB.
	req := &manage.ServiceCommonRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("getServiceAttr decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req)
	if err != nil {
		glog.Errorln("getServiceAttr invalid request, local cluster", s.cluster, "region",
			s.region, "requuid", requuid, req, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	service, err := s.dbIns.GetService(ctx, s.cluster, req.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req)
		return convertToHTTPError(err)
	}

	attr, err := s.dbIns.GetServiceAttr(ctx, service.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, service, "requuid", requuid)
		return convertToHTTPError(err)
	}

	resp := &manage.GetServiceAttributesResponse{Service: attr}

	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal GetServiceAttributesResponse error", err, attr, "requuid", requuid)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) getServiceStatus(ctx context.Context,
	w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	req := &manage.ServiceCommonRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("getServiceStatus decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Cluster != s.cluster || req.Region != s.region {
		glog.Errorln("invalid request, local cluster", s.cluster, "region", s.region, "requuid", requuid, req)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	status, err := s.containersvcIns.GetServiceStatus(ctx, req.Cluster, req.ServiceName)
	if err != nil {
		glog.Errorln("GetServiceStatus error", err, "requuid", requuid, req)
		return convertToHTTPError(err)
	}

	resp := &common.ServiceStatus{
		RunningCount: status.RunningCount,
		DesiredCount: status.DesiredCount,
	}

	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal error", err, "requuid", requuid, req)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	glog.Infoln("get service status", status, "requuid", requuid, req)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) getServiceConfigFile(ctx context.Context,
	w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	req := &manage.GetServiceConfigFileRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("getServiceConfigFile decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("invalid request, local cluster", s.cluster, "region", s.region, "requuid", requuid, req.Service)
		return err.Error(), http.StatusBadRequest
	}

	svc, err := s.dbIns.GetService(ctx, req.Service.Cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	attr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	for _, cfg := range attr.Spec.ServiceConfigs {
		if cfg.FileName == req.ConfigFileName {
			cfg, err := s.dbIns.GetConfigFile(ctx, attr.ServiceUUID, cfg.FileID)
			if err != nil {
				glog.Errorln("GetConfigFile error", err, cfg, "requuid", requuid, req.Service)
				return convertToHTTPError(err)
			}

			resp := &manage.GetConfigFileResponse{
				ConfigFile: cfg,
			}

			b, err := json.Marshal(resp)
			if err != nil {
				glog.Errorln("Marshal error", err, "requuid", requuid, req.Service)
				return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
			}

			w.WriteHeader(http.StatusOK)
			w.Write(b)

			glog.Infoln("get config file", cfg, "requuid", requuid, req.Service)

			return "", http.StatusOK
		}
	}

	errmsg = fmt.Sprintf("config file not found, requuid %s, %s", requuid, req.Service)
	glog.Errorln(errmsg)
	return errmsg, http.StatusNotFound
}

func (s *ManageHTTPServer) closeBody(r *http.Request) {
	if r.Body != nil {
		r.Body.Close()
	}
}

func (s *ManageHTTPServer) runTask(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.RunTaskRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("runTask decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Service.Cluster != s.cluster || req.Service.Region != s.region {
		glog.Errorln("invalid request, local cluster", s.cluster, "region",
			s.region, "requuid", requuid, "task type", req.TaskType, req.Service)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	svc, err := s.dbIns.GetService(ctx, req.Service.Cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req.Service)
		return convertToHTTPError(err)
	}

	logConfig := s.logIns.CreateStreamLogConfig(ctx, s.cluster, req.Service.ServiceName, svc.ServiceUUID, req.TaskType)

	commonOpts := &containersvc.CommonOptions{
		Cluster:        req.Service.Cluster,
		ServiceName:    req.Service.ServiceName,
		ServiceUUID:    svc.ServiceUUID,
		ContainerImage: req.ContainerImage,
		Resource:       req.Resource,
		LogConfig:      logConfig,
	}

	opts := &containersvc.RunTaskOptions{
		Common:   commonOpts,
		TaskType: req.TaskType,
		Envkvs:   req.Envkvs,
	}

	taskID, err := s.containersvcIns.RunTask(ctx, opts)
	if err != nil {
		glog.Errorln("RunTask error", err, "requuid", requuid, req.Service, svc)
		return convertToHTTPError(err)
	}

	glog.Infoln("run task", taskID, "requuid", requuid, req.Service, svc)

	resp := &manage.RunTaskResponse{
		TaskID: taskID,
	}

	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal ServiceRunningStatus error", err, "requuid", requuid, req)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) getTaskStatus(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.GetTaskStatusRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Service.Cluster != s.cluster || req.Service.Region != s.region || len(req.TaskID) == 0 {
		glog.Errorln("invalid request, local cluster", s.cluster, "region",
			s.region, "requuid", requuid, "taskID", req.TaskID, req.Service)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	taskStatus, err := s.containersvcIns.GetTaskStatus(ctx, s.cluster, req.TaskID)
	if err != nil {
		glog.Errorln("GetTaskStatus error", err, "requuid", requuid, "taskID", req.TaskID, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("get task", req.TaskID, "status", taskStatus, "requuid", requuid, req.Service)

	resp := &manage.GetTaskStatusResponse{
		Status: taskStatus,
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

func (s *ManageHTTPServer) deleteTask(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.DeleteTaskRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Service.Cluster != s.cluster || req.Service.Region != s.region ||
		len(req.Service.ServiceName) == 0 || len(req.TaskType) == 0 {
		glog.Errorln("invalid request, local cluster", s.cluster, "region",
			s.region, "requuid", requuid, "taskID", req.TaskType, req.Service)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.containersvcIns.DeleteTask(ctx, s.cluster, req.Service.ServiceName, req.TaskType)
	if err != nil {
		glog.Errorln("DeleteTask error", err, "requuid", requuid, "TaskType", req.TaskType, req.Service)
		return convertToHTTPError(err)
	}

	glog.Infoln("deleted task, requuid", requuid, "TaskType", req.TaskType, req.Service)
	return "", http.StatusOK
}

func (s *ManageHTTPServer) closeRespBody(resp *http.Response) {
	if resp.Body != nil {
		resp.Body.Close()
	}
}
