package controldbcli

import (
	"flag"
	"strconv"
	"testing"
	"time"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage/service"
	"github.com/cloudstax/firecamp/server"
)

func TestServiceWithControlDBWithStaticIP(t *testing.T) {
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
	csvcIns := containersvc.NewMemContainerSvc()
	s := manageservice.NewManageService(dbcli, serverInfo, serverIns, dnsIns, csvcIns)

	requireStaticIP := true
	manageservice.TestUtil_ServiceCreation(t, s, dbcli, serverIns, requireStaticIP)
}

func TestServiceWithControlDBWithoutStaticIP(t *testing.T) {
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
	csvcIns := containersvc.NewMemContainerSvc()
	s := manageservice.NewManageService(dbcli, serverInfo, serverIns, dnsIns, csvcIns)

	requireStaticIP := false
	manageservice.TestUtil_ServiceCreation(t, s, dbcli, serverIns, requireStaticIP)
}

func TestServiceCreationRetryWithControlDBWithStaticIP(t *testing.T) {
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
	csvcIns := containersvc.NewMemContainerSvc()
	s := manageservice.NewManageService(dbcli, serverInfo, serverIns, dnsIns, csvcIns)

	requireStaticIP := true
	requireJournalVolume := true
	manageservice.TestUtil_ServiceCreationRetry(t, s, dbcli, dnsIns, serverIns, requireStaticIP, requireJournalVolume)
}

func TestServiceCreationRetryWithControlDBWithoutStaticIP(t *testing.T) {
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
	csvcIns := containersvc.NewMemContainerSvc()
	s := manageservice.NewManageService(dbcli, serverInfo, serverIns, dnsIns, csvcIns)

	requireStaticIP := false
	requireJournalVolume := false
	manageservice.TestUtil_ServiceCreationRetry(t, s, dbcli, dnsIns, serverIns, requireStaticIP, requireJournalVolume)
}
