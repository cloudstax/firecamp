package manageserver

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/context"

	"github.com/golang/glog"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/log"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/manage/service"
	"github.com/cloudstax/firecamp/server"
	"github.com/cloudstax/firecamp/utils"
)

// The ManageHTTPServer is the management http server for the service management.
// It will run in a container, publish a well-known DNS name, which could be accessed
// publicly or privately depend on the customer.
//
// The service creation needs to talk to DB (the controldb, dynamodb, etc), which is
// accessable inside the cloud platform (aws, etc). For example, the controldb running
// as the container in AWS ECS, and accessable via the private DNS name.
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
	platform  string
	region    string
	cluster   string
	manageurl string
	azs       []string
	domain    string
	vpcID     string

	validName *regexp.Regexp

	dbIns           db.DB
	serverInfo      server.Info
	logIns          cloudlog.CloudLog
	containersvcIns containersvc.ContainerSvc
	svc             *manageservice.ManageService
	catalogSvcInit  *catalogServiceInit
}

// NewManageHTTPServer creates a ManageHTTPServer instance
func NewManageHTTPServer(platform string, cluster string, azs []string, managedns string,
	dbIns db.DB, dnsIns dns.DNS, logIns cloudlog.CloudLog, serverIns server.Server,
	serverInfo server.Info, containersvcIns containersvc.ContainerSvc) *ManageHTTPServer {
	svc := manageservice.NewManageService(dbIns, serverInfo, serverIns, dnsIns)
	s := &ManageHTTPServer{
		platform:        platform,
		region:          serverInfo.GetLocalRegion(),
		cluster:         cluster,
		manageurl:       dns.GetManageServiceURL(managedns, false),
		azs:             azs,
		domain:          dns.GenDefaultDomainName(cluster),
		vpcID:           serverInfo.GetLocalVpcID(),
		validName:       regexp.MustCompile(common.ServiceNamePattern),
		dbIns:           dbIns,
		logIns:          logIns,
		serverInfo:      serverInfo,
		containersvcIns: containersvcIns,
		svc:             svc,
		catalogSvcInit:  newCatalogServiceInit(cluster, containersvcIns, dbIns),
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
	trimURL := strings.TrimLeft(unescapedURL, "/")

	glog.Infoln("request Method", r.Method, "URL", r.URL, trimURL, "Host", r.Host, "requuid", requuid, "headers", r.Header)

	// make sure body is closed
	defer s.closeBody(r)

	ctx, cancel := context.WithCancel(context.Background())
	ctx = utils.NewRequestContext(ctx, requuid)
	// call cancel before return. This is to ensure any resource derived
	// from the context will be canceled.
	defer cancel()

	errmsg := ""
	errcode := http.StatusOK
	switch r.Method {
	case http.MethodPost:
		errmsg, errcode = s.putOp(ctx, w, r, trimURL, requuid)
	case http.MethodPut:
		errmsg, errcode = s.putOp(ctx, w, r, trimURL, requuid)
	case http.MethodGet:
		errmsg, errcode = s.getOp(ctx, w, r, trimURL, requuid)
	case http.MethodDelete:
		errmsg, errcode = s.delOp(ctx, w, r, trimURL, requuid)
	default:
		errmsg = http.StatusText(http.StatusNotImplemented)
		errcode = http.StatusNotImplemented
	}

	if errcode != http.StatusOK {
		http.Error(w, errmsg, errcode)
	}
}

// PUT/POST to do the service related operations. The necessary parameters should be
// passed in the http headers.
// Example:
//   PUT /servicename, create a service.
//   PUT /?SetServiceInitialized, mark a service initialized.
func (s *ManageHTTPServer) putOp(ctx context.Context, w http.ResponseWriter, r *http.Request, trimURL string, requuid string) (errmsg string, errcode int) {
	// check if the request is the catalog service request.
	if strings.HasPrefix(trimURL, manage.CatalogOpPrefix) {
		return s.putCatalogServiceOp(ctx, w, r, trimURL, requuid)
	}

	// check if the request is other special request.
	if strings.HasPrefix(trimURL, manage.SpecialOpPrefix) {
		switch trimURL {
		case manage.ServiceInitializedOp:
			return s.putServiceInitialized(ctx, w, r, requuid)
		case manage.RunTaskOp:
			return s.runTask(ctx, w, r, requuid)
		case manage.StopServiceOp:
			return s.stopService(ctx, w, r, requuid)
		case manage.StartServiceOp:
			return s.startService(ctx, w, r, requuid)
		default:
			return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
		}
	}

	// the common service creation request.
	return s.createService(ctx, w, r, requuid)
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
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("set service", service, "initialized, requuid", requuid)
	return "", http.StatusOK
}

func (s *ManageHTTPServer) checkCommonRequest(service *manage.ServiceCommonRequest) error {
	if !s.validName.MatchString(service.ServiceName) {
		return errors.New("invalid request, service name is not valid")
	}

	if service.Cluster != s.cluster || service.Region != s.region {
		return errors.New("invalid request, cluster or region not match")
	}

	return nil
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
		return manage.ConvertToHTTPError(err)
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
		return manage.ConvertToHTTPError(err)
	}

	attr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, req)
		return manage.ConvertToHTTPError(err)
	}

	err = s.containersvcIns.ScaleService(ctx, s.cluster, req.ServiceName, attr.Replicas)
	if err != nil {
		glog.Errorln("start container service error", err, "requuid", requuid, req)
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("started container service", req, "requuid", requuid)
	return "", http.StatusOK
}

func (s *ManageHTTPServer) createService(ctx context.Context, w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	// parse the request
	req := &manage.CreateServiceRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("createService decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	err = s.checkCommonRequest(req.Service)
	if err != nil {
		glog.Errorln("createService invalid request, local cluster", s.cluster, "region",
			s.region, "requuid", requuid, req.Service, "error", err)
		return err.Error(), http.StatusBadRequest
	}

	_, err = s.createCommonService(ctx, req, requuid)
	if err != nil {
		return manage.ConvertToHTTPError(err)
	}
	return "", http.StatusOK
}

func (s *ManageHTTPServer) createCommonService(ctx context.Context,
	req *manage.CreateServiceRequest, requuid string) (serviceUUID string, err error) {
	// create the service in the control plane
	serviceUUID, err = s.svc.CreateService(ctx, req, s.domain, s.vpcID)
	if err != nil {
		glog.Errorln("create service error", err, "requuid", requuid, req.Service)
		return "", err
	}

	return serviceUUID, s.createContainerService(ctx, req, serviceUUID, requuid)
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
		logConfig := s.logIns.CreateServiceLogConfig(ctx, s.cluster, req.Service.ServiceName, serviceUUID)
		opts := s.genCreateServiceOptions(req, serviceUUID, logConfig)
		err = s.containersvcIns.CreateService(ctx, opts)
		if err != nil {
			glog.Errorln("CreateService error", err, "requuid", requuid, req.Service)
			return err
		}
	}

	glog.Infoln("create service done, serviceUUID", serviceUUID, "requuid", requuid, req.Service)
	return nil
}

func (s *ManageHTTPServer) genCreateServiceOptions(req *manage.CreateServiceRequest,
	serviceUUID string, logConfig *cloudlog.LogConfig) *containersvc.CreateServiceOptions {
	commonOpts := &containersvc.CommonOptions{
		Cluster:        req.Service.Cluster,
		ServiceName:    req.Service.ServiceName,
		ServiceUUID:    serviceUUID,
		ContainerImage: req.ContainerImage,
		Resource:       req.Resource,
		LogConfig:      logConfig,
	}

	createOpts := &containersvc.CreateServiceOptions{
		Common: commonOpts,
		DataVolume: &containersvc.VolumeOptions{
			MountPath:  req.ContainerPath,
			VolumeType: req.Volume.VolumeType,
			SizeGB:     req.Volume.VolumeSizeGB,
			Iops:       req.Volume.Iops,
		},
		PortMappings: req.PortMappings,
		Replicas:     req.Replicas,
	}
	if req.JournalVolume != nil {
		createOpts.JournalVolume = &containersvc.VolumeOptions{
			MountPath:  req.JournalContainerPath,
			VolumeType: req.JournalVolume.VolumeType,
			SizeGB:     req.JournalVolume.VolumeSizeGB,
			Iops:       req.JournalVolume.Iops,
		}
	}

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

	return createOpts
}

// Get one service, GET /servicename. Or list services, Get / or /?list-type=1, and additional parameters in headers
func (s *ManageHTTPServer) getOp(ctx context.Context, w http.ResponseWriter,
	r *http.Request, trimURL string, requuid string) (errmsg string, errcode int) {
	// check if the request is the catalog service request.
	if strings.HasPrefix(trimURL, manage.CatalogOpPrefix) {
		return s.getCatalogServiceOp(ctx, w, r, requuid)
	}

	// check if the request is the internal service request.
	if strings.HasPrefix(trimURL, manage.InternalOpPrefix) {
		return s.internalGetOp(ctx, w, r, trimURL, requuid)
	}

	if strings.HasPrefix(trimURL, manage.SpecialOpPrefix) {
		switch trimURL {
		case manage.ListServiceOp:
			return s.listServices(ctx, w, r, requuid)

		case manage.ListServiceMemberOp:
			return s.listServiceMembers(ctx, w, r, requuid)

		case manage.GetServiceStatusOp:
			return s.getServiceStatus(ctx, w, r, requuid)

		case manage.GetConfigFileOp:
			return s.getConfigFile(ctx, w, r, requuid)

		case manage.GetTaskStatusOp:
			return s.getTaskStatus(ctx, w, r, requuid)

		default:
			return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
		}
	} else {
		// get the detail of one service
		return s.getServiceAttr(ctx, w, r, requuid)
	}
}

func (s *ManageHTTPServer) delOp(ctx context.Context, w http.ResponseWriter, r *http.Request, trimURL string, requuid string) (errmsg string, errcode int) {
	switch trimURL {
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
		return manage.ConvertToHTTPError(err)
	}

	err = s.containersvcIns.DeleteService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("delete service from container platform error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	// delete the service cloud log
	svc, err := s.dbIns.GetService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	err = s.logIns.DeleteServiceLogConfig(ctx, s.cluster, req.Service.ServiceName, svc.ServiceUUID)
	if err != nil {
		glog.Errorln("DeleteServiceLogConfig error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
	}

	// delete the service on the control plane
	// TODO support the possible concurrent service deletion and creation with the same name.
	volIDs, err := s.svc.DeleteService(ctx, s.cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("DeleteService error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
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
		return manage.ConvertToHTTPError(err)
	}

	var serviceAttrs []*common.ServiceAttr
	for _, service := range services {
		if len(req.Prefix) == 0 || strings.HasPrefix(service.ServiceName, req.Prefix) {
			// fetch the detail service attr
			attr, err := s.dbIns.GetServiceAttr(ctx, service.ServiceUUID)
			if err != nil {
				glog.Errorln("GetServiceAttr error", err, service, "requuid", requuid)
				return manage.ConvertToHTTPError(err)
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
		return manage.ConvertToHTTPError(err)
	}

	members, err := s.dbIns.ListServiceMembers(ctx, service.ServiceUUID)
	if err != nil {
		glog.Errorln("db ListServiceMembers error", err, "requuid", requuid, req.Service)
		return manage.ConvertToHTTPError(err)
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
		return manage.ConvertToHTTPError(err)
	}

	attr, err := s.dbIns.GetServiceAttr(ctx, service.ServiceUUID)
	if err != nil {
		glog.Errorln("GetServiceAttr error", err, service, "requuid", requuid)
		return manage.ConvertToHTTPError(err)
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
		return manage.ConvertToHTTPError(err)
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

func (s *ManageHTTPServer) getConfigFile(ctx context.Context,
	w http.ResponseWriter, r *http.Request, requuid string) (errmsg string, errcode int) {
	req := &manage.GetConfigFileRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("getConfigFile decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Cluster != s.cluster || req.Region != s.region {
		glog.Errorln("invalid request, local cluster", s.cluster, "region", s.region, "requuid", requuid, req)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	cfg, err := s.dbIns.GetConfigFile(ctx, req.ServiceUUID, req.FileID)
	if err != nil {
		glog.Errorln("GetConfigFile error", err, "requuid", requuid, req)
		return manage.ConvertToHTTPError(err)
	}

	resp := &manage.GetConfigFileResponse{
		ConfigFile: cfg,
	}

	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal error", err, "requuid", requuid, req)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	glog.Infoln("get config file", db.PrintConfigFile(cfg), "requuid", requuid, req)

	return "", http.StatusOK
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
		return manage.ConvertToHTTPError(err)
	}

	logConfig := s.logIns.CreateLogConfigForStream(ctx, s.cluster, req.Service.ServiceName, svc.ServiceUUID, req.TaskType)

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
		return manage.ConvertToHTTPError(err)
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
		return manage.ConvertToHTTPError(err)
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
		return manage.ConvertToHTTPError(err)
	}

	glog.Infoln("deleted task, requuid", requuid, "TaskType", req.TaskType, req.Service)
	return "", http.StatusOK
}
