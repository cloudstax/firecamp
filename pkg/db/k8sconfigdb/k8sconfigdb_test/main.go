package main

import (
	"flag"
	"time"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/jazzl0ver/firecamp/pkg/db"
	"github.com/jazzl0ver/firecamp/pkg/db/k8sconfigdb"
	"github.com/jazzl0ver/firecamp/pkg/utils"
)

var kubeconfig = flag.String("kubeconfig", "/home/ubuntu/.kube/config", "absolute path to the kubeconfig file")

func main() {
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	k8sdb, err := k8sconfigdb.NewK8sConfigDBWithConfig("default", config)
	if err != nil {
		glog.Fatalf("NewK8sConfigDB error", err)
	}

	testDevices(k8sdb)
	testServices(k8sdb)
	testServiceAttrs(k8sdb)
	testServiceMembers(k8sdb)
	testConfigFile(k8sdb)
	testServiceStaticIPs(k8sdb)
}

func testDevices(dbIns *k8sconfigdb.K8sConfigDB) {
	clusterName := "cluster1"
	devPrefix := "/dev/xvd"
	servicePrefix := "service-"

	ctx := context.Background()

	// create 6 device items
	x := [6]string{"f", "g", "h", "i", "j", "k"}
	for _, c := range x {
		item := db.CreateDevice(clusterName, devPrefix+c, servicePrefix+c)
		err := dbIns.CreateDevice(ctx, item)
		if err != nil {
			glog.Fatalf("failed to create db.Device %s, error %s", c, err)
		}
	}

	// create xvdf again, expect to fail
	t1 := db.CreateDevice(clusterName, devPrefix+x[0], servicePrefix+x[0])
	err := dbIns.CreateDevice(ctx, t1)
	if err != db.ErrDBConditionalCheckFailed {
		glog.Fatalf("create existing db.Device %s, expect fail but status is %s", t1, err)
	}

	// get xvdf
	t2, err := dbIns.GetDevice(ctx, t1.ClusterName, t1.DeviceName)
	if err != nil || !db.EqualDevice(t1, t2) {
		glog.Fatalf("get db.Device not the same %s %s error %s", t1, t2, err)
	}

	// list Devices
	items, err := dbIns.ListDevices(ctx, clusterName)
	if err != nil || len(items) != len(x) {
		glog.Fatalf("Listdb.Devices failed, get items %s error %s", items, err)
	}
	for i, item := range items {
		expectItem := db.CreateDevice(clusterName, devPrefix+x[i], servicePrefix+x[i])
		if !db.EqualDevice(expectItem, item) {
			glog.Fatalf("Listdb.Devices failed, expected %s got %s, index %d", expectItem, item, i)
		}
	}

	// pagination list
	items, err = dbIns.ListDevicesWithLimit(ctx, clusterName, 1)
	if err != nil || len(items) != len(x) {
		glog.Fatalf("Listdb.Devices failed, get items %s error %s", items, err)
	}
	for i, item := range items {
		expectItem := db.CreateDevice(clusterName, devPrefix+x[i], servicePrefix+x[i])
		if !db.EqualDevice(expectItem, item) {
			glog.Fatalf("Listdb.Devices failed, expected %s got %s, index %d", expectItem, item, i)
		}
	}

	// delete the last device
	err = dbIns.DeleteDevice(ctx, clusterName, devPrefix+x[5])
	if err != nil {
		glog.Fatalf("failed to delete db.Device %s error %s", t1, err)
	}

	// pagination list again after deletion
	items, err = dbIns.ListDevicesWithLimit(ctx, clusterName, 2)
	if err != nil || len(items) != (len(x)-1) {
		glog.Fatalf("Listdb.Devices failed, get items %s error %s", items, err)
	}
	for i, item := range items {
		expectItem := db.CreateDevice(clusterName, devPrefix+x[i], servicePrefix+x[i])
		if !db.EqualDevice(expectItem, item) {
			glog.Fatalf("Listdb.Devices failed, expected %s got %s, index %d", expectItem, item, i)
		}
	}

	// get unexist device
	item, err := dbIns.GetDevice(ctx, "cluster-x", "dev-x")
	if err == nil || err != db.ErrDBRecordNotFound {
		glog.Fatalf("get unexist device, expect db.ErrDBRecordNotFound, got error %s item %s", err, item)
	}

	// delete one unexist device
	err = dbIns.DeleteDevice(ctx, "cluster-x", "dev-x")
	if err == nil || err != db.ErrDBRecordNotFound {
		glog.Fatalf("delete unexist device, expect db.ErrDBRecordNotFound, got error %s", err)
	}

	// cleanup: delete all devices
	for _, c := range x {
		err := dbIns.DeleteDevice(ctx, clusterName, devPrefix+c)
		if err != nil && err != db.ErrDBRecordNotFound {
			glog.Fatalf("failed to delete db.Device %s, error %s", c, err)
		}
	}

}

func testServices(dbIns *k8sconfigdb.K8sConfigDB) {
	clusterName := "cluster1"
	servicePrefix := "service-"
	uuidPrefix := "uuid-"

	ctx := context.Background()

	// create 5 services
	var s [5]*common.Service
	x := [5]string{"a", "b", "c", "d", "e"}
	for i, c := range x {
		s[i] = db.CreateService(clusterName, servicePrefix+c, uuidPrefix+c)
		err := dbIns.CreateService(ctx, s[i])
		if err != nil {
			glog.Fatalf("failed to create service %s, err %s", s[i], err)
		}
	}

	// list all services
	services, err := dbIns.ListServices(ctx, clusterName)
	if err != nil || len(services) != 5 {
		glog.Fatalf("ListServices failed, error %s, expected 5 services, got %d", err, len(services))
	}

	// get service to verify
	item, err := dbIns.GetService(ctx, s[1].ClusterName, s[1].ServiceName)
	if err != nil || !db.EqualService(item, s[1]) {
		glog.Fatalf("get service failed, error %s, expected %s get %s", err, s[1], item)
	}

	// pagination list all services
	services, err = dbIns.ListServicesWithLimit(ctx, clusterName, 2)
	if err != nil || len(services) != 5 {
		glog.Fatalf("ListServices failed, error %s, expected 5 services, got %d", err, len(services))
	}

	// delete service
	err = dbIns.DeleteService(ctx, s[2].ClusterName, s[2].ServiceName)
	if err != nil {
		glog.Fatalf("failed to delete service %s error %s", s[2], err)
	}

	// delete one unexist service
	err = dbIns.DeleteService(ctx, s[2].ClusterName, s[2].ServiceName)
	if err == nil || err != db.ErrDBRecordNotFound {
		glog.Fatalf("delete unexist service %s, expect db.ErrDBRecordNotFound, got error %s", s[2], err)
	}

	// get one unexist service
	item, err = dbIns.GetService(ctx, "cluster-x", "service-x")
	if err == nil || err != db.ErrDBRecordNotFound {
		glog.Fatalf("get unexist service, expect db.ErrDBRecordNotFound, got error %s item %s", err, item)
	}

	// delete one unexist service
	err = dbIns.DeleteService(ctx, "cluster-x", "service-x")
	if err == nil || err != db.ErrDBRecordNotFound {
		glog.Fatalf("delete unexist service, expect db.ErrDBRecordNotFound, got error %s", err)
	}

	// cleanup: delete all services
	for _, c := range x {
		err := dbIns.DeleteService(ctx, clusterName, servicePrefix+c)
		if err != nil && err != db.ErrDBRecordNotFound {
			glog.Fatalf("failed to delete service %s, err %s", c, err)
		}
	}

}

func testServiceAttrs(dbIns *k8sconfigdb.K8sConfigDB) {
	uuidPrefix := "uuid-"
	clusterName := "cluster1"
	servicePrefix := "service-"
	devPrefix := "/dev/xvd"
	registerDNS := true
	domain := "domain"
	hostedZoneID := "hostedZoneID"
	requireStaticIP := false
	res := common.Resources{
		MaxCPUUnits:     common.DefaultMaxCPUUnits,
		ReserveCPUUnits: common.DefaultReserveCPUUnits,
		MaxMemMB:        common.DefaultMaxMemoryMB,
		ReserveMemMB:    common.DefaultReserveMemoryMB,
	}
	cfgs := []common.ConfigID{
		common.ConfigID{FileName: "fname", FileID: "fid", FileMD5: "fmd5"},
	}

	ctx := context.Background()

	// create 5 services
	var s [5]*common.ServiceAttr
	x := [5]string{"a", "b", "c", "d", "e"}
	for i, c := range x {
		svols := common.ServiceVolumes{
			PrimaryDeviceName: devPrefix + c,
			PrimaryVolume: common.ServiceVolume{
				VolumeType:   common.VolumeTypeGPSSD,
				VolumeSizeGB: 1,
			},
			JournalDeviceName: devPrefix + "journal" + c,
			JournalVolume: common.ServiceVolume{
				VolumeType:   common.VolumeTypeGPSSD,
				VolumeSizeGB: 1,
			},
		}

		serviceType := common.ServiceTypeStateful
		if i%2 == 0 {
			serviceType = common.ServiceTypeStateless
		}
		mtime := time.Now().UnixNano()
		attrMeta := db.CreateServiceMeta(clusterName, servicePrefix+c, mtime, serviceType, common.ServiceStatusCreating)
		attrSpec := db.CreateServiceSpec(int64(i), &res, registerDNS, domain, hostedZoneID, requireStaticIP, cfgs, common.CatalogService_Kafka, &svols)
		s[i] = db.CreateServiceAttr(uuidPrefix+c, 0, attrMeta, attrSpec)
		err := dbIns.CreateServiceAttr(ctx, s[i])
		if err != nil {
			glog.Fatalf("failed to create service attr %s, err %s", s[i], err)
		}

		item, err := dbIns.GetServiceAttr(ctx, s[i].ServiceUUID)
		if err != nil || !db.EqualServiceAttr(item, s[i], false, false) {
			glog.Fatalf("get service attr failed, error %s, expected %s get %s", err, s[i], item)
		}
	}

	// get service to verify
	item, err := dbIns.GetServiceAttr(ctx, s[1].ServiceUUID)
	if err != nil || !db.EqualServiceAttr(item, s[1], false, false) {
		glog.Fatalf("get service attr failed, error %s, expected %s get %s", err, s[1], item)
	}

	// update service status
	item.Revision++
	item.Meta.LastModified = time.Now().UnixNano()
	item.Meta.ServiceStatus = "ACTIVE"
	err = dbIns.UpdateServiceAttr(ctx, s[1], item)
	if err != nil {
		glog.Fatalf("update service attr failed, service %s error %s", item, err)
	}
	// service updated
	s[1].Revision++
	s[1].Meta.LastModified = item.Meta.LastModified
	s[1].Meta.ServiceStatus = "ACTIVE"
	// get service again to verify the update
	item, err = dbIns.GetServiceAttr(ctx, s[1].ServiceUUID)
	if err != nil || !db.EqualServiceAttr(item, s[1], false, false) {
		glog.Fatalf("get service attr after update failed, error %s, expected %s get %s", err, s[1], item)
	}

	// update service replicas
	item.Revision++
	item.Meta.LastModified = time.Now().UnixNano()
	item.Spec.Replicas = 10
	err = dbIns.UpdateServiceAttr(ctx, s[1], item)
	if err != nil {
		glog.Fatalf("update service attr failed, service %s error %s", item, err)
	}
	// service updated
	s[1].Revision++
	s[1].Meta.LastModified = item.Meta.LastModified
	s[1].Spec.Replicas = 10
	// get service again to verify the update
	item, err = dbIns.GetServiceAttr(ctx, s[1].ServiceUUID)
	if err != nil || !db.EqualServiceAttr(item, s[1], false, false) {
		glog.Fatalf("get service attr after update failed, error %s, expected %s get %s", err, s[1], item)
	}

	// negative case: update immutable fields
	newAttr := db.CopyServiceAttr(item)
	newAttr.Meta.ServiceName = "new-name"
	err = dbIns.UpdateServiceAttr(ctx, item, newAttr)
	if err != db.ErrDBInvalidRequest {
		glog.Fatalf("update service attr, expect db.ErrDBInvalidRequest, get error %s, attr %s", err, item)
	}

	// delete service
	err = dbIns.DeleteServiceAttr(ctx, s[2].ServiceUUID)
	if err != nil {
		glog.Fatalf("failed to delete service attr %s error %s", s[2], err)
	}

	// delete one unexist service
	err = dbIns.DeleteServiceAttr(ctx, s[2].ServiceUUID)
	if err == nil || err != db.ErrDBRecordNotFound {
		glog.Fatalf("delete unexist service %s, expect db.ErrDBRecordNotFound, got error %s", s[2], err)
	}

	// get one unexist service
	gitem, err := dbIns.GetService(ctx, "cluster-x", "service-x")
	if err == nil || err != db.ErrDBRecordNotFound {
		glog.Fatalf("get unexist service, expect db.ErrDBRecordNotFound, got error %s item %s", err, gitem)
	}

	// delete one unexist service
	err = dbIns.DeleteService(ctx, "cluster-x", "service-x")
	if err == nil || err != db.ErrDBRecordNotFound {
		glog.Fatalf("delete unexist service, expect db.ErrDBRecordNotFound, got error %s", err)
	}

	// cleanup: delete all services
	for _, c := range x {
		err := dbIns.DeleteServiceAttr(ctx, uuidPrefix+c)
		if err != nil && err != db.ErrDBRecordNotFound {
			glog.Fatalf("failed to delete service attr %s, err %s", c, err)
		}
	}
}

func testServiceMembers(dbIns *k8sconfigdb.K8sConfigDB) {
	service1 := "service1"
	service2 := "service2"
	uuid1 := "uuid1"
	uuid2 := "uuid2"
	dev1 := "/dev/xvdf"
	dev2 := "/dev/xvdg"
	volPrefix := "vol-"
	taskPrefix := "taskID-"
	contPrefix := "containerInstanceID-"
	hostPrefix := "ServerInstanceID-"
	azPrefix := "az-"
	fileIDPrefix := "cfgfile-id"
	fileNamePrefix := "cfgfile-name"
	fileMD5Prefix := "cfgfile-md5"
	staticIPPrefix := "ip-"
	mtime := time.Now().UnixNano()

	ctx := context.Background()

	// create 6 serviceMembers for service1
	x := [6]string{"a", "b", "c", "d", "e", "f"}
	s1 := make(map[string]*common.ServiceMember)
	for i, c := range x {
		cfgs := []common.ConfigID{
			common.ConfigID{FileName: fileNamePrefix + c, FileID: fileIDPrefix + c, FileMD5: fileMD5Prefix + c},
		}
		mvols := common.MemberVolumes{
			PrimaryVolumeID:   volPrefix + c,
			PrimaryDeviceName: dev1,
		}
		memberName := utils.GenServiceMemberName(service1, int64(i))
		memberMeta := db.CreateMemberMeta(mtime, common.ServiceMemberStatusActive)
		memberSpec := db.CreateMemberSpec(azPrefix+c, taskPrefix+c, contPrefix+c, hostPrefix+c, &mvols, staticIPPrefix+c, cfgs)
		s1[memberName] = db.CreateServiceMember(uuid1, memberName, 0, memberMeta, memberSpec)

		err := dbIns.CreateServiceMember(ctx, s1[memberName])
		if err != nil {
			glog.Fatalf("failed to create serviceMember %s, err %s", c, err)
		}

		item, err := dbIns.GetServiceMember(ctx, uuid1, memberName)
		if err != nil || !db.EqualServiceMember(item, s1[memberName], false) {
			glog.Fatalf("get serviceMember failed, error %s, expected %s get %s", err, s1[memberName], item)
		}
	}

	// create 4 serviceMembers for service2
	s2 := make(map[string]*common.ServiceMember)
	for i := 0; i < 4; i++ {
		c := x[i]
		cfgs := []common.ConfigID{
			common.ConfigID{FileName: fileNamePrefix + c, FileID: fileIDPrefix + c, FileMD5: fileMD5Prefix + c},
		}
		mvols := common.MemberVolumes{
			PrimaryVolumeID:   volPrefix + c,
			PrimaryDeviceName: dev2,
		}
		memberName := utils.GenServiceMemberName(service2, int64(i))
		memberMeta := db.CreateMemberMeta(mtime, common.ServiceMemberStatusActive)
		memberSpec := db.CreateMemberSpec(azPrefix+c, taskPrefix+c, contPrefix+c, hostPrefix+c, &mvols, staticIPPrefix+c, cfgs)
		s2[memberName] = db.CreateServiceMember(uuid2, memberName, 0, memberMeta, memberSpec)

		err := dbIns.CreateServiceMember(ctx, s2[memberName])
		if err != nil {
			glog.Fatalf("failed to create serviceMember %s, err %s", c, err)
		}

		item, err := dbIns.GetServiceMember(ctx, uuid2, memberName)
		if err != nil || !db.EqualServiceMember(item, s2[memberName], false) {
			glog.Fatalf("get serviceMember failed, error %s, expected %s get %s", err, s2[memberName], item)
		}
	}

	// get service to verify
	member1 := utils.GenServiceMemberName(service1, 1)
	item, err := dbIns.GetServiceMember(ctx, uuid1, member1)
	if err != nil || !db.EqualServiceMember(item, s1[member1], false) {
		glog.Fatalf("get serviceMember failed, error %s, expected %s get %s", err, s1[member1], item)
	}

	// update serviceMember
	item = db.UpdateServiceMemberOwner(item, taskPrefix+"z", contPrefix+"z", hostPrefix+"z")
	err = dbIns.UpdateServiceMember(ctx, s1[member1], item)
	if err != nil {
		glog.Fatalf("update serviceMember failed, serviceMember %s error %s", item, err)
	}

	// serviceMember updated
	s1[member1].Revision++
	s1[member1].Meta.LastModified = item.Meta.LastModified
	s1[member1].Spec.TaskID = item.Spec.TaskID
	s1[member1].Spec.ContainerInstanceID = item.Spec.ContainerInstanceID
	s1[member1].Spec.ServerInstanceID = item.Spec.ServerInstanceID

	// get serviceMember again to verify the update
	item, err = dbIns.GetServiceMember(ctx, uuid1, member1)
	if err != nil || !db.EqualServiceMember(item, s1[member1], false) {
		glog.Fatalf("get serviceMember after update failed, error %s, expected %s get %s", err, s1[member1], item)
	}

	// list serviceMembers of service1
	items, err := dbIns.ListServiceMembers(ctx, uuid1)
	if err != nil || len(items) != len(s1) {
		glog.Fatalf("expected %d serviceMembers for service %s, got %s, error %s",
			len(s1), uuid1, items, err)
	}
	for i, item := range items {
		if !db.EqualServiceMember(item, s1[item.MemberName], false) {
			glog.Fatalf("expected %s, got %s, index %d", s1[item.MemberName], item, i)
		}
	}

	// pagination list serviceMembers of service2
	items, err = dbIns.ListServiceMembersWithLimit(ctx, uuid2, 3)
	if err != nil || len(items) != len(s2) {
		glog.Fatalf("expected %d serviceMembers for service %s, got %s, error %s",
			len(s2), uuid2, items, err)
	}
	for i, item := range items {
		if !db.EqualServiceMember(item, s2[item.MemberName], false) {
			glog.Fatalf("expected %s, got %s, index %d", s2[item.MemberName], item, i)
		}
	}

	// delete serviceMember
	lastMember := utils.GenServiceMemberName(service1, int64(len(s1)-1))
	err = dbIns.DeleteServiceMember(ctx, uuid1, lastMember)
	if err != nil {
		glog.Fatalf("failed to delete serviceMember %s error %s", s1[lastMember], err)
	}

	// pagination list serviceMembers of service1
	items, err = dbIns.ListServiceMembersWithLimit(ctx, uuid1, 3)
	if err != nil || len(items) != (len(s1)-1) {
		glog.Fatalf("expected %d serviceMembers for service %s, got %s, error %s",
			len(s1)-1, uuid1, items, err)
	}
	for i, item := range items {
		if !db.EqualServiceMember(item, s1[item.MemberName], false) {
			glog.Fatalf("expected %s, got %s, index %d", s1[item.MemberName], item, i)
		}
	}

	// get one unexist serviceMember
	item, err = dbIns.GetServiceMember(ctx, uuid1, "non-exist-member")
	if err == nil || err != db.ErrDBRecordNotFound {
		glog.Fatalf("get unexist serviceMember, expect db.ErrDBRecordNotFound, got error %s item %s", err, item)
	}

	// delete one unexist serviceMember
	err = dbIns.DeleteServiceMember(ctx, uuid1, "non-exist")
	if err == nil || err != db.ErrDBRecordNotFound {
		glog.Fatalf("delete unexist serviceMember, expect db.ErrDBRecordNotFound, got error %s", err)
	}

	// cleanup: delete all members
	for _, s := range s1 {
		err := dbIns.DeleteServiceMember(ctx, uuid1, s.MemberName)
		if err != nil && err != db.ErrDBRecordNotFound {
			glog.Fatalf("failed to delete service member %s, err %s", s, err)
		}
	}
	for _, s := range s2 {
		err := dbIns.DeleteServiceMember(ctx, s.ServiceUUID, s.MemberName)
		if err != nil && err != db.ErrDBRecordNotFound {
			glog.Fatalf("failed to delete service member %s, err %s", s, err)
		}
	}

}

func testConfigFile(dbIns *k8sconfigdb.K8sConfigDB) {
	uuidPrefix := "uuid-"
	fileIDPrefix := "fileid-"
	fileNamePrefix := "filename-"
	fileContentPrefix := "filecontent-"
	fileMode := uint32(0600)

	ctx := context.Background()

	// create 5 services
	var s [5]*common.ConfigFile
	x := [5]string{"a", "b", "c", "d", "e"}
	for i, c := range x {
		s[i] = db.CreateInitialConfigFile(uuidPrefix+c, fileIDPrefix+c, fileNamePrefix+c, fileMode, fileContentPrefix+c)
		err := dbIns.CreateConfigFile(ctx, s[i])
		if err != nil {
			glog.Fatalf("failed to create config file %s, err %s", s[i], err)
		}

		// negative case: create config file again
		err = dbIns.CreateConfigFile(ctx, s[i])
		if err == nil {
			glog.Fatalf("create config file again, expect err but success", s[i])
		}
	}

	// get config to verify
	item, err := dbIns.GetConfigFile(ctx, s[1].ServiceUUID, s[1].FileID)
	if err != nil || !db.EqualConfigFile(item, s[1], false, false) {
		glog.Fatalf("get config file failed, error %s, expected %s get %s", err, s[1], item)
	}

	// delete config
	err = dbIns.DeleteConfigFile(ctx, s[2].ServiceUUID, s[2].FileID)
	if err != nil {
		glog.Fatalf("failed to delete config file %s error %s", s[2], err)
	}

	// negative case: delete one unexist config
	err = dbIns.DeleteConfigFile(ctx, s[2].ServiceUUID, s[2].FileID)
	if err == nil || err != db.ErrDBRecordNotFound {
		glog.Fatalf("delete unexist config %s, expect db.ErrDBRecordNotFound, got error %s", s[2], err)
	}

	// negative case: get one unexist config
	gitem, err := dbIns.GetConfigFile(ctx, "service-x", "config-x")
	if err == nil || err != db.ErrDBRecordNotFound {
		glog.Fatalf("get unexist config, expect db.ErrDBRecordNotFound, got error %s item %s", err, gitem)
	}

	// negative case: delete one unexist config
	err = dbIns.DeleteConfigFile(ctx, "service-x", "config-x")
	if err == nil || err != db.ErrDBRecordNotFound {
		glog.Fatalf("delete unexist config, expect db.ErrDBRecordNotFound, got error %s", err)
	}

	// cleanup: delete all records
	for _, item := range s {
		err := dbIns.DeleteConfigFile(ctx, item.ServiceUUID, item.FileID)
		if err != nil && err != db.ErrDBRecordNotFound {
			glog.Fatalf("failed to delete config file %s, err %s", item, err)
		}
	}
}

func testServiceStaticIPs(dbIns *k8sconfigdb.K8sConfigDB) {
	ipPrefix := "ip-"
	uuidPrefix := "uuid-"
	az := "test-az"
	instanceIDPrefix := "server-"
	netInterfacePrefix := "net-"

	ctx := context.Background()

	// create
	var s [5]*common.ServiceStaticIP
	x := [5]string{"a", "b", "c", "d", "e"}
	for i, c := range x {
		ipSpec := db.CreateStaticIPSpec(uuidPrefix+c, az, instanceIDPrefix+c, netInterfacePrefix+c)
		s[i] = db.CreateServiceStaticIP(ipPrefix+c, 0, ipSpec)
		err := dbIns.CreateServiceStaticIP(ctx, s[i])
		if err != nil {
			glog.Fatalf("failed to create %s, err %s", s[i], err)
		}
	}

	// get to verify
	item, err := dbIns.GetServiceStaticIP(ctx, s[1].StaticIP)
	if err != nil || !db.EqualServiceStaticIP(item, s[1]) {
		glog.Fatalf("get failed, error %s, expected %s get %s", err, s[1], item)
	}

	// update
	item = db.UpdateServiceStaticIP(item, "new-server", "new-netinterface")
	err = dbIns.UpdateServiceStaticIP(ctx, s[1], item)
	if err != nil {
		glog.Fatalf("update service %s error %s", item, err)
	}

	// get again to verify the update
	item1, err := dbIns.GetServiceStaticIP(ctx, s[1].StaticIP)
	if err != nil || !db.EqualServiceStaticIP(item1, item) {
		glog.Fatalf("get after update failed, error %s, expected %s get %s", err, item, item1)
	}

	// delete
	err = dbIns.DeleteServiceStaticIP(ctx, s[2].StaticIP)
	if err != nil {
		glog.Fatalf("delete %s error %s", s[2], err)
	}

	// delete one unexist item
	err = dbIns.DeleteServiceStaticIP(ctx, s[2].Spec.ServiceUUID)
	if err == nil || err != db.ErrDBRecordNotFound {
		glog.Fatalf("delete unexist item %s, expect db.ErrDBRecordNotFound, got error %s", s[2], err)
	}

	// get one unexist item
	gitem, err := dbIns.GetServiceStaticIP(ctx, "ipxxx")
	if err == nil || err != db.ErrDBRecordNotFound {
		glog.Fatalf("get unexist item, expect db.ErrDBRecordNotFound, got error %s item %s", err, gitem)
	}

	// cleanup: delete all records
	for _, item := range s {
		err := dbIns.DeleteServiceStaticIP(ctx, item.StaticIP)
		if err != nil && err != db.ErrDBRecordNotFound {
			glog.Fatalf("failed to delete config file %s, err %s", item, err)
		}
	}
}
