package controldbcli

import (
	"flag"
	"strconv"
	"testing"
	"time"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage/service"
	"github.com/cloudstax/firecamp/server"
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
	serverInfo := server.NewMockServerInfo()
	dnsIns := dns.NewMockDNS()
	s := manageservice.NewManageService(dbcli, serverInfo, serverIns, dnsIns)
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
	serverInfo := server.NewMockServerInfo()
	dnsIns := dns.NewMockDNS()
	s := manageservice.NewManageService(dbcli, serverInfo, serverIns, dnsIns)
	manageservice.TestUtil_ServiceCreationRetry(t, s, dbcli, dnsIns)
}
