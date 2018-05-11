package managesvc

import (
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/pkg/containersvc"
	"github.com/cloudstax/firecamp/pkg/db"
	"github.com/cloudstax/firecamp/pkg/dns"
	"github.com/cloudstax/firecamp/pkg/log"
	"github.com/cloudstax/firecamp/pkg/server"
	"github.com/cloudstax/firecamp/pkg/utils"
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

	// register dns
	ctx := context.Background()
	private := true
	vpcID := serverInfo.GetLocalVpcID()
	vpcRegion := serverInfo.GetLocalRegion()
	hostedZoneID, err := dnsIns.GetOrCreateHostedZoneIDByName(ctx, domain, vpcID, vpcRegion, private)
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
	serv := NewManageHTTPServer(platform, cluster, azs, dnsname, dbIns, dnsIns, logIns, serverIns, serverInfo, containersvcIns)

	// listen on all ips, as managesvc runs inside the container
	addr := ":" + strconv.Itoa(managePort)
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
