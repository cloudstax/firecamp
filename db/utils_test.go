package db

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/utils"
)

func TestDBUtils(t *testing.T) {
	serviceUUID := "uuid-1"
	replicas := int64(1)
	cluster := "cluster"
	service := "service-1"
	svols := common.ServiceVolumes{
		PrimaryDeviceName: "dev-1",
		PrimaryVolume: common.ServiceVolume{
			VolumeType:   common.VolumeTypeGPSSD,
			VolumeSizeGB: 1,
		},
		JournalDeviceName: "dev-2",
		JournalVolume: common.ServiceVolume{
			VolumeType:   common.VolumeTypeGPSSD,
			VolumeSizeGB: 1,
		},
	}
	registerDNS := true
	domain := ""
	hostedZoneID := ""
	requireStaticIP := false
	res := common.Resources{
		MaxCPUUnits:     common.DefaultMaxCPUUnits,
		ReserveCPUUnits: common.DefaultReserveCPUUnits,
		MaxMemMB:        common.DefaultMaxMemoryMB,
		ReserveMemMB:    common.DefaultReserveMemoryMB,
	}

	mtime := time.Now().UnixNano()
	attr1 := CreateInitialServiceAttr(serviceUUID, replicas,
		cluster, service, svols, registerDNS, domain, hostedZoneID, requireStaticIP, nil, nil, res, "")
	attr1.LastModified = mtime
	attr2 := CreateServiceAttr(serviceUUID, common.ServiceStatusCreating, mtime, replicas,
		cluster, service, svols, registerDNS, domain, hostedZoneID, requireStaticIP, nil, nil, res, "")
	if !EqualServiceAttr(attr1, attr2, false) {
		t.Fatalf("attr is not the same, %s %s", attr1, attr2)
	}

	attr2 = UpdateServiceStatus(attr1, common.ServiceStatusActive)
	attr1.ServiceStatus = common.ServiceStatusActive
	attr1.LastModified = attr2.LastModified
	if !EqualServiceAttr(attr1, attr2, false) {
		t.Fatalf("attr is not the same, %s %s", attr1, attr2)
	}

	attr2 = UpdateServiceReplicas(attr1, 5)
	attr1.Replicas = 5
	attr1.LastModified = attr2.LastModified
	if !EqualServiceAttr(attr1, attr2, false) {
		t.Fatalf("attr is not the same, %s %s", attr1, attr2)
	}

	mattr := common.MongoDBUserAttr{
		Shards:           1,
		ReplicasPerShard: 3,
		ReplicaSetOnly:   false,
		ConfigServers:    3,
		KeyFileContent:   "keyfile",
	}
	b, err := json.Marshal(mattr)
	if err != nil {
		t.Fatalf("Marshal MongoDBUserAttr error %s", err)
	}
	userAttr := &common.ServiceUserAttr{
		ServiceType: common.CatalogService_MongoDB,
		AttrBytes:   b,
	}
	cfg := &common.ConfigID{FileName: "cfgfile-name", FileID: "cfgfile-id", FileMD5: "cfgfile-md5"}
	cfgs := []*common.ConfigID{cfg}
	attr3 := CreateInitialServiceAttr(serviceUUID, replicas,
		cluster, service, svols, registerDNS, domain, hostedZoneID, requireStaticIP, userAttr, cfgs, res, common.ServiceTypeStateless)
	attr3.LastModified = mtime
	attr4 := CreateServiceAttr(serviceUUID, common.ServiceStatusCreating, mtime, replicas,
		cluster, service, svols, registerDNS, domain, hostedZoneID, requireStaticIP, userAttr, cfgs, res, common.ServiceTypeStateless)
	if !EqualServiceAttr(attr3, attr4, false) {
		t.Fatalf("attr is not the same, %s %s", attr3, attr4)
	}

	// update service userAttr
	mattr.Shards = 2
	b, err = json.Marshal(mattr)
	if err != nil {
		t.Fatalf("Marshal MongoDBUserAttr error %s", err)
	}
	userAttr = &common.ServiceUserAttr{
		ServiceType: common.CatalogService_MongoDB,
		AttrBytes:   b,
	}
	tmpattr := UpdateServiceUserAttr(attr4, userAttr)
	attr3.UserAttr = userAttr
	attr3.LastModified = tmpattr.LastModified
	if !EqualServiceAttr(attr3, tmpattr, false) {
		t.Fatalf("attr is not the same, %s %s", attr3, tmpattr)
	}

	volID := "vol-1"
	az := "az-1"
	memberIndex := int64(1)
	memberName := "member-1"
	staticIP := "10.0.0.1"
	mvols := common.MemberVolumes{
		PrimaryVolumeID:   volID,
		PrimaryDeviceName: svols.PrimaryDeviceName,
	}
	member1 := CreateInitialServiceMember(serviceUUID, memberIndex, memberName, az, mvols, staticIP, cfgs)
	member2 := CreateServiceMember(serviceUUID, memberIndex, common.ServiceMemberStatusActive, memberName,
		az, DefaultTaskID, DefaultContainerInstanceID, DefaultServerInstanceID, mtime,
		mvols, staticIP, cfgs)
	if !EqualServiceMember(member1, member2, true) {
		t.Fatalf("serviceMember is not the same, %s %s", member1, member2)
	}

	taskID := "task-1"
	containerInstanceID := "containerInstance-1"
	ec2InstanceID := "ec2Instance-1"
	member2 = UpdateServiceMemberOwner(member1, taskID, containerInstanceID, ec2InstanceID)
	member1.TaskID = taskID
	member1.ContainerInstanceID = containerInstanceID
	member1.ServerInstanceID = ec2InstanceID
	member1.LastModified = member2.LastModified
	if !EqualServiceMember(member1, member2, false) {
		t.Fatalf("serviceMember is not the same, %s %s", member1, member2)
	}

	// test ConfigFile
	fileID := "cfgfile-1"
	fileName := "cfgfile-name"
	fileMode := uint32(0600)
	content := "cfgfile-content"
	chksum := utils.GenMD5(content)
	cfg1 := CreateInitialConfigFile(serviceUUID, fileID, fileName, fileMode, content)
	cfg1.LastModified = mtime
	cfg2, err := CreateConfigFile(serviceUUID, fileID, chksum, fileName, fileMode, mtime, content)
	if err != nil {
		t.Fatalf("CreateConfigFile error %s", err)
	}
	if !EqualConfigFile(cfg1, cfg2, false, false) {
		t.Fatalf("configfile is not the same, %s %s", cfg1, cfg2)
	}

	newFileID := "newID"
	newContent := "newContent"
	cfg3 := UpdateConfigFile(cfg1, newFileID, newContent)
	cfg1.FileID = newFileID
	cfg1.FileMD5 = cfg3.FileMD5
	cfg1.LastModified = cfg3.LastModified
	cfg1.Content = newContent
	if !EqualConfigFile(cfg1, cfg3, false, false) {
		t.Fatalf("configfile is not the same, %s %s", cfg1, cfg3)
	}

	// test service static ip
	netInterfaceID := "test-netinterface"
	serviceip := CreateServiceStaticIP(staticIP, serviceUUID, az, ec2InstanceID, netInterfaceID)

	ec2InstanceID1 := "new-ec2"
	netInterfaceID1 := "new-netinterface"
	serviceip1 := UpdateServiceStaticIP(serviceip, ec2InstanceID1, netInterfaceID1)
	serviceip.ServerInstanceID = ec2InstanceID1
	serviceip.NetworkInterfaceID = netInterfaceID1
	if !EqualServiceStaticIP(serviceip, serviceip1) {
		t.Fatalf("service static ip is not the same, %s %s", serviceip, serviceip1)
	}
}
