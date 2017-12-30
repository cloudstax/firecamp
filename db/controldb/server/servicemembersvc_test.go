package controldbserver

import (
	"flag"
	"os"
	"strconv"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/db/controldb"
	pb "github.com/cloudstax/firecamp/db/controldb/protocols"
)

func TestServiceMemberReadWriter(t *testing.T) {
	flag.Parse()
	ctx := context.Background()
	ti := time.Now().UnixNano()
	rootdir := "/tmp/fileutil" + strconv.FormatInt(ti, 10)
	serviceUUID := "serviceuuid-1"

	s, err := newServiceMemberReadWriter(ctx, rootdir, serviceUUID)
	if err != nil {
		t.Fatalf("newServiceMemberReadWriter error %s, rootdir %s serviceUUID %s", err, rootdir, serviceUUID)
	}

	memberNamePrefix := "member-"
	volIDPrefix := "volid-"
	devNamePrefix := "/dev/xvd-"
	az := "az-1"
	taskIDPrefix := "task-"
	contInsIDPrefix := "containerInstanceID-"
	serverInsIDPrefix := "serverInstanceID-"
	updateSuffix := "-update"
	cfgIDPrefix := "cfgfileid-"
	cfgNamePrefix := "cfgname-"
	cfgMD5Prefix := "cfgmd5-"
	staticIPPrefix := "ip-"

	maxNum := 17
	for i := 0; i < maxNum; i++ {
		str := strconv.Itoa(i)
		memberName := memberNamePrefix + str
		devName := devNamePrefix + str
		taskID := taskIDPrefix + str
		contInsID := contInsIDPrefix + str
		serverInsID := serverInsIDPrefix + str
		volID := volIDPrefix + str
		staticIP := staticIPPrefix + str
		cfg := &pb.MemberConfig{FileID: cfgIDPrefix + str, FileName: cfgNamePrefix + str, FileMD5: cfgMD5Prefix + str}
		cfgs := []*pb.MemberConfig{cfg}

		mvols := &pb.MemberVolumes{
			PrimaryVolumeID:   volID,
			PrimaryDeviceName: devName,
		}
		member := &pb.ServiceMember{
			ServiceUUID:         serviceUUID,
			MemberIndex:         int64(i),
			Status:              common.ServiceMemberStatusActive,
			MemberName:          memberName,
			AvailableZone:       az,
			TaskID:              taskID,
			ContainerInstanceID: contInsID,
			ServerInstanceID:    serverInsID,
			Volumes:             mvols,
			StaticIP:            staticIP,
			Configs:             cfgs,
		}
		err = s.createServiceMember(ctx, member)
		if err != nil {
			t.Fatalf("createServiceMember error %s rootdir %s", err, rootdir)
		}

		// test the request retry
		err = s.createServiceMember(ctx, member)
		if err != nil {
			t.Fatalf("createServiceMember error %s rootdir %s", err, rootdir)
		}

		// create the serviceMember again with different mtime
		// copy serviceMember as s.createServiceMember returns the pointer, member and member1 point to the same pb.ServiceMember
		member1 := controldb.CopyServiceMember(member)
		member1.LastModified = time.Now().UnixNano()
		err = s.createServiceMember(ctx, member1)
		if err != nil {
			t.Fatalf("create the existing serviceMember with different mtime, expect success, got %s, rootdir %s", err, rootdir)
		}

		// negative case: create the existing serviceMember with different fields
		member1.ContainerInstanceID = "ContainerInstanceID-xxx"
		err = s.createServiceMember(ctx, member1)
		if err != db.ErrDBConditionalCheckFailed {
			t.Fatalf("create the existing serviceMember, expect error db.ErrDBConditionalCheckFailed, got %s, rootdir %s", err, rootdir)
		}

		// negative: get non-exist member
		key := &pb.ServiceMemberKey{
			ServiceUUID: serviceUUID,
			MemberIndex: 100,
		}
		member1, err = s.getServiceMember(ctx, key)
		if err != db.ErrDBRecordNotFound {
			t.Fatalf("get non-exist serviceMember, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
		}
		// get member
		key = &pb.ServiceMemberKey{
			ServiceUUID: serviceUUID,
			MemberIndex: int64(i),
		}
		member1, err = s.getServiceMember(ctx, key)
		if err != nil {
			t.Fatalf("getServiceMember error %s key %s rootdir %s", err, key, rootdir)
		}
		if !controldb.EqualServiceMember(member, member1, false) {
			t.Fatalf("getServiceMember returns %s, expect %s rootdir %s", member1, member, rootdir)
		}

		// test update member
		// copy serviceMember as s.getServiceMember returns the pointer, member and member1 point to the same pb.ServiceMember
		member2 := controldb.CopyServiceMember(member1)
		member2.MemberIndex = 1000
		// negative case: OldMember non-exist serviceMember
		req := &pb.UpdateServiceMemberRequest{
			OldMember: member2,
			NewMember: member2,
		}
		err = s.updateServiceMember(ctx, req)
		if err != db.ErrDBRecordNotFound {
			t.Fatalf("update non-exist serviceMember, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
		}
		// negative case: OldMember mismatch
		member2.MemberIndex = member.MemberIndex
		member2.ContainerInstanceID = member.ContainerInstanceID + updateSuffix
		member2.ServerInstanceID = member.ServerInstanceID + updateSuffix
		member2.TaskID = member.TaskID + updateSuffix
		req = &pb.UpdateServiceMemberRequest{
			OldMember: member2,
			NewMember: member2,
		}
		err = s.updateServiceMember(ctx, req)
		if err != db.ErrDBConditionalCheckFailed {
			t.Fatalf("updateServiceMember expect db.ErrDBConditionalCheckFailed, got %s, rootdir %s", err, rootdir)
		}
		// update member
		req = &pb.UpdateServiceMemberRequest{
			OldMember: member,
			NewMember: member2,
		}
		err = s.updateServiceMember(ctx, req)
		if err != nil {
			t.Fatalf("updateServiceMember error %s rootdir %s", err, rootdir)
		}

		// test member list
		lreq := &pb.ListServiceMemberRequest{
			ServiceUUID: serviceUUID,
		}
		members, err := s.listServiceMembers(lreq)
		if err != nil {
			t.Fatalf("listServiceMembers error %s, rootdir %s serviceUUID %s", err, rootdir, serviceUUID)
		}
		if len(members) != i+1 {
			t.Fatalf("listServiceMembers expect %d serviceMembers, got %d, rootdir %s, serviceUUID %s",
				i+1, len(members), rootdir, serviceUUID)
		}
	}

	// delete 6 serviceMembers
	idx := 0
	for i := 0; i < 6; i++ {
		idx += 2

		// negative case: delete non-exist serviceMember
		key := &pb.ServiceMemberKey{
			ServiceUUID: serviceUUID,
			MemberIndex: 100,
		}
		err = s.deleteServiceMember(ctx, key)
		if err != db.ErrDBRecordNotFound {
			t.Fatalf("delete non-exist serviceMember, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
		}

		// delete serviceMember
		key = &pb.ServiceMemberKey{
			ServiceUUID: serviceUUID,
			MemberIndex: int64(idx),
		}
		err = s.deleteServiceMember(ctx, key)
		if err != nil {
			t.Fatalf("deleteServiceMember error %s rootdir %s", err, rootdir)
		}

		// test member list
		lreq := &pb.ListServiceMemberRequest{
			ServiceUUID: serviceUUID,
		}
		members, err := s.listServiceMembers(lreq)
		if err != nil {
			t.Fatalf("listServiceMembers error %s, rootdir %s serviceUUID %s", err, rootdir, serviceUUID)
		}
		if len(members) != maxNum-i-1 {
			t.Fatalf("listServiceMembers expect %d serviceMembers, got %d, rootdir %s, serviceUUID %s",
				maxNum-i-1, len(members), rootdir, serviceUUID)
		}
	}

	// cleanup
	os.Remove(rootdir)
}

func testServiceMemberOp(t *testing.T, s *serviceMemberSvc, serviceUUID string, i int, rootdir string) {
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

	ctx := context.Background()
	str := strconv.Itoa(i)
	volID := volIDPrefix + str
	devName := devNamePrefix + str
	taskID := taskIDPrefix + str
	contInsID := contInsIDPrefix + str
	serverInsID := serverInsIDPrefix + str
	memberName := memberNamePrefix + str
	staticIP := staticIPPrefix + str
	cfg := &pb.MemberConfig{FileID: cfgIDPrefix + str, FileName: cfgNamePrefix + str, FileMD5: cfgMD5Prefix + str}
	cfgs := []*pb.MemberConfig{cfg}

	mvols := &pb.MemberVolumes{
		PrimaryVolumeID:   volID,
		PrimaryDeviceName: devName,
	}
	member := &pb.ServiceMember{
		ServiceUUID:         serviceUUID,
		MemberIndex:         int64(i),
		Status:              common.ServiceMemberStatusActive,
		MemberName:          memberName,
		AvailableZone:       az,
		TaskID:              taskID,
		ContainerInstanceID: contInsID,
		ServerInstanceID:    serverInsID,
		Volumes:             mvols,
		StaticIP:            staticIP,
		Configs:             cfgs,
	}

	err := s.CreateServiceMember(ctx, member)
	if err != nil {
		t.Fatalf("createServiceMember error %s rootdir %s", err, rootdir)
	}

	// test the request retry
	err = s.CreateServiceMember(ctx, member)
	if err != nil {
		t.Fatalf("createServiceMember error %s rootdir %s", err, rootdir)
	}

	// negative case: create the existing serviceMember with different fields
	// copy serviceMember as s.createServiceMember returns the pointer, member and member1 point to the same pb.ServiceMember
	member1 := controldb.CopyServiceMember(member)
	member1.ContainerInstanceID = "ContainerInstanceID-a"
	err = s.CreateServiceMember(ctx, member1)
	if err != db.ErrDBConditionalCheckFailed {
		t.Fatalf("create the existing serviceMember, expect error db.ErrDBConditionalCheckFailed, got %s, rootdir %s", err, rootdir)
	}

	// negative: get non-exist member
	key := &pb.ServiceMemberKey{
		ServiceUUID: serviceUUID,
		MemberIndex: 100,
	}
	_, err = s.GetServiceMember(ctx, key)
	if err != db.ErrDBRecordNotFound {
		t.Fatalf("get non-exist serviceMember, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
	}
	// get member
	key = &pb.ServiceMemberKey{
		ServiceUUID: serviceUUID,
		MemberIndex: int64(i),
	}
	member1, err = s.GetServiceMember(ctx, key)
	if err != nil {
		t.Fatalf("getServiceMember error %s key %s rootdir %s", err, key, rootdir)
	}
	if !controldb.EqualServiceMember(member, member1, false) {
		t.Fatalf("getServiceMember returns %s, expect %s rootdir %s", member1, member, rootdir)
	}

	// test update member
	// copy serviceMember as s.getServiceMember returns the pointer, member and member1 point to the same pb.ServiceMember
	member2 := controldb.CopyServiceMember(member1)
	member2.MemberIndex = 1000
	// negative case: OldMember non-exist serviceMember
	req := &pb.UpdateServiceMemberRequest{
		OldMember: member2,
		NewMember: member2,
	}
	err = s.UpdateServiceMember(ctx, req)
	if err != db.ErrDBRecordNotFound {
		t.Fatalf("update non-exist serviceMember, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
	}
	// negative case: OldMember mismatch
	member2.MemberIndex = member.MemberIndex
	member2.ContainerInstanceID = member.ContainerInstanceID + updateSuffix
	member2.ServerInstanceID = member.ServerInstanceID + updateSuffix
	member2.TaskID = member.TaskID + updateSuffix
	req = &pb.UpdateServiceMemberRequest{
		OldMember: member2,
		NewMember: member2,
	}
	err = s.UpdateServiceMember(ctx, req)
	if err != db.ErrDBConditionalCheckFailed {
		t.Fatalf("updateServiceMember expect db.ErrDBConditionalCheckFailed, got %s, rootdir %s", err, rootdir)
	}
	// update member
	req = &pb.UpdateServiceMemberRequest{
		OldMember: member,
		NewMember: member2,
	}
	err = s.UpdateServiceMember(ctx, req)
	if err != nil {
		t.Fatalf("updateServiceMember error %s rootdir %s", err, rootdir)
	}

	// test member list
	lreq := &pb.ListServiceMemberRequest{
		ServiceUUID: serviceUUID,
	}
	members, err := s.listServiceMembers(ctx, lreq)
	if err != nil {
		t.Fatalf("listServiceMembers error %s, rootdir %s serviceUUID %s", err, rootdir, serviceUUID)
	}
	if len(members) != 1 {
		t.Fatalf("listServiceMembers expect %d serviceMembers, got %d, rootdir %s, serviceUUID %s",
			1, len(members), rootdir, serviceUUID)
	}

}

func TestServiceMemberSvc(t *testing.T) {
	flag.Parse()
	time.Sleep(2 * time.Second)
	ti := time.Now().UnixNano()
	rootdir := "/tmp/fileutil" + strconv.FormatInt(ti, 10)
	// set the max cache size to test lru evict
	maxCacheSize := 5

	s := newServiceMemberSvcWithMaxCacheSize(rootdir, maxCacheSize)

	ctx := context.Background()

	serviceUUIDPrefix := "serviceuuid-"
	for i := 0; i < maxCacheSize; i++ {
		serviceUUID := serviceUUIDPrefix + strconv.Itoa(i)
		testServiceMemberOp(t, s, serviceUUID, i, rootdir)

		// member should be in lruCache
		_, ok := s.lruCache.Get(serviceUUID)
		if !ok {
			t.Fatalf("service %s should be in lruCache", serviceUUID)
		}
	}

	// create 2 more member
	maxNum := maxCacheSize + 2
	for i := maxCacheSize; i < maxNum; i++ {
		serviceUUID := serviceUUIDPrefix + strconv.Itoa(i)
		testServiceMemberOp(t, s, serviceUUID, i, rootdir)
	}

	// verify the first 2 service members are not in lruCache any more
	serviceUUID := serviceUUIDPrefix + strconv.Itoa(0)
	_, ok := s.lruCache.Get(serviceUUID)
	if ok {
		t.Fatalf("service %s should not be in lruCache", serviceUUID)
	}
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(1)
	_, ok = s.lruCache.Get(serviceUUID)
	if ok {
		t.Fatalf("service %s should not be in lruCache", serviceUUID)
	}

	// access the 3rd service member
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(2)
	key := &pb.ServiceMemberKey{
		ServiceUUID: serviceUUID,
		MemberIndex: 2,
	}
	_, err := s.GetServiceMember(ctx, key)
	if err != nil {
		t.Fatalf("getServiceMember error %s rootdir %s", err, rootdir, serviceUUID)
	}
	// create one more service member
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(maxNum)
	testServiceMemberOp(t, s, serviceUUID, maxNum, rootdir)
	// verify the 3rd service member is still in cache
	_, ok = s.lruCache.Get(serviceUUID)
	if !ok {
		t.Fatalf("service %s should be in lruCache", serviceUUID)
	}
	// verify the 4th service member is not in lruCache any more
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(3)
	_, ok = s.lruCache.Get(serviceUUID)
	if ok {
		t.Fatalf("service %s should not be in lruCache", serviceUUID)
	}

	// access the first service member again
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(0)
	key = &pb.ServiceMemberKey{
		ServiceUUID: serviceUUID,
		MemberIndex: 0,
	}
	_, err = s.GetServiceMember(ctx, key)
	if err != nil {
		t.Fatalf("getServiceMember error %s rootdir %s", err, rootdir, serviceUUID)
	}
	// verify the 1st service member in cache
	_, ok = s.lruCache.Get(serviceUUID)
	if !ok {
		t.Fatalf("service %s should be in lruCache", serviceUUID)
	}

	// delete the 1st service member
	err = s.DeleteServiceMember(ctx, key)
	if err != nil {
		t.Fatalf("deleteServiceMember error %s rootdir %s", err, rootdir, serviceUUID)
	}

	// delete the 2nd service member
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(1)
	key = &pb.ServiceMemberKey{
		ServiceUUID: serviceUUID,
		MemberIndex: 1,
	}
	err = s.DeleteServiceMember(ctx, key)
	if err != nil {
		t.Fatalf("deleteServiceMember error %s rootdir %s", err, rootdir, serviceUUID)
	}
	// getServiceMember should return error
	_, err = s.GetServiceMember(ctx, key)
	if err != db.ErrDBRecordNotFound {
		t.Fatalf("get not exist member %s, expect error db.ErrDBRecordNotFound, got %s", serviceUUID, err)
	}

	// cleanup
	os.RemoveAll(rootdir)
}
