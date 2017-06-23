package client

import (
	"flag"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/containersvc"
	"github.com/cloudstax/openmanage/db"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/manage/server"
	"github.com/cloudstax/openmanage/server"
	"github.com/cloudstax/openmanage/utils"
)

func TestTLSMgrOperationsWithMemDB(t *testing.T) {
	flag.Parse()
	//flag.Set("stderrthreshold", "INFO")

	cluster := "cluster1"
	azs := []string{"us-east-1a", "us-east-1b", "us-east-1c"}
	manageurl := dns.GetDefaultManageServiceDNSName(cluster)
	dbIns := db.NewMemDB()
	dnsIns := dns.NewMockDNS()
	serverIns := server.NewMemServer()
	serverInfo := server.NewMockServerInfo()
	containersvcIns := containersvc.NewMemContainerSvc()

	mgtsvc := manageserver.NewManageHTTPServer(cluster, azs, manageurl, dbIns, dnsIns, serverIns, serverInfo, containersvcIns)
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
