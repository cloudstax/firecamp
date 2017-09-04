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
	volSize := int64(1)
	cluster := "cluster"
	service := "service-1"
	devName := "dev-1"
	registerDNS := true
	domain := ""
	hostedZoneID := ""

	mtime := time.Now().UnixNano()
	attr1 := CreateInitialServiceAttr(serviceUUID, replicas, volSize,
		cluster, service, devName, registerDNS, domain, hostedZoneID)
	attr1.LastModified = mtime
	attr2 := CreateServiceAttr(serviceUUID, common.ServiceStatusCreating, mtime, replicas,
		volSize, cluster, service, devName, registerDNS, domain, hostedZoneID)
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
	member1 := CreateInitialServiceMember(serviceUUID, memberName, az, volID, devName, cfgs)
	member2 := CreateServiceMember(serviceUUID, memberName,
		az, DefaultTaskID, DefaultContainerInstanceID, DefaultServerInstanceID, mtime,
		volID, devName, cfgs)
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
}
