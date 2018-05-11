package catalogsvc

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/catalog"
	"github.com/cloudstax/firecamp/api/manage"
	manageclient "github.com/cloudstax/firecamp/api/manage/client"
	"github.com/cloudstax/firecamp/api/manage/error"
	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/pkg/dns"
	"github.com/cloudstax/firecamp/pkg/log"
	"github.com/cloudstax/firecamp/pkg/utils"
)

const (
	// The max concurrent service tasks. 100 would be enough.
	maxTaskCounts = 100
)

// The CatalogHTTPServer is the management http server for the service management.
// It will run in a container, publish a well-known DNS name, which could be accessed
// publicly or privately depend on the customer.
//
// The service creation needs to talk to DB (dynamodb, etc), which is
// accessable inside the cloud platform (aws, etc).
// The CatalogHTTPServer will accept the calls from the admin, and talk with DB. This also
// enhance the security. The CatalogHTTPServer REST APIs are the only exposed access to
// the cluster.
//
// AWS VPC is the region wide concept. One VPC could cross all AZs of the region.
// The Route53 HostedZone is global concept, one VPC could associate with multiple VPCs.
//
// For the stateful application across multiple Regions, we will have the federation mode.
// Each Region has its own container cluster. Each cluster has its own DB service and
// hosted zone. One federation HostedZone is created for all clusters.
// Note: the federation HostedZone could include multiple VPCs at multiple Regions.
type CatalogHTTPServer struct {
	// container platform
	platform  string
	region    string
	cluster   string
	serverurl string
	azs       []string
	domain    string
	vpcID     string

	validName *regexp.Regexp

	managecli      *manageclient.ManageClient
	catalogSvcInit *catalogServiceInit

	logIns cloudlog.CloudLog
}

// NewCatalogHTTPServer creates a CatalogHTTPServer instance
func NewCatalogHTTPServer(region string, vpcID string, platform string, cluster string,
	azs []string, serverdns string, logIns cloudlog.CloudLog) *CatalogHTTPServer {
	manageurl := dns.GetDefaultManageServiceURL(cluster, false)
	cli := manageclient.NewManageClient(manageurl, nil)
	s := &CatalogHTTPServer{
		platform:       platform,
		region:         region,
		cluster:        cluster,
		serverurl:      dns.GetCatalogServiceURL(serverdns, false),
		azs:            azs,
		domain:         dns.GenDefaultDomainName(cluster),
		vpcID:          vpcID,
		validName:      regexp.MustCompile(common.ServiceNamePattern),
		managecli:      cli,
		catalogSvcInit: newCatalogServiceInit(region, cluster, cli),
		logIns:         logIns,
	}
	return s
}

func (s *CatalogHTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	// TODO currently the catalog service operations are included in the same management
	//      container. would be better to separate the catalog services into another container.
	errmsg := ""
	errcode := http.StatusOK
	switch r.Method {
	case http.MethodPost:
		errmsg, errcode = s.putOp(ctx, w, r, trimURL, requuid)
	case http.MethodPut:
		errmsg, errcode = s.putOp(ctx, w, r, trimURL, requuid)
	case http.MethodGet:
		errmsg, errcode = s.getOp(ctx, w, r, trimURL, requuid)
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
func (s *CatalogHTTPServer) putOp(ctx context.Context, w http.ResponseWriter, r *http.Request, trimURL string, requuid string) (errmsg string, errcode int) {
	// check if the request is the catalog service request.
	if strings.HasPrefix(trimURL, catalog.CatalogOpPrefix) {
		err := s.putCatalogServiceOp(ctx, w, r, trimURL, requuid)
		if err != nil {
			_, ok := err.(clienterr.Error)
			if ok {
				return err.(clienterr.Error).Error(), err.(clienterr.Error).Code()
			}
			glog.Errorln("putCatalogServiceOp returns non clienterr", err, "requuid", requuid, trimURL, r)
			return err.Error(), http.StatusInternalServerError
		}
		return "", http.StatusOK
	}

	return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
}

func (s *CatalogHTTPServer) getOp(ctx context.Context, w http.ResponseWriter,
	r *http.Request, trimURL string, requuid string) (errmsg string, errcode int) {
	// check if the request is the catalog service request.
	if strings.HasPrefix(trimURL, catalog.CatalogOpPrefix) {
		err := s.getCatalogServiceOp(ctx, w, r, requuid)
		if err != nil {
			_, ok := err.(clienterr.Error)
			if ok {
				return err.(clienterr.Error).Error(), err.(clienterr.Error).Code()
			}
			glog.Errorln("getCatalogServiceOp returns non clienterr", err, "requuid", requuid, trimURL, r)
			return err.Error(), http.StatusInternalServerError
		}
		return "", http.StatusOK
	}

	return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
}

func (s *CatalogHTTPServer) closeBody(r *http.Request) {
	if r.Body != nil {
		r.Body.Close()
	}
}

func (s *CatalogHTTPServer) checkRequest(service *manage.ServiceCommonRequest, res *common.Resources) error {
	if res.MaxMemMB != common.DefaultMaxMemoryMB && res.MaxMemMB < res.ReserveMemMB {
		return clienterr.New(http.StatusBadRequest, "Invalid request, max-memory should be larger than reserve-memory")
	}

	if res.MaxCPUUnits != common.DefaultMaxCPUUnits && res.MaxCPUUnits < res.ReserveCPUUnits {
		return clienterr.New(http.StatusBadRequest, "Invalid request, max-cpuunits should be larger than reserve-cpuunits")
	}

	return s.checkCommonRequest(service)
}

func (s *CatalogHTTPServer) checkCommonRequest(service *manage.ServiceCommonRequest) error {
	if !s.validName.MatchString(service.ServiceName) {
		return clienterr.New(http.StatusBadRequest, "Invalid request, service name is not valid")
	}

	if len(service.ServiceName) > common.MaxServiceNameLength {
		return clienterr.New(http.StatusBadRequest, fmt.Sprintf("Invalid Request, service name max length is %d", common.MaxServiceNameLength))
	}

	if service.Cluster != s.cluster || service.Region != s.region {
		return clienterr.New(http.StatusBadRequest, "Invalid request, cluster or region not match")
	}

	return nil
}
