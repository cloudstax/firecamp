package controldbcli

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/openconnectio/openmanage/common"
	"github.com/openconnectio/openmanage/db"
)

func TestControlDB(t *testing.T) {
	testdir := "/tmp/test-" + strconv.FormatInt((time.Now().UnixNano()), 10)
	cluster := "cluster"

	s := &TestControlDBServer{Testdir: testdir, ListenPort: common.ControlDBServerPort}
	go s.RunControldbTestServer(cluster)
	defer s.StopControldbTestServer()

	ctx := context.Background()

	dbcli := NewControlDBCli("localhost:" + strconv.Itoa(common.ControlDBServerPort))
	err := testDevice(ctx, dbcli, cluster)
	if err != nil {
		t.Fatalf("test device error")
	}

	err = testService(ctx, dbcli, cluster)
	if err != nil {
		t.Fatalf("test service error")
	}

	err = testServiceAttr(ctx, dbcli, cluster)
	if err != nil {
		t.Fatalf("test service attr error")
	}

	err = testVolume(ctx, dbcli, cluster)
	if err != nil {
		t.Fatalf("test volume error")
	}
}

func testDevice(ctx context.Context, dbcli *ControlDBCli, cluster string) error {
	devPrefix := "/dev/xvda-"
	svcPrefix := "service-"

	// create 21 devices
	maxCounts := 21
	for i := 0; i < maxCounts; i++ {
		// create device
		str := strconv.Itoa(i)
		devName := devPrefix + str
		svcName := svcPrefix + str
		dev := db.CreateDevice(cluster, devName, svcName)
		err := dbcli.CreateDevice(ctx, dev)
		if err != nil {
			glog.Errorln("create device, expect success, got", err, "dev", dev)
			return err
		}

		// get device
		dev1, err := dbcli.GetDevice(ctx, cluster, devName)
		if err != nil {
			glog.Errorln("get device, expect success, got", err, "cluster", cluster, "devName", devName)
			return err
		}
		if !db.EqualDevice(dev, dev1) {
			glog.Errorln("get device, expect", dev, "got", dev1)
			return db.ErrDBInternal
		}

		// negative case: get non-exist device
		_, err = dbcli.GetDevice(ctx, cluster, devName+"xxx")
		if err != db.ErrDBRecordNotFound {
			glog.Errorln("get non-exist device, expect db.ErrDBRecordNotFound, got", err)
			return db.ErrDBInternal
		}

		// negative case: create the device again
		err = dbcli.CreateDevice(ctx, dev)
		if err != nil {
			glog.Errorln("create device, expect success, got", err, "dev", dev)
			return err
		}

		// negative case: create the device again for another service
		dev = db.CreateDevice(cluster, devName, svcName+"xxx")
		err = dbcli.CreateDevice(ctx, dev)
		if err != db.ErrDBConditionalCheckFailed {
			glog.Errorln("create existing device with different service, expect db.ErrDBConditionalCheckFailed, got", err, dev)
			return err
		}

		// list devices
		devs, err := dbcli.ListDevices(ctx, cluster)
		if err != nil {
			glog.Errorln("list devices error", err)
			return err
		}
		if len(devs) != i+1 {
			glog.Errorln("list devices, got", len(devs), "expect", i+1)
			return db.ErrDBInternal
		}
		for j := 0; j < len(devs); j++ {
			str = strconv.Itoa(j)
			devName = devPrefix + str
			svcName = svcPrefix + str
			dev = db.CreateDevice(cluster, devName, svcName)
			if !db.EqualDevice(dev, devs[j]) {
				glog.Errorln("list devices, expect", dev, "got", devs[j], "j", j)
				return db.ErrDBInternal
			}
		}
	}

	// delete 3 devices
	devName := devPrefix + strconv.Itoa(1)
	err := dbcli.DeleteDevice(ctx, cluster, devName)
	if err != nil {
		glog.Errorln("DeleteDevice error", err, devName)
		return err
	}
	devName = devPrefix + strconv.Itoa(4)
	err = dbcli.DeleteDevice(ctx, cluster, devName)
	if err != nil {
		glog.Errorln("DeleteDevice error", err, devName)
		return err
	}
	devName = devPrefix + strconv.Itoa(11)
	err = dbcli.DeleteDevice(ctx, cluster, devName)
	if err != nil {
		glog.Errorln("DeleteDevice error", err, devName)
		return err
	}
	// list devices again
	devs, err := dbcli.ListDevices(ctx, cluster)
	if err != nil {
		glog.Errorln("list devices error", err)
		return err
	}
	if len(devs) != maxCounts-3 {
		glog.Errorln("list devices, expect", maxCounts-3, "devices, got", len(devs))
		return db.ErrDBInternal
	}

	return nil
}

func testService(ctx context.Context, dbcli *ControlDBCli, cluster string) error {
	svcPrefix := "service-"
	uuidPrefix := "serviceuuid-"

	// create 21 services
	maxCounts := 21
	for i := 0; i < maxCounts; i++ {
		// create service
		str := strconv.Itoa(i)
		svcName := svcPrefix + str
		uuid := uuidPrefix + str
		svc := db.CreateService(cluster, svcName, uuid)
		err := dbcli.CreateService(ctx, svc)
		if err != nil {
			glog.Errorln("create service, expect success, got", err, svc)
			return err
		}

		// get service
		svc1, err := dbcli.GetService(ctx, cluster, svcName)
		if err != nil {
			glog.Errorln("get service, expect success, got", err, "cluster", cluster, "svcName", svcName)
			return err
		}
		if !db.EqualService(svc, svc1) {
			glog.Errorln("get service, expect", svc, "got", svc1)
			return db.ErrDBInternal
		}

		// negative case: get non-exist service
		_, err = dbcli.GetService(ctx, cluster, svcName+"xxx")
		if err != db.ErrDBRecordNotFound {
			glog.Errorln("get non-exist service, expect db.ErrDBRecordNotFound, got", err)
			return db.ErrDBInternal
		}

		// negative case: create the service again
		err = dbcli.CreateService(ctx, svc)
		if err != nil {
			glog.Errorln("create service again, expect success, got", err, "svc", svc)
			return err
		}

		// negative case: create the service again with another uuid
		svc = db.CreateService(cluster, svcName, uuid+"xxx")
		err = dbcli.CreateService(ctx, svc)
		if err != db.ErrDBConditionalCheckFailed {
			glog.Errorln("create existing service with different service, expect db.ErrDBConditionalCheckFailed, got", err, svc)
			return err
		}

		// list services
		svcs, err := dbcli.ListServices(ctx, cluster)
		if err != nil {
			glog.Errorln("list services error", err)
			return err
		}
		if len(svcs) != i+1 {
			glog.Errorln("list services, got", len(svcs), "expect", i+1)
			return db.ErrDBInternal
		}
		for j := 0; j < len(svcs); j++ {
			str = strconv.Itoa(j)
			svcName = svcPrefix + str
			uuid = uuidPrefix + str
			svc = db.CreateService(cluster, svcName, uuid)
			if !db.EqualService(svc, svcs[j]) {
				glog.Errorln("list services, expect", svc, "got", svcs[j], "j", j)
				return db.ErrDBInternal
			}
		}
	}

	// delete 3 services
	svcName := svcPrefix + strconv.Itoa(1)
	err := dbcli.DeleteService(ctx, cluster, svcName)
	if err != nil {
		glog.Errorln("DeleteService error", err, svcName)
		return err
	}
	svcName = svcPrefix + strconv.Itoa(4)
	err = dbcli.DeleteService(ctx, cluster, svcName)
	if err != nil {
		glog.Errorln("DeleteService error", err, svcName)
		return err
	}
	svcName = svcPrefix + strconv.Itoa(11)
	err = dbcli.DeleteService(ctx, cluster, svcName)
	if err != nil {
		glog.Errorln("DeleteService error", err, svcName)
		return err
	}
	// list services again
	svcs, err := dbcli.ListServices(ctx, cluster)
	if err != nil {
		glog.Errorln("list services error", err)
		return err
	}
	if len(svcs) != maxCounts-3 {
		glog.Errorln("list services, expect", maxCounts-3, "services, got", len(svcs))
		return db.ErrDBInternal
	}

	return nil
}

func testServiceAttr(ctx context.Context, dbcli *ControlDBCli, cluster string) error {
	serviceUUIDPrefix := "serviceuuid-"
	serviceStatus := "CREATING"
	serviceNamePrefix := "servicename-"
	devNamePrefix := "/dev/xvd"
	hasMembership := true
	domainPrefix := "domain"
	hostedZone := "zone1"

	// create 21 service attrs
	maxCounts := 21
	for i := 0; i < maxCounts; i++ {
		mtime := time.Now().UnixNano()
		// create service attr
		str := strconv.Itoa(i)
		uuid := serviceUUIDPrefix + str
		attr := db.CreateServiceAttr(uuid, serviceStatus, mtime, int64(i), int64(i), cluster,
			serviceNamePrefix+str, devNamePrefix+str, hasMembership, domainPrefix+str, hostedZone)
		err := dbcli.CreateServiceAttr(ctx, attr)
		if err != nil {
			glog.Errorln("create service, expect success, got", err, attr)
			return err
		}

		// negative case: create the service attr again
		err = dbcli.CreateServiceAttr(ctx, attr)
		if err != nil {
			glog.Errorln("create service attr again, expect success, got", err, "attr", attr)
			return err
		}

		// negative case: create the service attr again with different field
		attr1 := db.CreateServiceAttr(uuid, serviceStatus, mtime, int64(i), int64(i), cluster,
			serviceNamePrefix+str+"xxx", devNamePrefix+str, hasMembership, domainPrefix+str, hostedZone)
		err = dbcli.CreateServiceAttr(ctx, attr1)
		if err != db.ErrDBConditionalCheckFailed {
			glog.Errorln("create existing service attr with different field, expect db.ErrDBConditionalCheckFailed, got", err, attr1)
			return err
		}

		// get service attr
		attr2, err := dbcli.GetServiceAttr(ctx, uuid)
		if err != nil {
			glog.Errorln("get service attr, expect success, got", err, "cluster", cluster, "serviceuuid", uuid)
			return err
		}
		if !db.EqualServiceAttr(attr, attr2, false) {
			glog.Errorln("get service attr, expect", attr, "got", attr2)
			return db.ErrDBInternal
		}

		// negative case: get non-exist service attr
		_, err = dbcli.GetServiceAttr(ctx, uuid+"xxx")
		if err != db.ErrDBRecordNotFound {
			glog.Errorln("get non-exist service attr, expect db.ErrDBRecordNotFound, got error", err)
			return db.ErrDBInternal
		}

		// update service attr
		attr1 = db.CreateServiceAttr(uuid, "ACTIVE", time.Now().UnixNano(), int64(i), int64(i), cluster,
			serviceNamePrefix+str, devNamePrefix+str, hasMembership, domainPrefix+str, hostedZone)

		// negative case: old attr mismatch
		err = dbcli.UpdateServiceAttr(ctx, attr1, attr1)
		if err != db.ErrDBConditionalCheckFailed {
			glog.Errorln("UpdateServiceAttr expect db.ErrDBConditionalCheckFailed, got error", err, attr1)
			return db.ErrDBInternal
		}

		// update attr
		err = dbcli.UpdateServiceAttr(ctx, attr, attr1)
		if err != nil {
			glog.Errorln("UpdateServiceAttr expect success, got error", err, attr1)
			return db.ErrDBInternal
		}
	}

	// delete 3 service attrs
	uuid := serviceUUIDPrefix + strconv.Itoa(1)
	err := dbcli.DeleteServiceAttr(ctx, uuid)
	if err != nil {
		glog.Errorln("DeleteServiceAttr error", err, uuid)
		return err
	}
	_, err = dbcli.GetServiceAttr(ctx, uuid)
	if err != db.ErrDBRecordNotFound {
		glog.Errorln("get deleted service attr, expect db.ErrDBRecordNotFound, got", err)
		return err
	}
	uuid = serviceUUIDPrefix + strconv.Itoa(3)
	err = dbcli.DeleteServiceAttr(ctx, uuid)
	if err != nil {
		glog.Errorln("DeleteServiceAttr error", err, uuid)
		return err
	}
	_, err = dbcli.GetServiceAttr(ctx, uuid)
	if err != db.ErrDBRecordNotFound {
		glog.Errorln("get deleted service attr, expect db.ErrDBRecordNotFound, got", err)
		return err
	}
	uuid = serviceUUIDPrefix + strconv.Itoa(maxCounts-5)
	err = dbcli.DeleteServiceAttr(ctx, uuid)
	if err != nil {
		glog.Errorln("DeleteServiceAttr error", err, uuid)
		return err
	}
	_, err = dbcli.GetServiceAttr(ctx, uuid)
	if err != db.ErrDBRecordNotFound {
		glog.Errorln("get deleted service attr, expect db.ErrDBRecordNotFound, got", err)
		return err
	}

	return nil
}

func testVolume(ctx context.Context, dbcli *ControlDBCli, cluster string) error {
	serviceUUID := "serviceuuid-1"
	volIDPrefix := "volid-"
	devNamePrefix := "/dev/xvd-"
	az := "az-1"
	taskIDPrefix := "task-"
	contInsIDPrefix := "containerInstanceID-"
	serverInsIDPrefix := "serverInstanceID-"
	memberNamePrefix := "member-"
	updateSuffix := "-update"
	cfgIDPrefix := "cfgfileid-"
	cfgNamePrefix := "cfgname-"
	cfgMD5Prefix := "cfgmd5-"

	// create 21 volumes
	maxCounts := 21
	for i := 0; i < maxCounts; i++ {
		mtime := time.Now().UnixNano()
		// create volume
		str := fmt.Sprintf("%08x", i)
		volID := volIDPrefix + str
		cfg := &common.MemberConfig{FileID: cfgIDPrefix + str, FileName: cfgNamePrefix + str, FileMD5: cfgMD5Prefix + str}
		cfgs := []*common.MemberConfig{cfg}
		vol := db.CreateVolume(serviceUUID, volID, mtime, devNamePrefix+str, az,
			taskIDPrefix+str, contInsIDPrefix+str, serverInsIDPrefix+str, memberNamePrefix+str, cfgs)
		err := dbcli.CreateVolume(ctx, vol)
		if err != nil {
			glog.Errorln("create volume, expect success, got", err, vol)
			return err
		}

		// negative case: create the volume again
		err = dbcli.CreateVolume(ctx, vol)
		if err != nil {
			glog.Errorln("create volume again, expect success, got", err, "vol", vol)
			return err
		}

		// negative case: create the volume again with different field
		vol1 := db.CreateVolume(serviceUUID, volID, mtime, devNamePrefix+str, az,
			taskIDPrefix+str+updateSuffix, contInsIDPrefix+str, serverInsIDPrefix+str, memberNamePrefix+str, cfgs)
		err = dbcli.CreateVolume(ctx, vol1)
		if err != db.ErrDBConditionalCheckFailed {
			glog.Errorln("create existing volume with different field, expect db.ErrDBConditionalCheckFailed, got", err, vol1)
			return err
		}

		// get volume
		vol2, err := dbcli.GetVolume(ctx, serviceUUID, volID)
		if err != nil {
			glog.Errorln("get volume, expect success, got", err, "volumeID", volID)
			return err
		}
		if !db.EqualVolume(vol, vol2, false) {
			glog.Errorln("get volume, expect", vol, "got", vol2)
			return db.ErrDBInternal
		}

		// negative case: get non-exist volume
		_, err = dbcli.GetVolume(ctx, serviceUUID, volID+"xxx")
		if err != db.ErrDBRecordNotFound {
			glog.Errorln("get non-exist volume, expect db.ErrDBRecordNotFound, got error", err)
			return db.ErrDBInternal
		}

		// update volume
		vol1 = db.CreateVolume(serviceUUID, volID, time.Now().UnixNano(), devNamePrefix+str, az,
			taskIDPrefix+str+updateSuffix, contInsIDPrefix+str+updateSuffix,
			serverInsIDPrefix+str+updateSuffix, memberNamePrefix+str, cfgs)

		// negative case: old vol mismatch
		err = dbcli.UpdateVolume(ctx, vol1, vol1)
		if err != db.ErrDBConditionalCheckFailed {
			glog.Errorln("UpdateVolume expect db.ErrDBConditionalCheckFailed, got error", err, vol1)
			return db.ErrDBInternal
		}

		// update vol
		err = dbcli.UpdateVolume(ctx, vol, vol1)
		if err != nil {
			glog.Errorln("UpdateVolume expect success, got error", err, vol1)
			return db.ErrDBInternal
		}

		// list volumes
		vols, err := dbcli.ListVolumes(ctx, serviceUUID)
		if err != nil {
			glog.Errorln("list volumes error", err)
			return err
		}
		if len(vols) != i+1 {
			glog.Errorln("list volumes, got", len(vols), "expect", i+1)
			return db.ErrDBInternal
		}
	}

	// delete 3 volumes
	volID := volIDPrefix + fmt.Sprintf("%08x", 2)
	err := dbcli.DeleteVolume(ctx, serviceUUID, volID)
	if err != nil {
		glog.Errorln("DeleteVolume error", err, volID)
		return err
	}
	_, err = dbcli.GetVolume(ctx, serviceUUID, volID)
	if err != db.ErrDBRecordNotFound {
		glog.Errorln("get deleted volume, expect db.ErrDBRecordNotFound, got", err)
		return err
	}
	volID = volIDPrefix + fmt.Sprintf("%08x", 9)
	err = dbcli.DeleteVolume(ctx, serviceUUID, volID)
	if err != nil {
		glog.Errorln("DeleteVolume error", err, volID)
		return err
	}
	_, err = dbcli.GetVolume(ctx, serviceUUID, volID)
	if err != db.ErrDBRecordNotFound {
		glog.Errorln("get deleted volume, expect db.ErrDBRecordNotFound, got", err)
		return err
	}
	volID = volIDPrefix + fmt.Sprintf("%08x", maxCounts-5)
	err = dbcli.DeleteVolume(ctx, serviceUUID, volID)
	if err != nil {
		glog.Errorln("DeleteVolume error", err, volID)
		return err
	}
	_, err = dbcli.GetVolume(ctx, serviceUUID, volID)
	if err != db.ErrDBRecordNotFound {
		glog.Errorln("get deleted volume, expect db.ErrDBRecordNotFound, got", err)
		return err
	}

	vols, err := dbcli.ListVolumes(ctx, serviceUUID)
	if err != nil {
		glog.Errorln("list volumes error", err)
		return err
	}
	if len(vols) != maxCounts-3 {
		glog.Errorln("list volumes, got", len(vols), "expect", maxCounts-3)
		return db.ErrDBInternal
	}

	return nil
}

func testConfigFile(ctx context.Context, dbcli *ControlDBCli, cluster string) error {
	serviceUUIDPrefix := "serviceuuid-"
	fileIDPrefix := "fileid-"
	fileNamePrefix := "filename-"
	fileContentPrefix := "filecontent-"

	// create 21 config files
	maxCounts := 21
	for i := 0; i < maxCounts; i++ {
		// create config file
		str := strconv.Itoa(i)
		serviceUUID := serviceUUIDPrefix + str
		fileID := fileIDPrefix + str
		fileName := fileNamePrefix + str
		fileContent := fileContentPrefix + str
		cfg := db.CreateInitialConfigFile(serviceUUID, fileID, fileName, fileContent)
		err := dbcli.CreateConfigFile(ctx, cfg)
		if err != nil {
			glog.Errorln("create config file, expect success, got", err, cfg)
			return err
		}

		// negative case: create the config file again
		err = dbcli.CreateConfigFile(ctx, cfg)
		if err != nil {
			glog.Errorln("create config file again, expect success, got", err, "cfg", cfg)
			return err
		}

		// negative case: create the config file again with different field
		cfg1 := db.CreateInitialConfigFile(serviceUUID, fileID, fileName, fileContent)
		err = dbcli.CreateConfigFile(ctx, cfg1)
		if err != db.ErrDBConditionalCheckFailed {
			glog.Errorln("create existing config file with different field, expect db.ErrDBConditionalCheckFailed, got", err, cfg1)
			return err
		}

		// get config file
		cfg2, err := dbcli.GetConfigFile(ctx, serviceUUID, fileID)
		if err != nil {
			glog.Errorln("get config file, expect success, got", err, "serviceuuid", serviceUUID, "fileID", fileID)
			return err
		}
		if !db.EqualConfigFile(cfg, cfg2, false, false) {
			glog.Errorln("get config file, expect", cfg, "got", cfg2)
			return db.ErrDBInternal
		}

		// negative case: get non-exist config file
		_, err = dbcli.GetConfigFile(ctx, serviceUUID+"xxx", fileID)
		if err != db.ErrDBRecordNotFound {
			glog.Errorln("get non-exist config file, expect db.ErrDBRecordNotFound, got error", err)
			return db.ErrDBInternal
		}
	}

	// delete 2 config files
	str := strconv.Itoa(1)
	serviceUUID := serviceUUIDPrefix + str
	fileID := fileIDPrefix + str
	err := dbcli.DeleteConfigFile(ctx, serviceUUID, fileID)
	if err != nil {
		glog.Errorln("DeleteConfigFile error", err, serviceUUID, fileID)
		return err
	}
	_, err = dbcli.GetConfigFile(ctx, serviceUUID, fileID)
	if err != db.ErrDBRecordNotFound {
		glog.Errorln("get deleted config file, expect db.ErrDBRecordNotFound, got", err)
		return err
	}

	str = strconv.Itoa(3)
	serviceUUID = serviceUUIDPrefix + str
	fileID = fileIDPrefix + str
	err = dbcli.DeleteConfigFile(ctx, serviceUUID, fileID)
	if err != nil {
		glog.Errorln("DeleteConfigFile error", err, serviceUUID, fileID)
		return err
	}
	_, err = dbcli.GetConfigFile(ctx, serviceUUID, fileID)
	if err != db.ErrDBRecordNotFound {
		glog.Errorln("get deleted config file, expect db.ErrDBRecordNotFound, got", err)
		return err
	}

	return nil
}
