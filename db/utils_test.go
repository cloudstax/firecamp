package db

import (
	"testing"
	"time"

	"github.com/openconnectio/openmanage/common"
	"github.com/openconnectio/openmanage/utils"
)

func TestUtils(t *testing.T) {
	serviceUUID := "uuid-1"
	replicas := int64(1)
	volSize := int64(1)
	cluster := "cluster"
	service := "service-1"
	devName := "dev-1"
	hasStrictMembership := true
	domain := ""
	hostedZoneID := ""

	mtime := time.Now().UnixNano()
	attr1 := CreateInitialServiceAttr(serviceUUID, replicas, volSize,
		cluster, service, devName, hasStrictMembership, domain, hostedZoneID)
	attr1.LastModified = mtime
	attr2 := CreateServiceAttr(serviceUUID, common.ServiceStatusCreating, mtime, replicas,
		volSize, cluster, service, devName, hasStrictMembership, domain, hostedZoneID)
	if !EqualServiceAttr(attr1, attr2, false) {
		t.Fatalf("attr is not the same, %s %s", attr1, attr2)
	}

	attr2 = UpdateServiceAttr(attr1, common.ServiceStatusActive)
	attr1.ServiceStatus = common.ServiceStatusActive
	attr1.LastModified = attr2.LastModified
	if !EqualServiceAttr(attr1, attr2, false) {
		t.Fatalf("attr is not the same, %s %s", attr1, attr2)
	}

	volID := "vol-1"
	az := "az-1"
	memberName := "member-1"
	cfg := &common.MemberConfig{FileName: "cfgfile-name", FileID: "cfgfile-id", FileMD5: "cfgfile-md5"}
	cfgs := []*common.MemberConfig{cfg}
	vol1 := CreateInitialVolume(serviceUUID, volID, devName, az, memberName, cfgs)
	vol2 := CreateVolume(serviceUUID, volID, mtime, devName, az, DefaultTaskID,
		DefaultContainerInstanceID, DefaultServerInstanceID, memberName, cfgs)
	if !EqualVolume(vol1, vol2, true) {
		t.Fatalf("volume is not the same, %s %s", vol1, vol2)
	}

	taskID := "task-1"
	containerInstanceID := "containerInstance-1"
	ec2InstanceID := "ec2Instance-1"
	vol2 = UpdateVolume(vol1, taskID, containerInstanceID, ec2InstanceID)
	vol1.TaskID = taskID
	vol1.ContainerInstanceID = containerInstanceID
	vol1.ServerInstanceID = ec2InstanceID
	vol1.LastModified = vol2.LastModified
	if !EqualVolume(vol1, vol2, false) {
		t.Fatalf("volume is not the same, %s %s", vol1, vol2)
	}

	// test ConfigFile
	fileID := "cfgfile-1"
	fileName := "cfgfile-name"
	content := "cfgfile-content"
	chksum := utils.GenMD5(content)
	cfg1 := CreateInitialConfigFile(serviceUUID, fileID, fileName, content)
	cfg1.LastModified = mtime
	cfg2, err := CreateConfigFile(serviceUUID, fileID, chksum, fileName, mtime, content)
	if err != nil {
		t.Fatalf("CreateConfigFile error %s", err)
	}
	if !EqualConfigFile(cfg1, cfg2, false, false) {
		t.Fatalf("configfile is not the same, %s %s", cfg1, cfg2)
	}
}
