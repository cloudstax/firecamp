package catalogsvc

import (
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/log"
	"github.com/cloudstax/firecamp/server"
	"github.com/cloudstax/firecamp/utils"
)

// StartServer creates the catalog http server and listen for requests.
func StartServer(platform string, cluster string, azs []string, serverDNSName string, listenPort int,
	dnsIns dns.DNS, logIns cloudlog.CloudLog, serverInfo server.Info,
	tlsEnabled bool, caFile, certFile, keyFile string) error {
	// register the management service dnsname
	dnsname := serverDNSName
	domain := dns.GenDefaultDomainName(cluster)
	var err error
	if len(dnsname) == 0 {
		// dnsname not specified, use the default
		dnsname = dns.GenDNSName(common.CatalogServiceName, domain)
	} else {
		domain, err = dns.GetDomainNameFromDNSName(dnsname)
		if err != nil {
			glog.Errorln("bad management dnsname", serverDNSName)
			return err
		}
	}

	// register dns
	ctx := context.Background()
	private := true
	region := serverInfo.GetLocalRegion()
	vpcID := serverInfo.GetLocalVpcID()
	hostedZoneID, err := dnsIns.GetOrCreateHostedZoneIDByName(ctx, domain, vpcID, region, private)
	if err != nil {
		glog.Errorln("GetOrCreateHostedZoneIDByName error", err, "domain", domain, "vpcID", vpcID)
		return err
	}

	privateIP := serverInfo.GetPrivateIP()
	err = dnsIns.UpdateDNSRecord(ctx, dnsname, privateIP, hostedZoneID)
	if err != nil {
		glog.Errorln("UpdateDNSRecord error", err, "domain", domain, "dnsname", dnsname)
		return err
	}

	// create the management http server
	serv := NewCatalogHTTPServer(region, vpcID, platform, cluster, azs, dnsname, logIns)

	// listen on all ips, as managesvc runs inside the container
	addr := ":" + strconv.Itoa(listenPort)
	s := &http.Server{
		Addr:    addr,
		Handler: serv,
	}

	if tlsEnabled && caFile != "" {
		// load ca file
		tlsConf, err := utils.GenServerTLSConfigFromCAFile(caFile)
		if err != nil {
			glog.Errorln("GenServerTLSConfigFromCAFile error", err, caFile)
			return err
		}
		s.TLSConfig = tlsConf
	}

	go func() {
		glog.Infoln("start the management service for cluster", cluster, azs, "on", addr,
			"tlsEnabled", tlsEnabled, "ca file", caFile, "cert file", certFile, "key file", keyFile)

		if tlsEnabled {
			err = s.ListenAndServeTLS(certFile, keyFile)
		}
		err = s.ListenAndServe()

		if err != http.ErrServerClosed {
			glog.Fatalln("ListenAndServe returns error", err)
		}
	}()

	// graceful stop
	stop := make(chan os.Signal, 1)

	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	glog.Infoln("Shutdown the management http server")

	// delete the dns record
	err = dnsIns.DeleteDNSRecord(ctx, dnsname, privateIP, hostedZoneID)
	glog.Infoln("DeleteDNSRecord domain", domain, "dnsname", dnsname, "hostedZone", hostedZoneID, "error", err)

	// wait a few seconds, sometimes the CloudFormation stack deletion fails at deleting Route53HostedZone,
	// while the record is actually deleted when checks manually. Probably, route53 needs some time to actually delete the record.
	time.Sleep(8 * time.Second)

	return s.Shutdown(ctx)
}
