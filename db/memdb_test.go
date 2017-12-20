package db

import (
	"encoding/json"
	"flag"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/utils"
)

var dbIns *MemDB

func TestMain(m *testing.M) {
	flag.Parse()
	//flag.Set("stderrthreshold", "FATAL")

	dbIns = NewMemDB()

	ctx := context.Background()

	// create device and service tables
	err := dbIns.CreateSystemTables(ctx)
	defer dbIns.DeleteSystemTables(ctx)
	if err != nil {
		return
	}

	m.Run()
}

func TestDevices(t *testing.T) {
	clusterName := "cluster1"
	devPrefix := "/dev/xvd"
	servicePrefix := "service-"

	ctx := context.Background()

	// create 6 device items
	x := [6]string{"f", "g", "h", "i", "j", "k"}
	for _, c := range x {
		item := CreateDevice(clusterName, devPrefix+c, servicePrefix+c)
		err := dbIns.CreateDevice(ctx, item)
		if err != nil {
			t.Fatalf("failed to create Device %s, error %s", c, err)
		}
	}

	// create xvdf again, expect to fail
	t1 := CreateDevice(clusterName, devPrefix+x[0], servicePrefix+x[0])
	err := dbIns.CreateDevice(ctx, t1)
	if err != ErrDBConditionalCheckFailed {
		t.Fatalf("create existing Device %s, expect fail but status is %s", t1, err)
	}

	// get xvdf
	t2, err := dbIns.GetDevice(ctx, t1.ClusterName, t1.DeviceName)
	if err != nil || !EqualDevice(t1, t2) {
		t.Fatalf("get Device not the same %s %s error %s", t1, t2, err)
	}

	// list Devices
	items, err := dbIns.ListDevices(ctx, clusterName)
	if err != nil || len(items) != len(x) {
		t.Fatalf("ListDevices failed, get items %s error %s", items, err)
	}
	for _, item := range items {
		c := strings.TrimLeft(item.DeviceName, devPrefix)
		expectItem := CreateDevice(clusterName, devPrefix+c, servicePrefix+c)
		if !EqualDevice(expectItem, item) {
			t.Fatalf("ListDevices failed, expected %s got %s, index %s", expectItem, item, c)
		}
	}

	// pagination list
	items, err = dbIns.listDevicesWithLimit(ctx, clusterName, 1)
	if err != nil || len(items) != len(x) {
		t.Fatalf("ListDevices failed, get items %s error %s", items, err)
	}
	for _, item := range items {
		c := strings.TrimLeft(item.DeviceName, devPrefix)
		expectItem := CreateDevice(clusterName, devPrefix+c, servicePrefix+c)
		if !EqualDevice(expectItem, item) {
			t.Fatalf("ListDevices failed, expected %s got %s, index %s", expectItem, item, c)
		}
	}

	// delete xvdk
	err = dbIns.DeleteDevice(ctx, clusterName, devPrefix+x[5])
	if err != nil {
		t.Fatalf("failed to delete Device %s error %s", devPrefix+x[5], err)
	}

	// pagination list again after deletion
	items, err = dbIns.listDevicesWithLimit(ctx, clusterName, 2)
	if err != nil || len(items) != (len(x)-1) {
		t.Fatalf("ListDevices failed, get items %s error %s", items, err)
	}
	for _, item := range items {
		c := strings.TrimLeft(item.DeviceName, devPrefix)
		expectItem := CreateDevice(clusterName, devPrefix+c, servicePrefix+c)
		if !EqualDevice(expectItem, item) {
			t.Fatalf("ListDevices failed, expected %s got %s, index %s", expectItem, item, c)
		}
	}

	// delete one unexist device
	err = dbIns.DeleteDevice(ctx, "cluster-x", "dev-x")
	if err == nil || err != ErrDBRecordNotFound {
		t.Fatalf("delete unexist device, expect ErrDBRecordNotFound, got error %s", err)
	}
}

func TestServices(t *testing.T) {
	clusterName := "cluster1"
	servicePrefix := "service-"
	uuidPrefix := "uuid-"

	ctx := context.Background()

	// create 5 services
	var s [5]*common.Service
	x := [5]string{"a", "b", "c", "d", "e"}
	for i, c := range x {
		s[i] = CreateService(clusterName, servicePrefix+c, uuidPrefix+c)
		err := dbIns.CreateService(ctx, s[i])
		if err != nil {
			t.Fatalf("failed to create service %s, err %s", c, err)
		}
	}

	// list all services
	services, err := dbIns.ListServices(ctx, clusterName)
	if err != nil || len(services) != 5 {
		t.Fatalf("ListServices error %s, services %s", err, services)
	}

	// get service to verify
	svc, err := dbIns.GetService(ctx, s[1].ClusterName, s[1].ServiceName)
	if err != nil || !EqualService(svc, s[1]) {
		t.Fatalf("get service failed, error %s, expected %s get %s", err, s[1], svc)
	}

	// delete service
	err = dbIns.DeleteService(ctx, s[2].ClusterName, s[2].ServiceName)
	if err != nil {
		t.Fatalf("failed to delete service %s error %s", s[2], err)
	}

	// delete one unexist service
	err = dbIns.DeleteService(ctx, s[2].ClusterName, s[2].ServiceName)
	if err == nil || err != ErrDBRecordNotFound {
		t.Fatalf("delete unexist service %s, expect ErrDBRecordNotFound, got error %s", err)
	}
}

func TestServiceAttr(t *testing.T) {
	uuidPrefix := "uuid-"
	volSize := 10
	clusterName := "cluster1"
	servicePrefix := "service-"
	devPrefix := "/dev/xvd"
	registerDNS := true
	domain := "domain"
	hostedZoneID := "hostedZoneID"
	requireStaticIP := false

	ctx := context.Background()

	mattr := common.MongoDBUserAttr{
		Shards:           1,
		ReplicasPerShard: 3,
		ReplicaSetOnly:   false,
		ConfigServers:    3,
	}
	b, err := json.Marshal(mattr)
	if err != nil {
		t.Fatalf("Marshal MongoDBUserAttr error %s", err)
	}
	userAttr := &common.ServiceUserAttr{
		ServiceType: "mongodb",
		AttrBytes:   b,
	}
	res := common.Resources{
		MaxCPUUnits:     common.DefaultMaxCPUUnits,
		ReserveCPUUnits: common.DefaultReserveCPUUnits,
		MaxMemMB:        common.DefaultMaxMemoryMB,
		ReserveMemMB:    common.DefaultReserveMemoryMB,
	}

	// create 5 services
	var s [5]*common.ServiceAttr
	x := [5]string{"a", "b", "c", "d", "e"}
	for i, c := range x {
		svols := common.ServiceVolumes{
			PrimaryDeviceName: devPrefix + c,
			PrimaryVolume: common.ServiceVolume{
				VolumeType:   common.VolumeTypeGPSSD,
				VolumeSizeGB: int64(volSize + i),
			},
			JournalDeviceName: devPrefix + "journal" + c,
			JournalVolume: common.ServiceVolume{
				VolumeType:   common.VolumeTypeGPSSD,
				VolumeSizeGB: int64(volSize + i),
			},
		}

		s[i] = CreateInitialServiceAttr(uuidPrefix+c, int64(i),
			clusterName, servicePrefix+c, svols, registerDNS, domain, hostedZoneID, requireStaticIP, userAttr, res)

		err := dbIns.CreateServiceAttr(ctx, s[i])
		if err != nil {
			t.Fatalf("failed to create service attr %s, err %s", c, err)
		}
	}

	// get service attr to verify
	attr, err := dbIns.GetServiceAttr(ctx, s[1].ServiceUUID)
	if err != nil || !EqualServiceAttr(attr, s[1], false) {
		t.Fatalf("get service attr failed, error %s, expected %s get %s", err, s[1], attr)
	}

	// update service
	attr.ServiceStatus = "ACTIVE"
	err = dbIns.UpdateServiceAttr(ctx, s[1], attr)
	if err != nil {
		t.Fatalf("update service attr failed, service %s error %s", attr, err)
	}

	// service updated
	s[1].ServiceStatus = "ACTIVE"

	// get service again to verify the update
	attr, err = dbIns.GetServiceAttr(ctx, s[1].ServiceUUID)
	if err != nil || !EqualServiceAttr(attr, s[1], false) {
		t.Fatalf("get service attr after update failed, error %s, expected %s get %s", err, s[1], attr)
	}

	// delete service
	err = dbIns.DeleteServiceAttr(ctx, s[2].ServiceUUID)
	if err != nil {
		t.Fatalf("failed to delete service attr %s error %s", s[2], err)
	}

	// delete one unexist service
	err = dbIns.DeleteServiceAttr(ctx, s[2].ServiceUUID)
	if err == nil || err != ErrDBRecordNotFound {
		t.Fatalf("delete unexist service %s, expect ErrDBRecordNotFound, got error %s", s[2], err)
	}
}

func TestServiceMembers(t *testing.T) {
	service1 := "serviceuuid-1"
	service2 := "serviceuuid-2"
	dev1 := "/dev/xvdf"
	dev2 := "/dev/xvdg"
	volPrefix := "vol-"
	taskPrefix := "taskID-"
	contPrefix := "containerInstanceID-"
	hostPrefix := "ServerInstanceID-"
	az := "az-west"
	cfgNamePrefix := "cfgNamePrefix-"
	cfgIDPrefix := "cfgIDPrefix-"
	cfgContentPrefix := "cfgContentPrefix-"
	staticIPPrefix := "ip-"

	ctx := context.Background()

	// create 6 serviceMembers for service1
	mtime := time.Now().UnixNano()
	x := [6]string{"0", "1", "2", "3", "4", "5"}
	var s1 [6]*common.ServiceMember
	for i, c := range x {
		content := cfgContentPrefix + c
		chksum := utils.GenMD5(content)
		cfg := &common.MemberConfig{FileName: cfgNamePrefix + c, FileID: cfgIDPrefix + c, FileMD5: chksum}
		cfgs := []*common.MemberConfig{cfg}
		mvols := common.MemberVolumes{
			PrimaryVolumeID:   volPrefix + c,
			PrimaryDeviceName: dev1,
		}
		s1[i] = CreateServiceMember(service1, int64(i), utils.GenServiceMemberName(service1, int64(i)),
			az, taskPrefix+c, contPrefix+c, hostPrefix+c, mtime, mvols, staticIPPrefix+c, cfgs)

		err := dbIns.CreateServiceMember(ctx, s1[i])
		if err != nil {
			t.Fatalf("failed to create serviceMember %s, err %s", c, err)
		}
	}

	// create 4 serviceMembers for service2
	var s2 [4]*common.ServiceMember
	for i := 0; i < 4; i++ {
		c := x[i]
		content := cfgContentPrefix + c
		chksum := utils.GenMD5(content)
		cfg := &common.MemberConfig{FileName: cfgNamePrefix + c, FileID: cfgIDPrefix + c, FileMD5: chksum}
		cfgs := []*common.MemberConfig{cfg}
		mvols := common.MemberVolumes{
			PrimaryVolumeID:   volPrefix + c,
			PrimaryDeviceName: dev2,
		}
		s2[i] = CreateServiceMember(service2, int64(i), utils.GenServiceMemberName(service2, int64(i)),
			az, taskPrefix+c, contPrefix+c, hostPrefix+c, mtime, mvols, staticIPPrefix+c, cfgs)

		err := dbIns.CreateServiceMember(ctx, s2[i])
		if err != nil {
			t.Fatalf("failed to create serviceMember %s, err %s", c, err)
		}
	}

	// get service member to verify
	item, err := dbIns.GetServiceMember(ctx, s1[1].ServiceUUID, s1[1].MemberIndex)
	if err != nil || !EqualServiceMember(item, s1[1], false) {
		t.Fatalf("get serviceMember failed, error %s, expected %s get %s", err, s1[1], item)
	}

	// update serviceMember
	item.TaskID = taskPrefix + "z"
	item.ContainerInstanceID = contPrefix + "z"
	item.ServerInstanceID = hostPrefix + "z"
	err = dbIns.UpdateServiceMember(ctx, s1[1], item)
	if err != nil {
		t.Fatalf("update serviceMember failed, serviceMember %s error %s", item, err)
	}

	// serviceMember updated
	s1[1].TaskID = item.TaskID
	s1[1].ContainerInstanceID = item.ContainerInstanceID
	s1[1].ServerInstanceID = item.ServerInstanceID

	// get serviceMember again to verify the update
	item, err = dbIns.GetServiceMember(ctx, s1[1].ServiceUUID, s1[1].MemberIndex)
	if err != nil || !EqualServiceMember(item, s1[1], false) {
		t.Fatalf("get serviceMember after update failed, error %s, expected %s get %s", err, s1[1], item)
	}

	// list serviceMembers of service1
	items, err := dbIns.ListServiceMembers(ctx, s1[0].ServiceUUID)
	if err != nil || len(items) != len(s1) {
		t.Fatalf("expected %d serviceMembers for service %s, got %s, error %s",
			len(s1), s1[0].ServiceUUID, items, err)
	}
	for _, item := range items {
		if !EqualServiceMember(item, s1[item.MemberIndex], false) {
			t.Fatalf("expected %s, got %s, index %d", s1[item.MemberIndex], item, item.MemberIndex)
		}
	}

	// pagination list serviceMembers of service2
	items, err = dbIns.listServiceMembersWithLimit(ctx, s2[0].ServiceUUID, 3)
	if err != nil || len(items) != len(s2) {
		t.Fatalf("expected %d serviceMembers for service %s, got %s, error %s",
			len(s2), s2[0].ServiceUUID, items, err)
	}
	for _, item := range items {
		if !EqualServiceMember(item, s2[item.MemberIndex], false) {
			t.Fatalf("expected %s, got %s, index %d", s2[item.MemberIndex], item, item.MemberIndex)
		}
	}

	// delete serviceMember
	err = dbIns.DeleteServiceMember(ctx, s1[len(s1)-1].ServiceUUID, s1[len(s1)-1].MemberIndex)
	if err != nil {
		t.Fatalf("failed to delete serviceMember %s error %s", s1[len(s1)-1], err)
	}

	// pagination list serviceMembers of service1
	items, err = dbIns.listServiceMembersWithLimit(ctx, s1[0].ServiceUUID, 3)
	if err != nil || len(items) != (len(s1)-1) {
		t.Fatalf("expected %d serviceMembers for service %s, got %s, error %s",
			len(s1)-1, s1[0].ServiceUUID, items, err)
	}
	for _, item := range items {
		if !EqualServiceMember(item, s1[item.MemberIndex], false) {
			t.Fatalf("expected %s, got %s, index %d", s1[item.MemberIndex], item, item.MemberIndex)
		}
	}

	// delete one unexist serviceMember
	err = dbIns.DeleteServiceMember(ctx, s1[0].ServiceUUID, 100)
	if err == nil || err != ErrDBRecordNotFound {
		t.Fatalf("delete unexist serviceMember, expect ErrDBRecordNotFound, got error %s", err)
	}
}

func TestConfigFile(t *testing.T) {
	serviceUUIDPrefix := "serviceuuid-"
	fileIDPrefix := "fileid-"
	fileNamePrefix := "filename-"
	fileMode := uint32(0600)
	contentPrefix := "content-"

	ctx := context.Background()

	// create 5 config files
	var s [5]*common.ConfigFile
	x := [5]string{"a", "b", "c", "d", "e"}
	for i, c := range x {
		s[i] = CreateInitialConfigFile(serviceUUIDPrefix+c, fileIDPrefix+c, fileNamePrefix+c, fileMode, contentPrefix+c)

		err := dbIns.CreateConfigFile(ctx, s[i])
		if err != nil {
			t.Fatalf("failed to create config file %s, err %s", c, err)
		}
	}

	// get config file to verify
	cfg, err := dbIns.GetConfigFile(ctx, s[1].ServiceUUID, s[1].FileID)
	if err != nil || !EqualConfigFile(cfg, s[1], false, false) {
		t.Fatalf("get config file failed, error %s, expected %s get %s", err, s[1], cfg)
	}

	// delete service
	err = dbIns.DeleteConfigFile(ctx, s[2].ServiceUUID, s[2].FileID)
	if err != nil {
		t.Fatalf("failed to delete config file %s error %s", s[2], err)
	}

	// negative case: delete one unexist service
	err = dbIns.DeleteConfigFile(ctx, s[2].ServiceUUID, s[2].FileID)
	if err == nil || err != ErrDBRecordNotFound {
		t.Fatalf("delete unexist config file %s, expect ErrDBRecordNotFound, got error %s", s[2], err)
	}
}

func TestServiceStaticIP(t *testing.T) {
	ipPrefix := "ip-"
	serviceUUIDPrefix := "serviceuuid-"
	az := "az1"
	instanceIDPrefix := "server-"
	netInterfacePrefix := "net-"

	ctx := context.Background()

	// create
	var s [5]*common.ServiceStaticIP
	x := [5]string{"a", "b", "c", "d", "e"}
	for i, c := range x {
		s[i] = CreateServiceStaticIP(ipPrefix+c, serviceUUIDPrefix+c, az, instanceIDPrefix+c, netInterfacePrefix+c)

		err := dbIns.CreateServiceStaticIP(ctx, s[i])
		if err != nil {
			t.Fatalf("failed to create %s, err %s", c, err)
		}
	}

	// get to verify
	ip, err := dbIns.GetServiceStaticIP(ctx, s[1].StaticIP)
	if err != nil || !EqualServiceStaticIP(ip, s[1]) {
		t.Fatalf("get failed, error %s, expected %s get %s", err, s[1], ip)
	}

	// update
	newInstanceID := "newserver"
	newNetInterfaceID := "newinterface"
	newip := UpdateServiceStaticIP(ip, newInstanceID, newNetInterfaceID)
	err = dbIns.UpdateServiceStaticIP(ctx, ip, newip)
	if err != nil {
		t.Fatalf("update failed, error %s", err)
	}

	// get to verify
	ip1, err := dbIns.GetServiceStaticIP(ctx, s[1].StaticIP)
	if err != nil || !EqualServiceStaticIP(ip1, newip) {
		t.Fatalf("get failed, error %s, expected %s get %s", err, newip, ip1)
	}

	// delete
	err = dbIns.DeleteServiceStaticIP(ctx, s[2].StaticIP)
	if err != nil {
		t.Fatalf("failed to delete %s error %s", s[2], err)
	}

	// negative case: delete one unexist service
	err = dbIns.DeleteServiceStaticIP(ctx, s[2].StaticIP)
	if err == nil || err != ErrDBRecordNotFound {
		t.Fatalf("delete unexist item %s, expect ErrDBRecordNotFound, got error %s", s[2], err)
	}
}
