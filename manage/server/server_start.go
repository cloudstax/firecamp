package manageserver

import (
	"flag"
	"net/http"
	"strconv"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/log"
	"github.com/cloudstax/firecamp/server"
	"github.com/cloudstax/firecamp/utils"
)

// StartServer creates the manage http server and listen for requests.
func StartServer(platform string, cluster string, azs []string, manageDNSName string, managePort int,
	containersvcIns containersvc.ContainerSvc, dbIns db.DB, dnsIns dns.DNS, logIns cloudlog.CloudLog,
	serverInfo server.Info, serverIns server.Server, tlsEnabled bool, caFile, certFile, keyFile string) error {
	// register the management service dnsname
	dnsname := manageDNSName
	domain := dns.GenDefaultDomainName(cluster)
	var err error
	if len(dnsname) == 0 {
		// dnsname not specified, use the default
		dnsname = dns.GenDNSName(common.ManageServiceName, domain)
	} else {
		domain, err = dns.GetDomainNameFromDNSName(dnsname)
		if err != nil {
			glog.Errorln("bad management dnsname", manageDNSName)
			return err
		}
	}

	err = dns.RegisterDNSName(context.Background(), domain, dnsname, serverInfo, dnsIns)
	if err != nil {
		glog.Errorln("RegisterDNSName error", err, "domain", domain, "dnsname", dnsname)
		return err
	}

	// create the management http server
	serv := NewManageHTTPServer(platform, cluster, azs, dnsname, dbIns, dnsIns, logIns, serverIns, serverInfo, containersvcIns)

	// listen on all ips, as manageserver runs inside the container
	addr := ":" + strconv.Itoa(managePort)
	s := &http.Server{
		Addr:    addr,
		Handler: serv,
		//ReadTimeout:    10 * time.Second,
		//WriteTimeout:   10 * time.Second,
		//TLSConfig:
		//ErrorLog:
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

	glog.Infoln("start the management service for cluster", cluster, azs, "on", addr,
		"tlsEnabled", tlsEnabled, "ca file", caFile, "cert file", certFile, "key file", keyFile)
	// not log error to std
	flag.Set("stderrthreshold", "FATAL")

	if tlsEnabled {
		return s.ListenAndServeTLS(certFile, keyFile)
	}
	return s.ListenAndServe()
}
