package awsdynamodb

import (
	"flag"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/db"
	"github.com/cloudstax/openmanage/utils"
)

var region = flag.String("region", "us-west-1", "The target AWS region for DynamoDB")

var dbIns *DynamoDB

func createTables(ctx context.Context) error {
	err := dbIns.CreateSystemTables(ctx)
	if err != nil {
		glog.Errorln("failed to CreateSystemTables", err)
		return err
	}

	return dbIns.WaitSystemTablesReady(ctx, 120)
}

func TestMain(m *testing.M) {
	flag.Parse()

	config := aws.NewConfig().WithRegion(*region)
	sess, err := session.NewSession(config)
	if err != nil {
		glog.Errorln("CreateServiceAttr failed to create session, error", err)
		return
	}

	tableNameSuffix := utils.GenUUID()
	dbIns = NewTestDynamoDB(sess, tableNameSuffix)

	ctx := context.Background()

	err = createTables(ctx)
	if err != nil {
		dbIns.DeleteSystemTables(ctx)
		dbIns.WaitSystemTablesDeleted(ctx, 120)
		os.Exit(-1)
	}

	code := m.Run()

	err = dbIns.DeleteSystemTables(ctx)
	if err != nil {
		os.Exit(-1)
	}
	err = dbIns.WaitSystemTablesDeleted(ctx, 120)
	if err != nil {
		os.Exit(-1)
	}
	os.Exit(code)
}

func TestDevices(t *testing.T) {
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
			t.Fatalf("failed to create db.Device %s, error %s", c, err)
		}
	}

	// create xvdf again, expect to fail
	t1 := db.CreateDevice(clusterName, devPrefix+x[0], servicePrefix+x[0])
	err := dbIns.CreateDevice(ctx, t1)
	if err != db.ErrDBConditionalCheckFailed {
		t.Fatalf("create existing db.Device %s, expect fail but status is %s", t1, err)
	}

	// get xvdf
	t2, err := dbIns.GetDevice(ctx, t1.ClusterName, t1.DeviceName)
	if err != nil || !db.EqualDevice(t1, t2) {
		t.Fatalf("get db.Device not the same %s %s error %s", t1, t2, err)
	}

	// list Devices
	items, err := dbIns.ListDevices(ctx, clusterName)
	if err != nil || len(items) != len(x) {
		t.Fatalf("Listdb.Devices failed, get items %s error %s", items, err)
	}
	for i, item := range items {
		expectItem := db.CreateDevice(clusterName, devPrefix+x[i], servicePrefix+x[i])
		if !db.EqualDevice(expectItem, item) {
			t.Fatalf("Listdb.Devices failed, expected %s got %s, index %d", expectItem, item, i)
		}
	}

	// pagination list
	items, err = dbIns.listDevicesWithLimit(ctx, clusterName, 1)
	if err != nil || len(items) != len(x) {
		t.Fatalf("Listdb.Devices failed, get items %s error %s", items, err)
	}
	for i, item := range items {
		expectItem := db.CreateDevice(clusterName, devPrefix+x[i], servicePrefix+x[i])
		if !db.EqualDevice(expectItem, item) {
			t.Fatalf("Listdb.Devices failed, expected %s got %s, index %d", expectItem, item, i)
		}
	}

	// delete xvdk
	err = dbIns.DeleteDevice(ctx, clusterName, devPrefix+x[5])
	if err != nil {
		t.Fatalf("failed to delete db.Device %s error %s", t1, err)
	}

	// pagination list again after deletion
	items, err = dbIns.listDevicesWithLimit(ctx, clusterName, 2)
	if err != nil || len(items) != (len(x)-1) {
		t.Fatalf("Listdb.Devices failed, get items %s error %s", items, err)
	}
	for i, item := range items {
		expectItem := db.CreateDevice(clusterName, devPrefix+x[i], servicePrefix+x[i])
		if !db.EqualDevice(expectItem, item) {
			t.Fatalf("Listdb.Devices failed, expected %s got %s, index %d", expectItem, item, i)
		}
	}

	// get unexist device
	item, err := dbIns.GetDevice(ctx, "cluster-x", "dev-x")
	if err == nil || err != db.ErrDBRecordNotFound {
		t.Fatalf("get unexist device, expect db.ErrDBRecordNotFound, got error %s item %s", err, item)
	}

	// delete one unexist device
	err = dbIns.DeleteDevice(ctx, "cluster-x", "dev-x")
	if err == nil || err != db.ErrDBRecordNotFound {
		t.Fatalf("delete unexist device, expect db.ErrDBRecordNotFound, got error %s", err)
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
		s[i] = db.CreateService(clusterName, servicePrefix+c, uuidPrefix+c)
		err := dbIns.CreateService(ctx, s[i])
		if err != nil {
			t.Fatalf("failed to create service %s, err %s", s[i], err)
		}
	}

	// list all services
	services, err := dbIns.ListServices(ctx, clusterName)
	if err != nil || len(services) != 5 {
		t.Fatalf("ListServices failed, error %s, expected 5 services, got %d", err, len(services))
	}

	// get service to verify
	item, err := dbIns.GetService(ctx, s[1].ClusterName, s[1].ServiceName)
	if err != nil || !db.EqualService(item, s[1]) {
		t.Fatalf("get service failed, error %s, expected %s get %s", err, s[1], item)
	}

	// pagination list all services
	services, err = dbIns.listServicesWithLimit(ctx, clusterName, 2)
	if err != nil || len(services) != 5 {
		t.Fatalf("ListServices failed, error %s, expected 5 services, got %d", err, len(services))
	}

	// delete service
	err = dbIns.DeleteService(ctx, s[2].ClusterName, s[2].ServiceName)
	if err != nil {
		t.Fatalf("failed to delete service %s error %s", s[2], err)
	}

	// delete one unexist service
	err = dbIns.DeleteService(ctx, s[2].ClusterName, s[2].ServiceName)
	if err == nil || err != db.ErrDBRecordNotFound {
		t.Fatalf("delete unexist service %s, expect db.ErrDBRecordNotFound, got error %s", s[2], err)
	}

	// get one unexist service
	item, err = dbIns.GetService(ctx, "cluster-x", "service-x")
	if err == nil || err != db.ErrDBRecordNotFound {
		t.Fatalf("get unexist service, expect db.ErrDBRecordNotFound, got error %s item %s", err, item)
	}

	// delete one unexist service
	err = dbIns.DeleteService(ctx, "cluster-x", "service-x")
	if err == nil || err != db.ErrDBRecordNotFound {
		t.Fatalf("delete unexist service, expect db.ErrDBRecordNotFound, got error %s", err)
	}
}

func TestServiceAttrs(t *testing.T) {
	uuidPrefix := "uuid-"
	clusterName := "cluster1"
	servicePrefix := "service-"
	volSize := 10
	devPrefix := "/dev/xvd"
	registerDNS := true
	domain := "domain"
	hostedZoneID := "hostedZoneID"

	ctx := context.Background()

	// create 5 services
	var s [5]*common.ServiceAttr
	x := [5]string{"a", "b", "c", "d", "e"}
	for i, c := range x {
		s[i] = db.CreateInitialServiceAttr(uuidPrefix+c, int64(i), int64(volSize+i),
			clusterName, servicePrefix+c, devPrefix+c, registerDNS, domain, hostedZoneID)
		err := dbIns.CreateServiceAttr(ctx, s[i])
		if err != nil {
			t.Fatalf("failed to create service attr %s, err %s", s[i], err)
		}
	}

	// get service to verify
	item, err := dbIns.GetServiceAttr(ctx, s[1].ServiceUUID)
	if err != nil || !db.EqualServiceAttr(item, s[1], false) {
		t.Fatalf("get service attr failed, error %s, expected %s get %s", err, s[1], item)
	}

	// update service
	item.ServiceStatus = "ACTIVE"
	err = dbIns.UpdateServiceAttr(ctx, s[1], item)
	if err != nil {
		t.Fatalf("update service attr failed, service %s error %s", item, err)
	}

	// service updated
	s[1].ServiceStatus = "ACTIVE"

	// get service again to verify the update
	item, err = dbIns.GetServiceAttr(ctx, s[1].ServiceUUID)
	if err != nil || !db.EqualServiceAttr(item, s[1], false) {
		t.Fatalf("get service attr after update failed, error %s, expected %s get %s", err, s[1], item)
	}

	// delete service
	err = dbIns.DeleteServiceAttr(ctx, s[2].ServiceUUID)
	if err != nil {
		t.Fatalf("failed to delete service attr %s error %s", s[2], err)
	}

	// delete one unexist service
	err = dbIns.DeleteServiceAttr(ctx, s[2].ServiceUUID)
	if err == nil || err != db.ErrDBRecordNotFound {
		t.Fatalf("delete unexist service %s, expect db.ErrDBRecordNotFound, got error %s", s[2], err)
	}

	// get one unexist service
	gitem, err := dbIns.GetService(ctx, "cluster-x", "service-x")
	if err == nil || err != db.ErrDBRecordNotFound {
		t.Fatalf("get unexist service, expect db.ErrDBRecordNotFound, got error %s item %s", err, gitem)
	}

	// delete one unexist service
	err = dbIns.DeleteService(ctx, "cluster-x", "service-x")
	if err == nil || err != db.ErrDBRecordNotFound {
		t.Fatalf("delete unexist service, expect db.ErrDBRecordNotFound, got error %s", err)
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
	azPrefix := "az-"
	fileIDPrefix := "cfgfile-id"
	fileNamePrefix := "cfgfile-name"
	fileMD5Prefix := "cfgfile-md5"
	mtime := time.Now().UnixNano()

	ctx := context.Background()

	// create 6 serviceMembers for service1
	x := [6]string{"a", "b", "c", "d", "e", "f"}
	var s1 [6]*common.ServiceMember
	for i, c := range x {
		cfg := &common.MemberConfig{FileName: fileNamePrefix + c, FileID: fileIDPrefix + c, FileMD5: fileMD5Prefix + c}
		cfgs := []*common.MemberConfig{cfg}
		s1[i] = db.CreateServiceMember(service1, volPrefix+c, mtime, dev1, azPrefix+c, taskPrefix+c, contPrefix+c, hostPrefix+c, service1+c, cfgs)

		err := dbIns.CreateServiceMember(ctx, s1[i])
		if err != nil {
			t.Fatalf("failed to create serviceMember %s, err %s", c, err)
		}
	}

	// create 4 serviceMembers for service2
	var s2 [4]*common.ServiceMember
	for i := 0; i < 4; i++ {
		c := x[i]
		cfg := &common.MemberConfig{FileName: fileNamePrefix + c, FileID: fileIDPrefix + c, FileMD5: fileMD5Prefix + c}
		cfgs := []*common.MemberConfig{cfg}
		s2[i] = db.CreateServiceMember(service2, volPrefix+c, mtime, dev2, azPrefix+c, taskPrefix+c, contPrefix+c, hostPrefix+c, service2+c, cfgs)

		err := dbIns.CreateServiceMember(ctx, s2[i])
		if err != nil {
			t.Fatalf("failed to create serviceMember %s, err %s", c, err)
		}
	}

	// get service to verify
	item, err := dbIns.GetServiceMember(ctx, s1[1].ServiceUUID, s1[1].MemberName)
	if err != nil || !db.EqualServiceMember(item, s1[1], false) {
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
	item, err = dbIns.GetServiceMember(ctx, s1[1].ServiceUUID, s1[1].MemberName)
	if err != nil || !db.EqualServiceMember(item, s1[1], false) {
		t.Fatalf("get serviceMember after update failed, error %s, expected %s get %s", err, s1[1], item)
	}

	// list serviceMembers of service1
	items, err := dbIns.ListServiceMembers(ctx, s1[0].ServiceUUID)
	if err != nil || len(items) != len(s1) {
		t.Fatalf("expected %d serviceMembers for service %s, got %s, error %s",
			len(s1), s1[0].ServiceUUID, items, err)
	}
	for i, item := range items {
		if !db.EqualServiceMember(item, s1[i], false) {
			t.Fatalf("expected %s, got %s, index %d", s1[i], item, i)
		}
	}

	// pagination list serviceMembers of service2
	items, err = dbIns.listServiceMembersWithLimit(ctx, s2[0].ServiceUUID, 3)
	if err != nil || len(items) != len(s2) {
		t.Fatalf("expected %d serviceMembers for service %s, got %s, error %s",
			len(s2), s2[0].ServiceUUID, items, err)
	}
	for i, item := range items {
		if !db.EqualServiceMember(item, s2[i], false) {
			t.Fatalf("expected %s, got %s, index %d", s2[i], item, i)
		}
	}

	// delete serviceMember
	err = dbIns.DeleteServiceMember(ctx, s1[len(s1)-1].ServiceUUID, s1[len(s1)-1].MemberName)
	if err != nil {
		t.Fatalf("failed to delete serviceMember %s error %s", s1[len(s1)-1], err)
	}

	// pagination list serviceMembers of service1
	items, err = dbIns.listServiceMembersWithLimit(ctx, s1[0].ServiceUUID, 3)
	if err != nil || len(items) != (len(s1)-1) {
		t.Fatalf("expected %d serviceMembers for service %s, got %s, error %s",
			len(s1)-1, s1[0].ServiceUUID, items, err)
	}
	for i, item := range items {
		if !db.EqualServiceMember(item, s1[i], false) {
			t.Fatalf("expected %s, got %s, index %d", s1[i], item, i)
		}
	}

	// get one unexist serviceMember
	item, err = dbIns.GetServiceMember(ctx, s1[0].ServiceUUID, "mem")
	if err == nil || err != db.ErrDBRecordNotFound {
		t.Fatalf("get unexist serviceMember, expect db.ErrDBRecordNotFound, got error %s item %s", err, item)
	}

	// delete one unexist serviceMember
	err = dbIns.DeleteServiceMember(ctx, s1[0].ServiceUUID, "mem")
	if err == nil || err != db.ErrDBRecordNotFound {
		t.Fatalf("delete unexist serviceMember, expect db.ErrDBRecordNotFound, got error %s", err)
	}
}

func TestConfigFile(t *testing.T) {
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
			t.Fatalf("failed to create config file %s, err %s", s[i], err)
		}

		// negative case: create config file again
		err = dbIns.CreateConfigFile(ctx, s[i])
		if err == nil {
			t.Fatalf("create config file again, expect err but success", s[i])
		}
	}

	// get config to verify
	item, err := dbIns.GetConfigFile(ctx, s[1].ServiceUUID, s[1].FileID)
	if err != nil || !db.EqualConfigFile(item, s[1], false, false) {
		t.Fatalf("get config file failed, error %s, expected %s get %s", err, s[1], item)
	}

	// delete config
	err = dbIns.DeleteConfigFile(ctx, s[2].ServiceUUID, s[2].FileID)
	if err != nil {
		t.Fatalf("failed to delete config file %s error %s", s[2], err)
	}

	// negative case: delete one unexist config
	err = dbIns.DeleteConfigFile(ctx, s[2].ServiceUUID, s[2].FileID)
	if err == nil || err != db.ErrDBRecordNotFound {
		t.Fatalf("delete unexist config %s, expect db.ErrDBRecordNotFound, got error %s", s[2], err)
	}

	// negative case: get one unexist config
	gitem, err := dbIns.GetConfigFile(ctx, "service-x", "config-x")
	if err == nil || err != db.ErrDBRecordNotFound {
		t.Fatalf("get unexist config, expect db.ErrDBRecordNotFound, got error %s item %s", err, gitem)
	}

	// negative case: delete one unexist config
	err = dbIns.DeleteConfigFile(ctx, "service-x", "config-x")
	if err == nil || err != db.ErrDBRecordNotFound {
		t.Fatalf("delete unexist config, expect db.ErrDBRecordNotFound, got error %s", err)
	}
}
