package controldbcli

import (
	"flag"
	"strconv"
	"testing"
	"time"

	"github.com/openconnectio/openmanage/common"
	"github.com/openconnectio/openmanage/dns"
	"github.com/openconnectio/openmanage/manage/service"
	"github.com/openconnectio/openmanage/server"
)

func TestServiceWithControlDB(t *testing.T) {
	flag.Parse()
	flag.Set("stderrthreshold", "INFO")

	testdir := "/tmp/test-" + strconv.FormatInt((time.Now().UnixNano()), 10)
	cluster := "cluster1"

	dbs := &TestControlDBServer{Testdir: testdir, ListenPort: common.ControlDBServerPort + 1}
	go dbs.RunControldbTestServer(cluster)
	defer dbs.StopControldbTestServer()

	dbcli := NewControlDBCli("localhost:" + strconv.Itoa(common.ControlDBServerPort+1))
	serverIns := server.NewLoopServer()
	dnsIns := dns.NewMockDNS()
	s := manageservice.NewSCService(dbcli, serverIns, dnsIns)
	manageservice.TestUtil_ServiceCreateion(t, s, dbcli)
}

func TestServiceCreationRetryWithControlDB(t *testing.T) {
	flag.Parse()
	flag.Set("stderrthreshold", "INFO")

	testdir := "/tmp/test-" + strconv.FormatInt((time.Now().UnixNano()), 10)
	cluster := "cluster1"

	dbs := &TestControlDBServer{Testdir: testdir, ListenPort: common.ControlDBServerPort + 2}
	go dbs.RunControldbTestServer(cluster)
	defer dbs.StopControldbTestServer()

	dbcli := NewControlDBCli("localhost:" + strconv.Itoa(common.ControlDBServerPort+2))
	serverIns := server.NewLoopServer()
	dnsIns := dns.NewMockDNS()
	s := manageservice.NewSCService(dbcli, serverIns, dnsIns)
	manageservice.TestUtil_ServiceCreationRetry(t, s, dbcli, dnsIns)
}
