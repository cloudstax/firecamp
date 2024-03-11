package managesvc

import (
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/jazzl0ver/firecamp/pkg/containersvc"
	"github.com/jazzl0ver/firecamp/pkg/db"
	"github.com/jazzl0ver/firecamp/pkg/dns"
	"github.com/jazzl0ver/firecamp/pkg/server"
	"github.com/jazzl0ver/firecamp/pkg/server/awsec2"
)

// TODO more test to cover every function in service.go
func TestDeviceName(t *testing.T) {
	cfg := aws.NewConfig().WithRegion("us-west-1")
	sess, err := session.NewSession(cfg)
	if err != nil {
		glog.Errorln("failed to create session, error", err)
		os.Exit(-1)
	}

	dbIns := db.NewMemDB()
	serverInfo := server.NewMockServerInfo()
	serverIns := awsec2.NewAWSEc2(sess)
	dnsIns := dns.NewMockDNS()
	csvcIns := containersvc.NewMemContainerSvc()
	s := NewManageService(dbIns, serverInfo, serverIns, dnsIns, csvcIns)

	ctx := context.Background()

	cluster := "cluster1"
	servicePrefix := "service-"

	// /dev/xvdf ~ xvdz
	devSeq := "f"
	for i := 0; i < 21; i++ {
		service := servicePrefix + strconv.Itoa(i)
		expectDev := "/dev/xvd" + devSeq
		dev, err := s.createDevice(ctx, cluster, service, "", "requuid")
		if err != nil || dev != expectDev {
			t.Fatalf("assignDeviceName failed, expectDev %s, dev %s, error %s", expectDev, dev, err)
		}
		devSeq = string(devSeq[0] + 1)
	}

	// /dev/xvdba ~ z
	devSeq = "a"
	for i := 0; i < 26; i++ {
		service := servicePrefix + "b" + strconv.Itoa(i)
		expectDev := "/dev/xvdb" + devSeq
		dev, err := s.createDevice(ctx, cluster, service, "", "requuid")
		if err != nil || dev != expectDev {
			t.Fatalf("assignDeviceName failed, expectDev %s, dev %s, error %s", expectDev, dev, err)
		}
		devSeq = string(devSeq[0] + 1)
	}

	// /dev/xvdca ~ z
	devSeq = "a"
	for i := 0; i < 26; i++ {
		service := servicePrefix + "c" + strconv.Itoa(i)
		expectDev := "/dev/xvdc" + devSeq
		dev, err := s.createDevice(ctx, cluster, service, "", "requuid")
		if err != nil || dev != expectDev {
			t.Fatalf("assignDeviceName failed, expectDev %s, dev %s, error %s", expectDev, dev, err)
		}
		devSeq = string(devSeq[0] + 1)
	}

	// create device item for the existing service
	service := servicePrefix + "b0"
	dev, err := s.createDevice(ctx, cluster, service, "", "requuid")
	if err != nil || dev != "/dev/xvdba" {
		t.Fatalf("create service %s again, expectDev /dev/xvdba, get %s, error %s", service, dev, err)
	}
}

func TestServiceCreationWithStaticIP(t *testing.T) {
	dbIns := db.NewMemDB()
	serverInfo := server.NewMockServerInfo()
	serverIns := server.NewLoopServer()
	dnsIns := dns.NewMockDNS()
	csvcIns := containersvc.NewMemContainerSvc()
	s := NewManageService(dbIns, serverInfo, serverIns, dnsIns, csvcIns)

	requireStaticIP := true
	TestUtil_ServiceCreation(t, s, dbIns, serverIns, requireStaticIP)
}

func TestServiceCreationWithoutStaticIP(t *testing.T) {
	dbIns := db.NewMemDB()
	serverInfo := server.NewMockServerInfo()
	serverIns := server.NewLoopServer()
	dnsIns := dns.NewMockDNS()
	csvcIns := containersvc.NewMemContainerSvc()
	s := NewManageService(dbIns, serverInfo, serverIns, dnsIns, csvcIns)

	requireStaticIP := false
	TestUtil_ServiceCreation(t, s, dbIns, serverIns, requireStaticIP)
}

func TestServiceCreationRetryWithStaticIP(t *testing.T) {
	// special tests to simulate the failure and retry
	dbIns := db.NewMemDB()
	serverInfo := server.NewMockServerInfo()
	serverIns := server.NewLoopServer()
	dnsIns := dns.NewMockDNS()
	csvcIns := containersvc.NewMemContainerSvc()
	s := NewManageService(dbIns, serverInfo, serverIns, dnsIns, csvcIns)

	requireStaticIP := true
	requireJournalVolume := true
	TestUtil_ServiceCreationRetry(t, s, dbIns, dnsIns, serverIns, requireStaticIP, requireJournalVolume)
}

func TestServiceCreationRetryWithoutStaticIP(t *testing.T) {
	// special tests to simulate the failure and retry
	dbIns := db.NewMemDB()
	serverInfo := server.NewMockServerInfo()
	serverIns := server.NewLoopServer()
	dnsIns := dns.NewMockDNS()
	csvcIns := containersvc.NewMemContainerSvc()
	s := NewManageService(dbIns, serverInfo, serverIns, dnsIns, csvcIns)

	requireStaticIP := false
	requireJournalVolume := false
	TestUtil_ServiceCreationRetry(t, s, dbIns, dnsIns, serverIns, requireStaticIP, requireJournalVolume)
}

func TestUnassignedIPs(t *testing.T) {
	dbIns := db.NewMemDB()
	serverInfo := server.NewMockServerInfo()
	serverIns := server.NewLoopServer()
	dnsIns := dns.NewMockDNS()
	csvcIns := containersvc.NewMemContainerSvc()
	s := NewManageService(dbIns, serverInfo, serverIns, dnsIns, csvcIns)

	ctx := context.Background()
	az := serverInfo.GetLocalAvailabilityZone()
	assignedIPs := make(map[string]string)

	vols := common.ServiceVolumes{
		PrimaryDeviceName: "/dev/xvdf",
		PrimaryVolume: common.ServiceVolume{
			VolumeType:   common.VolumeTypeGPSSD,
			VolumeSizeGB: 1,
		},
	}
	res := common.Resources{
		MaxCPUUnits:     common.DefaultMaxCPUUnits,
		ReserveCPUUnits: common.DefaultReserveCPUUnits,
		MaxMemMB:        common.DefaultMaxMemoryMB,
		ReserveMemMB:    common.DefaultReserveMemoryMB,
	}
	serviceCfgs := []common.ConfigID{
		common.ConfigID{FileName: "fname", FileID: "fid", FileMD5: "fmd5"},
	}
	mtime := time.Now().UnixNano()
	attrMeta := db.CreateServiceMeta("cluster1", "service1", mtime, common.ServiceTypeStateful, common.ServiceStatusActive)
	attrSpec := db.CreateServiceSpec(1, &res, true, "domain1", "hostedZone1", true, serviceCfgs, common.CatalogService_Kafka, &vols)
	sattr := db.CreateServiceAttr("uuid1", 0, attrMeta, attrSpec)

	// case: 1 network interface with 0 private ip
	netInterfaces := []*server.NetworkInterface{
		&server.NetworkInterface{
			InterfaceID:      "interface1",
			ServerInstanceID: "server1",
			PrimaryPrivateIP: "10.0.0.5",
			PrivateIPs:       []string{},
		},
	}

	ips, err := s.getUnassignedIPs(ctx, sattr, netInterfaces, az, assignedIPs, "requuid1")
	if err != nil {
		t.Fatalf("getUnassignedIPs error %s", err)
	}
	if len(ips) != 0 {
		t.Fatalf("expect 0 ip, get %s", ips)
	}

	// case: 1 network interface with 1 private ip
	ip := "10.0.0.6"
	netInterfaces = []*server.NetworkInterface{
		&server.NetworkInterface{
			InterfaceID:      "interface1",
			ServerInstanceID: "server1",
			PrimaryPrivateIP: "10.0.0.5",
			PrivateIPs:       []string{ip},
		},
	}

	ips, err = s.getUnassignedIPs(ctx, sattr, netInterfaces, az, assignedIPs, "requuid1")
	if err != nil {
		t.Fatalf("getUnassignedIPs error %s", err)
	}
	if len(ips) != 1 {
		t.Fatalf("expect 1 ip, get %s", ips)
	}
	if ips[0].StaticIP != ip {
		t.Fatalf("expect unassigned ip %s, get %s", ip, ips)
	}

	// assign ip to member
	assignedIPs[ip] = "member1"
	ips, err = s.getUnassignedIPs(ctx, sattr, netInterfaces, az, assignedIPs, "requuid1")
	if err != nil {
		t.Fatalf("getUnassignedIPs error %s", err)
	}
	if len(ips) != 0 {
		t.Fatalf("expect 0 ip, get %s", ips)
	}
	delete(assignedIPs, ip)

	// case: 1 network interface with multiple private ips
	privateips := []string{"10.0.0.6", "10.0.0.8", "10.0.0.10"}
	netInterfaces = []*server.NetworkInterface{
		&server.NetworkInterface{
			InterfaceID:      "interface1",
			ServerInstanceID: "server1",
			PrimaryPrivateIP: "10.0.0.5",
			PrivateIPs:       privateips,
		},
	}

	ips, err = s.getUnassignedIPs(ctx, sattr, netInterfaces, az, assignedIPs, "requuid1")
	if err != nil {
		t.Fatalf("getUnassignedIPs error %s", err)
	}
	if len(ips) != 3 {
		t.Fatalf("expect 3 ip, get %s", ips)
	}
	if ips[0].StaticIP != privateips[0] || ips[1].StaticIP != privateips[1] || ips[2].StaticIP != privateips[2] {
		t.Fatalf("expect unassigned ip %s, get %s", privateips, ips)
	}

	// assign ip to member
	assignedIPs[privateips[0]] = "member0"
	assignedIPs[privateips[1]] = "member1"
	ips, err = s.getUnassignedIPs(ctx, sattr, netInterfaces, az, assignedIPs, "requuid1")
	if err != nil {
		t.Fatalf("getUnassignedIPs error %s", err)
	}
	if len(ips) != 1 {
		t.Fatalf("expect 1 ip, get %s", ips)
	}
	if ips[0].StaticIP != privateips[2] {
		t.Fatalf("expect unassigned ip %s, get %s", privateips, ips)
	}
	delete(assignedIPs, privateips[0])
	delete(assignedIPs, privateips[1])

	// case: 2 network interfaces with multiple private ips
	privateips1 := []string{"10.0.0.7", "10.0.0.8", "10.0.0.15"}
	privateips2 := []string{"10.0.0.9", "10.0.0.13"}
	netInterfaces = []*server.NetworkInterface{
		&server.NetworkInterface{
			InterfaceID:      "interface1",
			ServerInstanceID: "server1",
			PrimaryPrivateIP: "10.0.0.5",
			PrivateIPs:       privateips1,
		},
		&server.NetworkInterface{
			InterfaceID:      "interface2",
			ServerInstanceID: "server2",
			PrimaryPrivateIP: "10.0.0.6",
			PrivateIPs:       privateips2,
		},
	}

	ips, err = s.getUnassignedIPs(ctx, sattr, netInterfaces, az, assignedIPs, "requuid1")
	if err != nil {
		t.Fatalf("getUnassignedIPs error %s", err)
	}
	if len(ips) != 5 {
		t.Fatalf("expect 5 ip, get %s", ips)
	}
	if ips[0].StaticIP != privateips1[0] || ips[1].StaticIP != privateips1[1] ||
		ips[2].StaticIP != privateips1[2] || ips[3].StaticIP != privateips2[0] || ips[4].StaticIP != privateips2[1] {
		t.Fatalf("expect unassigned ip %s %s, get %s", privateips1, privateips2, ips)
	}

	assignedIPs[privateips1[0]] = "member0"
	assignedIPs[privateips1[2]] = "member1"
	ips, err = s.getUnassignedIPs(ctx, sattr, netInterfaces, az, assignedIPs, "requuid1")
	if err != nil {
		t.Fatalf("getUnassignedIPs error %s", err)
	}
	if len(ips) != 3 {
		t.Fatalf("expect 3 ip, get %s", ips)
	}
	if ips[0].StaticIP != privateips1[1] || ips[1].StaticIP != privateips2[0] || ips[2].StaticIP != privateips2[1] {
		t.Fatalf("expect unassigned ip %s %s, get %s", privateips1, privateips2, ips)
	}
	delete(assignedIPs, privateips1[0])
	delete(assignedIPs, privateips1[2])
}

func TestCreateNextIP(t *testing.T) {
	dbIns := db.NewMemDB()
	serverInfo := server.NewMockServerInfo()
	serverIns := server.NewLoopServer()
	dnsIns := dns.NewMockDNS()
	csvcIns := containersvc.NewMemContainerSvc()
	s := NewManageService(dbIns, serverInfo, serverIns, dnsIns, csvcIns)

	ctx := context.Background()

	usedIPs := make(map[string]bool)

	netInterfaces, _, err := serverIns.GetNetworkInterfaces(ctx, "cluster", "vpc", "zone")
	if err != nil {
		t.Fatalf("GetNetworkInterfaces error", err)
	}
	if len(netInterfaces) == 0 {
		t.Fatalf("no network interface")
	}

	netInterfaceID := netInterfaces[0].InterfaceID

	cidrprefix, cidrip, cidr, cidripnet := serverIns.GetCidrBlock()
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("ParseCIDR %s error %s", cidr, err)
	}
	if ip.String() != cidrip {
		t.Fatalf("ParseCIDR result error, expect ip %s get %s", cidrip, ip)
	}
	if ipnet.String() != cidripnet {
		t.Fatalf("ParseCIDR result error, expect ipnet %s get %s", cidripnet, ipnet)
	}

	for i := 4; i < 20; i++ {
		nextIP, err := s.createNextIP(ctx, usedIPs, ipnet, ip, netInterfaceID)
		if err != nil {
			t.Fatalf("createNextIP error %s", err)
		}
		expect := cidrprefix + strconv.Itoa(i)
		if nextIP.String() != expect {
			t.Fatalf("createNextIP expect %s, get %s", expect, nextIP)
		}
		ip = nextIP
	}
}

func TestCreateStaticIPsForZone(t *testing.T) {
	dbIns := db.NewMemDB()
	dnsIns := dns.NewMockDNS()
	serverInfo := server.NewMockServerInfo()
	serverIns := server.NewLoopServer()
	csvcIns := containersvc.NewMemContainerSvc()

	s := NewManageService(dbIns, serverInfo, serverIns, dnsIns, csvcIns)

	ctx := context.Background()
	az := serverInfo.GetLocalAvailabilityZone()

	vols := common.ServiceVolumes{
		PrimaryDeviceName: "/dev/xvdf",
		PrimaryVolume: common.ServiceVolume{
			VolumeType:   common.VolumeTypeGPSSD,
			VolumeSizeGB: 1,
		},
	}
	res := common.Resources{
		MaxCPUUnits:     common.DefaultMaxCPUUnits,
		ReserveCPUUnits: common.DefaultReserveCPUUnits,
		MaxMemMB:        common.DefaultMaxMemoryMB,
		ReserveMemMB:    common.DefaultReserveMemoryMB,
	}
	serviceCfgs := []common.ConfigID{
		common.ConfigID{FileName: "fname", FileID: "fid", FileMD5: "fmd5"},
	}
	mtime := time.Now().UnixNano()
	attrMeta := db.CreateServiceMeta("cluster1", "service1", mtime, common.ServiceTypeStateful, common.ServiceStatusActive)
	attrSpec := db.CreateServiceSpec(1, &res, true, "domain1", "hostedZone1", true, serviceCfgs, common.CatalogService_Kafka, &vols)
	sattr := db.CreateServiceAttr("uuid1", 0, attrMeta, attrSpec)

	assignedIPs := make(map[string]string)

	// create 5 ips
	pendingCounts := 5
	ips, err := s.createStaticIPsForZone(ctx, sattr, assignedIPs, pendingCounts, az)
	if err != nil {
		t.Fatalf("createStaticIPsForZone error %s", err)
	}
	if len(ips) != pendingCounts {
		t.Fatalf("expect %d ips, created %d", pendingCounts, len(ips))
	}

	// create again, expect no new ips to be created
	ips1, err := s.createStaticIPsForZone(ctx, sattr, assignedIPs, pendingCounts, az)
	if err != nil {
		t.Fatalf("createStaticIPsForZone error %s", err)
	}
	if len(ips1) != pendingCounts {
		t.Fatalf("expect %d ips, created %d", pendingCounts, len(ips1))
	}
	for _, ip1 := range ips1 {
		exist := false
		for _, ip := range ips {
			if db.EqualServiceStaticIP(ip1, ip) {
				exist = true
				break
			}
		}
		if !exist {
			t.Fatalf("created new ip %s", ip1)
		}
	}

	// put the ips into assignedIPs and create more ips
	for i, ip := range ips {
		assignedIPs[ip.StaticIP] = "member" + strconv.Itoa(i)
	}
	ips1, err = s.createStaticIPsForZone(ctx, sattr, assignedIPs, pendingCounts, az)
	if err != nil {
		t.Fatalf("createStaticIPsForZone error %s", err)
	}
	if len(ips1) != pendingCounts {
		t.Fatalf("expect %d ips, created %d", pendingCounts, len(ips1))
	}
	for _, ip1 := range ips1 {
		for _, ip := range ips {
			if db.EqualServiceStaticIP(ip1, ip) {
				t.Fatalf("expect new ip, but ip %s exist", ip1)
			}
		}
	}
}

func TestCreateStaticIPsForZoneMultiNetInterfaces(t *testing.T) {
	dbIns := db.NewMemDB()
	dnsIns := dns.NewMockDNS()
	serverInfo := server.NewMockServerInfo()

	serverIns := server.NewLoopServer()
	// add 2 more network interfaces
	serverIns.AddNetworkInterface()
	serverIns.AddNetworkInterface()

	csvcIns := containersvc.NewMemContainerSvc()
	s := NewManageService(dbIns, serverInfo, serverIns, dnsIns, csvcIns)

	ctx := context.Background()
	az := serverInfo.GetLocalAvailabilityZone()

	vols := common.ServiceVolumes{
		PrimaryDeviceName: "/dev/xvdf",
		PrimaryVolume: common.ServiceVolume{
			VolumeType:   common.VolumeTypeGPSSD,
			VolumeSizeGB: 1,
		},
	}
	res := common.Resources{
		MaxCPUUnits:     common.DefaultMaxCPUUnits,
		ReserveCPUUnits: common.DefaultReserveCPUUnits,
		MaxMemMB:        common.DefaultMaxMemoryMB,
		ReserveMemMB:    common.DefaultReserveMemoryMB,
	}
	serviceCfgs := []common.ConfigID{
		common.ConfigID{FileName: "fname", FileID: "fid", FileMD5: "fmd5"},
	}
	mtime := time.Now().UnixNano()
	attrMeta := db.CreateServiceMeta("cluster1", "service1", mtime, common.ServiceTypeStateful, common.ServiceStatusActive)
	attrSpec := db.CreateServiceSpec(1, &res, true, "domain1", "hostedZone1", true, serviceCfgs, common.CatalogService_Kafka, &vols)
	sattr := db.CreateServiceAttr("uuid1", 0, attrMeta, attrSpec)

	assignedIPs := make(map[string]string)

	netInterfaces, _, err := serverIns.GetNetworkInterfaces(ctx, "cluster1", "vpc", "zone")
	if err != nil {
		t.Fatalf("GetNetworkInterfaces error %s", err)
	}

	// create 5 ips
	pendingCounts := 5
	ips, err := s.createStaticIPsForZone(ctx, sattr, assignedIPs, pendingCounts, az)
	if err != nil {
		t.Fatalf("createStaticIPsForZone error %s", err)
	}
	if len(ips) != pendingCounts {
		t.Fatalf("expect %d ips, created %d", pendingCounts, len(ips))
	}
	for i, ip := range ips {
		idx := i % len(netInterfaces)
		if netInterfaces[idx].InterfaceID != ip.Spec.NetworkInterfaceID {
			t.Fatalf("expect netInterfaceID %s, ip %s", netInterfaces[idx].InterfaceID, ip)
		}
	}

	// create again, expect no new ips to be created
	ips1, err := s.createStaticIPsForZone(ctx, sattr, assignedIPs, pendingCounts, az)
	if err != nil {
		t.Fatalf("createStaticIPsForZone error %s", err)
	}
	if len(ips1) != pendingCounts {
		t.Fatalf("expect %d ips, created %d", pendingCounts, len(ips1))
	}
	for _, ip1 := range ips1 {
		exist := false
		for _, ip := range ips {
			if db.EqualServiceStaticIP(ip1, ip) {
				exist = true
				break
			}
		}
		if !exist {
			t.Fatalf("created new ip %s", ip1)
		}
	}

	// put the ips into assignedIPs and create more ips
	for i, ip := range ips {
		assignedIPs[ip.StaticIP] = "member" + strconv.Itoa(i)
	}
	ips1, err = s.createStaticIPsForZone(ctx, sattr, assignedIPs, pendingCounts, az)
	if err != nil {
		t.Fatalf("createStaticIPsForZone error %s", err)
	}
	if len(ips1) != pendingCounts {
		t.Fatalf("expect %d ips, created %d", pendingCounts, len(ips1))
	}
	if ips1[0].Spec.NetworkInterfaceID != netInterfaces[2].InterfaceID {
		t.Fatalf("expect the first ip belong to %s, get %s", netInterfaces[2].InterfaceID, ips1[0])
	}
	for i := 1; i < len(ips1); i++ {
		idx := (i - 1) % len(netInterfaces)
		if netInterfaces[idx].InterfaceID != ips1[i].Spec.NetworkInterfaceID {
			t.Fatalf("expect netInterfaceID %s, ip %s", netInterfaces[idx].InterfaceID, ips1[i])
		}
	}
	for _, ip1 := range ips1 {
		for _, ip := range ips {
			if db.EqualServiceStaticIP(ip1, ip) {
				t.Fatalf("expect new ip, but ip %s exist", ip1)
			}
		}
	}
}
