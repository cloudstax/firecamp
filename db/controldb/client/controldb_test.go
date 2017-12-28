package controldbcli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
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

	err = testServiceMember(ctx, dbcli, cluster)
	if err != nil {
		t.Fatalf("test serviceMember error")
	}

	err = testServiceStaticIP(ctx, dbcli, cluster)
	if err != nil {
		t.Fatalf("test serviceStaticIP error")
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
	registerDNS := true
	domainPrefix := "domain"
	hostedZone := "zone1"
	requireStaticIP := false
	res := common.Resources{
		MaxCPUUnits:     common.DefaultMaxCPUUnits,
		ReserveCPUUnits: common.DefaultReserveCPUUnits,
		MaxMemMB:        common.DefaultMaxMemoryMB,
		ReserveMemMB:    common.DefaultReserveMemoryMB,
	}

	// create 21 service attrs
	var err error
	maxCounts := 21
	for i := 0; i < maxCounts; i++ {
		mtime := time.Now().UnixNano()
		// create service attr
		str := strconv.Itoa(i)
		uuid := serviceUUIDPrefix + str
		svols := common.ServiceVolumes{
			PrimaryDeviceName: devNamePrefix + str,
			PrimaryVolume: common.ServiceVolume{
				VolumeType:   common.VolumeTypeGPSSD,
				VolumeSizeGB: int64(i),
			},
			JournalDeviceName: devNamePrefix + "log" + str,
			JournalVolume: common.ServiceVolume{
				VolumeType:   common.VolumeTypeIOPSSSD,
				VolumeSizeGB: int64(i),
			},
		}
		var userAttr *common.ServiceUserAttr
		if i%2 == 0 {
			rattr := &common.CasUserAttr{
				HeapSizeMB: 256,
			}
			b, err := json.Marshal(rattr)
			if err != nil {
				glog.Errorln("Marshal user attr error", err)
				return err
			}
			userAttr = &common.ServiceUserAttr{
				ServiceType: common.CatalogService_Cassandra,
				AttrBytes:   b,
			}
		}
		attr := db.CreateServiceAttr(uuid, serviceStatus, mtime, int64(i), cluster,
			serviceNamePrefix+str, svols, registerDNS, domainPrefix+str, hostedZone, requireStaticIP, userAttr, res)
		err := dbcli.CreateServiceAttr(ctx, attr)
		if err != nil {
			glog.Errorln("create service, expect success, got", err, attr)
			return err
		}

		// negative case: create the service attr again
		err = dbcli.CreateServiceAttr(ctx, attr)
		if err != db.ErrDBConditionalCheckFailed {
			glog.Errorln("create service attr again, expect db.ErrDBConditionalCheckFailed, got", err, "attr", attr)
			return err
		}

		// negative case: create the service attr again with different field
		attr1 := db.CreateServiceAttr(uuid, serviceStatus, mtime, int64(i), cluster,
			serviceNamePrefix+str+"xxx", svols, registerDNS, domainPrefix+str, hostedZone, requireStaticIP, userAttr, res)
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
		attr1 = db.CreateServiceAttr(uuid, "ACTIVE", time.Now().UnixNano(), int64(i), cluster,
			serviceNamePrefix+str, svols, registerDNS, domainPrefix+str, hostedZone, requireStaticIP, userAttr, res)

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

func testServiceMember(ctx context.Context, dbcli *ControlDBCli, cluster string) error {
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
	staticIPPrefix := "ip-"

	// create 21 serviceMembers
	maxCounts := 21
	for i := 0; i < maxCounts; i++ {
		mtime := time.Now().UnixNano()
		// create serviceMember
		str := fmt.Sprintf("%08x", i)
		memberName := memberNamePrefix + str
		cfg := &common.MemberConfig{FileID: cfgIDPrefix + str, FileName: cfgNamePrefix + str, FileMD5: cfgMD5Prefix + str}
		cfgs := []*common.MemberConfig{cfg}
		mvols := common.MemberVolumes{
			PrimaryVolumeID:   volIDPrefix + str,
			PrimaryDeviceName: devNamePrefix + str,
		}
		member := db.CreateServiceMember(serviceUUID,
			int64(i),
			memberName,
			az,
			taskIDPrefix+str,
			contInsIDPrefix+str,
			serverInsIDPrefix+str,
			mtime,
			mvols,
			staticIPPrefix+str,
			cfgs)
		err := dbcli.CreateServiceMember(ctx, member)
		if err != nil {
			glog.Errorln("create serviceMember, expect success, got", err, member)
			return err
		}

		// negative case: create the serviceMember again
		err = dbcli.CreateServiceMember(ctx, member)
		if err != nil {
			glog.Errorln("create serviceMember again, expect success, got", err, "member", member)
			return err
		}

		// negative case: create the serviceMember again with different field
		member1 := db.CreateServiceMember(serviceUUID,
			int64(i),
			memberName,
			az,
			taskIDPrefix+str+updateSuffix,
			contInsIDPrefix+str,
			serverInsIDPrefix+str,
			mtime,
			mvols,
			staticIPPrefix+str,
			cfgs)
		err = dbcli.CreateServiceMember(ctx, member1)
		if err != db.ErrDBConditionalCheckFailed {
			glog.Errorln("create existing serviceMember with different field, expect db.ErrDBConditionalCheckFailed, got", err, member1)
			return err
		}

		// get serviceMember
		member2, err := dbcli.GetServiceMember(ctx, serviceUUID, int64(i))
		if err != nil {
			glog.Errorln("get serviceMember, expect success, got", err, "memberName", memberName)
			return err
		}
		if !db.EqualServiceMember(member, member2, false) {
			glog.Errorln("get serviceMember, expect", member, "got", member2)
			return db.ErrDBInternal
		}

		// negative case: get non-exist serviceMember
		_, err = dbcli.GetServiceMember(ctx, serviceUUID, 1000)
		if err != db.ErrDBRecordNotFound {
			glog.Errorln("get non-exist serviceMember, expect db.ErrDBRecordNotFound, got error", err)
			return db.ErrDBInternal
		}

		// update serviceMember
		member1 = db.CreateServiceMember(serviceUUID,
			int64(i),
			memberName,
			az,
			taskIDPrefix+str+updateSuffix,
			contInsIDPrefix+str+updateSuffix,
			serverInsIDPrefix+str+updateSuffix,
			time.Now().UnixNano(),
			mvols,
			staticIPPrefix+str,
			cfgs)

		// negative case: old member mismatch
		err = dbcli.UpdateServiceMember(ctx, member1, member1)
		if err != db.ErrDBConditionalCheckFailed {
			glog.Errorln("UpdateServiceMember expect db.ErrDBConditionalCheckFailed, got error", err, member1)
			return db.ErrDBInternal
		}

		// update member
		err = dbcli.UpdateServiceMember(ctx, member, member1)
		if err != nil {
			glog.Errorln("UpdateServiceMember expect success, got error", err, member1)
			return db.ErrDBInternal
		}

		// list serviceMembers
		members, err := dbcli.ListServiceMembers(ctx, serviceUUID)
		if err != nil {
			glog.Errorln("list serviceMembers error", err)
			return err
		}
		if len(members) != i+1 {
			glog.Errorln("list serviceMembers, got", len(members), "expect", i+1)
			return db.ErrDBInternal
		}
	}

	// delete 3 serviceMembers
	memberIndex := int64(2)
	err := dbcli.DeleteServiceMember(ctx, serviceUUID, 2)
	if err != nil {
		glog.Errorln("DeleteServiceMember error", err, memberIndex)
		return err
	}
	_, err = dbcli.GetServiceMember(ctx, serviceUUID, memberIndex)
	if err != db.ErrDBRecordNotFound {
		glog.Errorln("get deleted serviceMember, expect db.ErrDBRecordNotFound, got", err)
		return err
	}
	memberIndex = int64(9)
	err = dbcli.DeleteServiceMember(ctx, serviceUUID, memberIndex)
	if err != nil {
		glog.Errorln("DeleteServiceMember error", err, memberIndex)
		return err
	}
	_, err = dbcli.GetServiceMember(ctx, serviceUUID, memberIndex)
	if err != db.ErrDBRecordNotFound {
		glog.Errorln("get deleted serviceMember, expect db.ErrDBRecordNotFound, got", err)
		return err
	}
	memberIndex = int64(maxCounts - 5)
	err = dbcli.DeleteServiceMember(ctx, serviceUUID, memberIndex)
	if err != nil {
		glog.Errorln("DeleteServiceMember error", err, memberIndex)
		return err
	}
	_, err = dbcli.GetServiceMember(ctx, serviceUUID, memberIndex)
	if err != db.ErrDBRecordNotFound {
		glog.Errorln("get deleted serviceMember, expect db.ErrDBRecordNotFound, got", err)
		return err
	}

	members, err := dbcli.ListServiceMembers(ctx, serviceUUID)
	if err != nil {
		glog.Errorln("list serviceMembers error", err)
		return err
	}
	if len(members) != maxCounts-3 {
		glog.Errorln("list serviceMembers, got", len(members), "expect", maxCounts-3)
		return db.ErrDBInternal
	}

	return nil
}

func testConfigFile(ctx context.Context, dbcli *ControlDBCli, cluster string) error {
	serviceUUIDPrefix := "serviceuuid-"
	fileIDPrefix := "fileid-"
	fileNamePrefix := "filename-"
	fileMode := uint32(0600)
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
		cfg := db.CreateInitialConfigFile(serviceUUID, fileID, fileName, fileMode, fileContent)
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
		cfg1 := db.CreateInitialConfigFile(serviceUUID, fileID, fileName, fileMode, fileContent)
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

func testServiceStaticIP(ctx context.Context, dbcli *ControlDBCli, cluster string) error {
	ipPrefix := "ip-"
	serviceUUIDPrefix := "serviceuuid-"
	az := "az-1"
	serverInsIDPrefix := "server-"
	netInterfaceIDPrefix := "netinterface-"

	// create 9 service serviceips
	maxCounts := 9
	for i := 0; i < maxCounts; i++ {
		// create serviceip
		str := strconv.Itoa(i)
		serviceip := db.CreateServiceStaticIP(ipPrefix+str, serviceUUIDPrefix+str, az,
			serverInsIDPrefix+str, netInterfaceIDPrefix+str)
		err := dbcli.CreateServiceStaticIP(ctx, serviceip)
		if err != nil {
			glog.Errorln("create serviceip, expect success, got", err, serviceip)
			return err
		}

		// negative case: create the serviceip again
		err = dbcli.CreateServiceStaticIP(ctx, serviceip)
		if err != nil {
			glog.Errorln("create service serviceip again, expect success, got", err, "attr", serviceip)
			return err
		}

		// negative case: create the serviceip again with different field
		serviceip1 := db.CreateServiceStaticIP(ipPrefix+str, serviceUUIDPrefix+str, az,
			serverInsIDPrefix+str+"xxx", netInterfaceIDPrefix+str)
		err = dbcli.CreateServiceStaticIP(ctx, serviceip1)
		if err != db.ErrDBConditionalCheckFailed {
			glog.Errorln("create existing service serviceip with different field, expect db.ErrDBConditionalCheckFailed, got", err, serviceip1)
			return err
		}

		// get serviceip
		serviceip2, err := dbcli.GetServiceStaticIP(ctx, ipPrefix+str)
		if err != nil {
			glog.Errorln("get serviceip, expect success, got", err, "cluster", cluster, "ip", ipPrefix+str)
			return err
		}
		if !db.EqualServiceStaticIP(serviceip, serviceip2) {
			glog.Errorln("get service serviceip, expect", serviceip, "got", serviceip2)
			return db.ErrDBInternal
		}

		// negative case: get non-exist serviceip
		_, err = dbcli.GetServiceStaticIP(ctx, ipPrefix+str+"xxx")
		if err != db.ErrDBRecordNotFound {
			glog.Errorln("get non-exist serviceip, expect db.ErrDBRecordNotFound, got error", err)
			return db.ErrDBInternal
		}

		// update serviceip
		serviceip1 = db.CreateServiceStaticIP(ipPrefix+str, serviceUUIDPrefix+str, az,
			serverInsIDPrefix+str+"xxx", netInterfaceIDPrefix+str+"xxx")

		// negative case: old serviceip mismatch
		err = dbcli.UpdateServiceStaticIP(ctx, serviceip1, serviceip1)
		if err != db.ErrDBConditionalCheckFailed {
			glog.Errorln("UpdateServiceStaticIP expect db.ErrDBConditionalCheckFailed, got error", err, serviceip1)
			return db.ErrDBInternal
		}

		// update serviceip
		err = dbcli.UpdateServiceStaticIP(ctx, serviceip, serviceip1)
		if err != nil {
			glog.Errorln("UpdateServiceStaticIP expect success, got error", err, serviceip1)
			return db.ErrDBInternal
		}
	}

	// delete 3 service serviceips
	ip := ipPrefix + strconv.Itoa(1)
	err := dbcli.DeleteServiceStaticIP(ctx, ip)
	if err != nil {
		glog.Errorln("DeleteServiceStaticIP error", err, ip)
		return err
	}
	_, err = dbcli.GetServiceStaticIP(ctx, ip)
	if err != db.ErrDBRecordNotFound {
		glog.Errorln("get deleted service serviceip, expect db.ErrDBRecordNotFound, got", err)
		return err
	}

	ip = ipPrefix + strconv.Itoa(3)
	err = dbcli.DeleteServiceStaticIP(ctx, ip)
	if err != nil {
		glog.Errorln("DeleteServiceStaticIP error", err, ip)
		return err
	}
	_, err = dbcli.GetServiceStaticIP(ctx, ip)
	if err != db.ErrDBRecordNotFound {
		glog.Errorln("get deleted service serviceip, expect db.ErrDBRecordNotFound, got", err)
		return err
	}

	ip = ipPrefix + strconv.Itoa(5)
	err = dbcli.DeleteServiceStaticIP(ctx, ip)
	if err != nil {
		glog.Errorln("DeleteServiceStaticIP error", err, ip)
		return err
	}
	_, err = dbcli.GetServiceStaticIP(ctx, ip)
	if err != db.ErrDBRecordNotFound {
		glog.Errorln("get deleted service serviceip, expect db.ErrDBRecordNotFound, got", err)
		return err
	}

	return nil
}
