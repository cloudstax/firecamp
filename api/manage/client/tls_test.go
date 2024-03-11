package client

import (
	"flag"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/jazzl0ver/firecamp/pkg/containersvc"
	"github.com/jazzl0ver/firecamp/pkg/db"
	"github.com/jazzl0ver/firecamp/pkg/dns"
	"github.com/jazzl0ver/firecamp/pkg/log/jsonfile"
	"github.com/jazzl0ver/firecamp/pkg/managesvc"
	"github.com/jazzl0ver/firecamp/pkg/server"
	"github.com/jazzl0ver/firecamp/pkg/utils"
)

func TestTLSMgrOperationsWithMemDB(t *testing.T) {
	flag.Parse()
	//flag.Set("stderrthreshold", "INFO")

	cluster := "cluster1"
	azs := []string{"us-east-1a", "us-east-1b", "us-east-1c"}
	manageurl := dns.GetDefaultManageServiceDNSName(cluster)
	dbIns := db.NewMemDB()
	dnsIns := dns.NewMockDNS()
	logIns := jsonfilelog.NewLog()
	serverIns := server.NewMemServer()
	serverInfo := server.NewMockServerInfo()
	containersvcIns := containersvc.NewMemContainerSvc()

	mgtsvc := managesvc.NewManageHTTPServer(common.ContainerPlatformECS, cluster, azs,
		manageurl, dbIns, dnsIns, logIns, serverIns, serverInfo, containersvcIns)
	// listen on a different port. not sure why, but when simply go test to run both client_test and tls_test,
	// tls_test failed with like "server gave HTTP response to HTTPS client".
	addr := "127.0.0.1:" + strconv.Itoa(common.ManageHTTPServerPort+2)

	caFile := "exampletls/ca.pem"
	scertFile := "exampletls/server-cert.pem"
	skeyFile := "exampletls/server-key.pem"

	tlsConf, err := utils.GenServerTLSConfigFromCAFile(caFile)
	if err != nil {
		t.Fatalf("GenServerTLSConfigFromCAFile error %s, caFile %s", err, caFile)
	}

	s := &http.Server{
		Addr:      addr,
		Handler:   mgtsvc,
		TLSConfig: tlsConf,
	}
	go s.ListenAndServeTLS(scertFile, skeyFile)

	// sleep 2s to make sure http server starts listening
	time.Sleep(2 * time.Second)
	certFile := "exampletls/cert.pem"
	keyFile := "exampletls/key.pem"
	cliTLSConf, err := utils.GenClientTLSConfig(caFile, certFile, keyFile)
	if err != nil {
		t.Fatalf("GenClientTLSConfig error %s, %s %s %s", err, caFile, certFile, keyFile)
	}

	surl := "https://" + addr + "/"
	cli := NewManageClient(surl, cliTLSConf)
	serviceNum := 17
	testMgrOps(t, cli, cluster, serverInfo, serviceNum)
}
