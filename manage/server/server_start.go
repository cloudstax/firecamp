package manageserver

import (
	"flag"
	"net/http"
	"strconv"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/containersvc"
	"github.com/cloudstax/openmanage/db"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/server"
	"github.com/cloudstax/openmanage/utils"
)

func StartServer(cluster string, azs []string, manageDNSName string, managePort int,
	containersvcIns containersvc.ContainerSvc, dbIns db.DB, dnsIns dns.DNS, serverInfo server.Info,
	serverIns server.Server, tlsEnabled bool, caFile, certFile, keyFile string) error {
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
	serv := NewManageHTTPServer(cluster, azs, dnsname, dbIns, dnsIns, serverIns, serverInfo, containersvcIns)

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
