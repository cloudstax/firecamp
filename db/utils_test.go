package db

import (
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

	cfgs := []common.ConfigID{
		common.ConfigID{FileName: "fname", FileID: "fid", FileMD5: "fmd5"},
	}

	// test service attr
	mtime := time.Now().UnixNano()
	attrMeta := CreateServiceMeta(cluster, service, mtime, common.ServiceTypeStateful, common.ServiceStatusCreating)
	attrSpec := CreateServiceSpec(replicas, &res, registerDNS, domain, hostedZoneID, requireStaticIP, cfgs, &svols)
	attr1 := CreateServiceAttr(serviceUUID, 0, attrMeta, attrSpec)

	attr2 := UpdateServiceStatus(attr1, common.ServiceStatusActive)
	attr1.Revision++
	attr1.Meta.ServiceStatus = common.ServiceStatusActive
	attr1.Meta.LastModified = attr2.Meta.LastModified
	if !EqualServiceAttr(attr1, attr2, false) {
		t.Fatalf("attr is not the same, %s %s", attr1, attr2)
	}

	attr2 = UpdateServiceReplicas(attr1, 5)
	attr1.Revision++
	attr1.Spec.Replicas = 5
	attr1.Meta.LastModified = attr2.Meta.LastModified
	if !EqualServiceAttr(attr1, attr2, false) {
		t.Fatalf("attr is not the same, %s %s", attr1, attr2)
	}

	// test service member
	volID := "vol-1"
	az := "az-1"
	memberIndex := int64(1)
	memberName := "member-1"
	staticIP := "10.0.0.1"
	mvols := common.MemberVolumes{
		PrimaryVolumeID:   volID,
		PrimaryDeviceName: svols.PrimaryDeviceName,
	}
	memberMeta := CreateMemberMeta(memberName, time.Now().UnixNano(), common.ServiceMemberStatusActive)
	memberSpec := CreateMemberSpec(az, DefaultTaskID, DefaultContainerInstanceID, DefaultServerInstanceID, &mvols, staticIP, cfgs)
	m1 := CreateServiceMember(serviceUUID, memberIndex, 0, memberMeta, memberSpec)

	taskID := "task-1"
	containerInstanceID := "containerInstance-1"
	ec2InstanceID := "ec2Instance-1"
	m2 := UpdateServiceMemberOwner(m1, taskID, containerInstanceID, ec2InstanceID)

	m1.Revision++
	m1.Meta.LastModified = m2.Meta.LastModified
	m1.Spec.TaskID = taskID
	m1.Spec.ContainerInstanceID = containerInstanceID
	m1.Spec.ServerInstanceID = ec2InstanceID
	if !EqualServiceMember(m1, m2, false) {
		t.Fatalf("serviceMember is not the same, %s %s", m1, m2)
	}

	// test ConfigFile
	fileID := "cfgfile-1"
	fileName := "cfgfile-name"
	fileMode := uint32(0600)
	content := "cfgfile-content"
	chksum := utils.GenMD5(content)
	cfg1 := CreateInitialConfigFile(serviceUUID, fileID, fileName, fileMode, content)
	cfg1.Meta.LastModified = mtime

	meta := CreateConfigFileMeta(fileName, mtime)
	spec := CreateConfigFileSpec(fileMode, chksum, content)
	cfg2 := CreateConfigFile(serviceUUID, fileID, 0, meta, spec)
	if !EqualConfigFile(cfg1, cfg2, false, false) {
		t.Fatalf("configfile is not the same, %s %s", cfg1, cfg2)
	}

	newFileID := "newID"
	newContent := "newContent"
	cfg3 := CreateNewConfigFile(cfg1, newFileID, newContent)
	cfg1.FileID = newFileID
	cfg1.Revision++
	cfg1.Meta.LastModified = cfg3.Meta.LastModified
	cfg1.Spec.FileMD5 = cfg3.Spec.FileMD5
	cfg1.Spec.Content = newContent
	if !EqualConfigFile(cfg1, cfg3, false, false) {
		t.Fatalf("configfile is not the same, %s %s", cfg1, cfg3)
	}

	// test service static ip
	netInterfaceID := "test-netinterface"
	ipSpec := CreateStaticIPSpec(serviceUUID, az, ec2InstanceID, netInterfaceID)
	serviceip := CreateServiceStaticIP(staticIP, 0, ipSpec)

	ec2InstanceID1 := "new-ec2"
	netInterfaceID1 := "new-netinterface"
	serviceip1 := UpdateServiceStaticIP(serviceip, ec2InstanceID1, netInterfaceID1)
	serviceip.Revision++
	serviceip.Spec.ServerInstanceID = ec2InstanceID1
	serviceip.Spec.NetworkInterfaceID = netInterfaceID1
	if !EqualServiceStaticIP(serviceip, serviceip1) {
		t.Fatalf("service static ip is not the same, %s %s", serviceip, serviceip1)
	}
}
