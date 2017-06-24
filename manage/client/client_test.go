package client

import (
	"flag"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/containersvc"
	"github.com/cloudstax/openmanage/db"
	"github.com/cloudstax/openmanage/db/awsdynamodb"
	"github.com/cloudstax/openmanage/db/controldb/client"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/manage"
	"github.com/cloudstax/openmanage/manage/server"
	"github.com/cloudstax/openmanage/server"
	"github.com/cloudstax/openmanage/utils"
)

var region = flag.String("region", "us-west-1", "The target AWS region for DynamoDB")

func TestClientMgrOperationsWithMemDB(t *testing.T) {
	flag.Parse()
	//flag.Set("stderrthreshold", "FATAL")

	cluster := "cluster1"
	azs := []string{"us-east-1a", "us-east-1b", "us-east-1c"}
	manageurl := dns.GetDefaultManageServiceDNSName(cluster)
	dbIns := db.NewMemDB()
	dnsIns := dns.NewMockDNS()
	serverIns := server.NewMemServer()
	serverInfo := server.NewMockServerInfo()
	containersvcIns := containersvc.NewMemContainerSvc()

	mgtsvc := manageserver.NewManageHTTPServer(cluster, azs, manageurl, dbIns, dnsIns, serverIns, serverInfo, containersvcIns)
	addr := "localhost:" + strconv.Itoa(common.ManageHTTPServerPort)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("failed to listen on addr", addr, "error", err)
	}

	s := &http.Server{
		Addr:    addr,
		Handler: mgtsvc,
	}

	go s.Serve(lis)

	tlsEnabled := false
	surl := dns.FormatManageServiceURL(addr, tlsEnabled)
	cli := NewManageClient(surl, nil)
	serviceNum := 23
	testMgrOps(t, cli, cluster, serverInfo, serviceNum)

	lis.Close()
}

func TestClientMgrOperationsWithControlDB(t *testing.T) {
	flag.Parse()
	//flag.Set("stderrthreshold", "INFO")

	testdir := "/tmp/test-" + strconv.FormatInt((time.Now().UnixNano()), 10)
	cluster := "cluster1"
	azs := []string{"us-east-1a", "us-east-1b", "us-east-1c"}
	manageurl := dns.GetDefaultManageServiceDNSName(cluster)

	testdb := &controldbcli.TestControlDBServer{Testdir: testdir, ListenPort: common.ControlDBServerPort + 1}
	go testdb.RunControldbTestServer(cluster)
	defer testdb.StopControldbTestServer()

	dbcli := controldbcli.NewControlDBCli("localhost:" + strconv.Itoa(common.ControlDBServerPort+1))
	dnsIns := dns.NewMockDNS()
	serverIns := server.NewMemServer()
	serverInfo := server.NewMockServerInfo()
	containersvcIns := containersvc.NewMemContainerSvc()

	mgtsvc := manageserver.NewManageHTTPServer(cluster, azs, manageurl, dbcli, dnsIns, serverIns, serverInfo, containersvcIns)
	addr := "localhost:" + strconv.Itoa(common.ManageHTTPServerPort+1)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("failed to listen on addr", addr, "error", err)
	}

	s := &http.Server{
		Addr:    addr,
		Handler: mgtsvc,
	}

	go s.Serve(lis)

	tlsEnabled := false
	surl := dns.FormatManageServiceURL(addr, tlsEnabled)
	cli := NewManageClient(surl, nil)
	serviceNum := 14
	testMgrOps(t, cli, cluster, serverInfo, serviceNum)

	lis.Close()
}

func TestClientMgrOperationsWithDynamoDB(t *testing.T) {
	flag.Parse()
	//flag.Set("stderrthreshold", "INFO")

	config := aws.NewConfig().WithRegion(*region)
	sess, err := session.NewSession(config)
	if err != nil {
		t.Fatalf("create aws session error", err, *region)
	}

	ctx := context.Background()

	tableNameSuffix := utils.GenUUID()
	dbIns := awsdynamodb.NewTestDynamoDB(sess, tableNameSuffix)
	err = dbIns.CreateSystemTables(ctx)
	defer dbIns.DeleteSystemTables(ctx)
	defer dbIns.WaitSystemTablesDeleted(ctx, 120)
	if err != nil {
		t.Fatalf("create system table error", err, "region", *region, "tableNameSuffix", tableNameSuffix)
	}

	err = dbIns.WaitSystemTablesReady(ctx, 120)
	if err != nil {
		t.Fatalf("WaitSystemTablesReady error", err)
	}

	dnsIns := dns.NewMockDNS()
	serverIns := server.NewMemServer()
	serverInfo := server.NewMockServerInfo()
	containersvcIns := containersvc.NewMemContainerSvc()

	cluster := "cluster1"
	azs := []string{"us-east-1a", "us-east-1b", "us-east-1c"}
	manageurl := dns.GetDefaultManageServiceDNSName(cluster)
	mgtsvc := manageserver.NewManageHTTPServer(cluster, azs, manageurl, dbIns, dnsIns, serverIns, serverInfo, containersvcIns)
	addr := "localhost:" + strconv.Itoa(common.ManageHTTPServerPort+1)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("failed to listen on addr", addr, "error", err)
	}

	s := &http.Server{
		Addr:    addr,
		Handler: mgtsvc,
	}

	go s.Serve(lis)

	tlsEnabled := false
	surl := dns.FormatManageServiceURL(addr, tlsEnabled)
	cli := NewManageClient(surl, nil)
	serviceNum := 6
	testMgrOps(t, cli, cluster, serverInfo, serviceNum)

	lis.Close()
	dbIns.WaitSystemTablesDeleted(ctx, 120)
}

func testMgrOps(t *testing.T, cli *ManageClient, cluster string, serverInfo server.Info, serviceNum int) {
	// create services
	hasMembership := true
	servicePrefix := "service-"
	for repNum := 1; repNum < serviceNum+1; repNum++ {
		service := servicePrefix + strconv.Itoa(repNum)

		replicaCfgs := make([]*manage.ReplicaConfig, repNum)
		for i := 0; i < repNum; i++ {
			cfg := &manage.ReplicaConfigFile{FileName: service, Content: service}
			configs := []*manage.ReplicaConfigFile{cfg}
			replicaCfg := &manage.ReplicaConfig{Zone: "us-west-1c", Configs: configs}
			replicaCfgs[i] = replicaCfg
		}

		r := &manage.CreateServiceRequest{
			Service: &manage.ServiceCommonRequest{
				Region:      serverInfo.GetLocalRegion(),
				Cluster:     cluster,
				ServiceName: service,
			},
			Resource: &common.Resources{
				MaxCPUUnits:     2,
				ReserveCPUUnits: 2,
				MaxMemMB:        2,
				ReserveMemMB:    2,
			},

			ContainerImage: "image",
			Replicas:       int64(repNum),
			VolumeSizeGB:   int64(repNum),
			ContainerPath:  "",
			HasMembership:  hasMembership,
			ReplicaConfigs: replicaCfgs,
		}

		err := cli.CreateService(context.Background(), r)
		if err != nil {
			t.Fatalf("create service error")
		}
	}

	// list services with and without prefix
	listServicesTest(t, cli, serviceNum, "", cluster, serverInfo)
	listServicesTest(t, cli, serviceNum, servicePrefix, cluster, serverInfo)
	// negative case: list non-exist prefix
	listServicesTest(t, cli, 0, "xxxx", cluster, serverInfo)

	// query service
	for i := 1; i < serviceNum+1; i++ {
		queryServiceTest(t, cli, cluster, servicePrefix, serverInfo, i, common.ServiceStatusInitializing)
	}

	// negative case: get non-exist service
	r1 := &manage.ServiceCommonRequest{
		Region:      serverInfo.GetLocalRegion(),
		Cluster:     cluster,
		ServiceName: "xxxx",
	}
	_, err := cli.GetServiceAttr(context.Background(), r1)
	if err != common.ErrNotFound {
		t.Fatalf("get non-exist service, expect StatusNotFound, got %s, %s", err, r1)
	}

	// set service initialized
	for i := 1; i < serviceNum+1; i++ {
		s1 := servicePrefix + strconv.Itoa(i)

		r := &manage.ServiceCommonRequest{
			Region:      serverInfo.GetLocalRegion(),
			Cluster:     cluster,
			ServiceName: s1,
		}

		err = cli.SetServiceInitialized(context.Background(), r)
		if err != nil {
			t.Fatalf("SetServiceInitialized error %s %s", err, s1)
		}
	}

	// query service
	for i := 1; i < serviceNum+1; i++ {
		queryServiceTest(t, cli, cluster, servicePrefix, serverInfo, i, common.ServiceStatusActive)
	}

	// delete 1/5 service
	delNum := 0
	for i := 1; i < serviceNum+1; i += 5 {
		s1 := servicePrefix + strconv.Itoa(i)
		r := &manage.ServiceCommonRequest{
			Region:      serverInfo.GetLocalRegion(),
			Cluster:     cluster,
			ServiceName: s1,
		}

		err := cli.DeleteService(context.Background(), r)
		if err != nil {
			t.Fatalf("delete service error %s, %s", err, r)
		}

		delNum++
	}
	// list services again
	listServicesTest(t, cli, serviceNum-delNum, "", cluster, serverInfo)

	// negative case: delete non-exist service
	r2 := &manage.ServiceCommonRequest{
		Region:      serverInfo.GetLocalRegion(),
		Cluster:     cluster,
		ServiceName: "xxxx",
	}

	err = cli.DeleteService(context.Background(), r2)
	if err != common.ErrNotFound {
		t.Fatalf("delete service error %s, %s", err, r2)
	}
}

func listServicesTest(t *testing.T, cli *ManageClient, serviceNum int, prefix string, cluster string, serverInfo server.Info) {
	r := &manage.ListServiceRequest{
		Region:  serverInfo.GetLocalRegion(),
		Cluster: cluster,
		Prefix:  prefix,
	}

	serviceAttrs, err := cli.ListService(context.Background(), r)
	if err != nil {
		t.Fatalf("ListService error %s, %s", err, r)
	}
	if len(serviceAttrs) != serviceNum {
		t.Fatalf("ListService expect %d services, got %d, %s", serviceNum, len(serviceAttrs), serviceAttrs)
	}
	glog.Infoln("ListService output", serviceAttrs)
}

func queryServiceTest(t *testing.T, cli *ManageClient, cluster string, servicePrefix string, serverInfo server.Info, i int, targetServiceStatus string) {
	s1 := servicePrefix + strconv.Itoa(i)

	r := &manage.ServiceCommonRequest{
		Region:      serverInfo.GetLocalRegion(),
		Cluster:     cluster,
		ServiceName: s1,
	}

	attr, err := cli.GetServiceAttr(context.Background(), r)
	if err != nil {
		t.Fatalf("GetServiceAttr error %s, %s", err, r)
	}

	if attr.ServiceName != s1 || attr.ServiceStatus != targetServiceStatus ||
		attr.Replicas != int64(i) || attr.VolumeSizeGB != int64(i) {
		t.Fatalf("expect service %s status %s TaskCounts %d VolumeSize %d, got %s", s1, targetServiceStatus, i, i, attr)
	}
	glog.Infoln("GetServiceAttr output", attr)
}
