package controldbserver

import (
	"flag"
	"os"
	"strconv"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/db"
	"github.com/cloudstax/openmanage/db/controldb"
	pb "github.com/cloudstax/openmanage/db/controldb/protocols"
)

func TestVolumeReadWriter(t *testing.T) {
	flag.Parse()
	ctx := context.Background()
	ti := time.Now().UnixNano()
	rootdir := "/tmp/fileutil" + strconv.FormatInt(ti, 10)
	serviceUUID := "serviceuuid-1"

	s, err := newVolumeReadWriter(ctx, rootdir, serviceUUID)
	if err != nil {
		t.Fatalf("newVolumeReadWriter error %s, rootdir %s serviceUUID %s", err, rootdir, serviceUUID)
	}

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

	maxNum := 17
	for i := 0; i < maxNum; i++ {
		str := strconv.Itoa(i)
		volID := volIDPrefix + str
		devName := devNamePrefix + str
		taskID := taskIDPrefix + str
		contInsID := contInsIDPrefix + str
		serverInsID := serverInsIDPrefix + str
		memberName := memberNamePrefix + str
		cfg := &pb.MemberConfig{FileID: cfgIDPrefix + str, FileName: cfgNamePrefix + str, FileMD5: cfgMD5Prefix + str}
		cfgs := []*pb.MemberConfig{cfg}

		vol := &pb.Volume{
			ServiceUUID:         serviceUUID,
			VolumeID:            volID,
			DeviceName:          devName,
			AvailableZone:       az,
			TaskID:              taskID,
			ContainerInstanceID: contInsID,
			ServerInstanceID:    serverInsID,
			MemberName:          memberName,
			Configs:             cfgs,
		}
		err = s.createVolume(ctx, vol)
		if err != nil {
			t.Fatalf("createVolume error %s rootdir %s", err, rootdir)
		}

		// test the request retry
		err = s.createVolume(ctx, vol)
		if err != nil {
			t.Fatalf("createVolume error %s rootdir %s", err, rootdir)
		}

		// create the volume again with different mtime
		// copy volume as s.createVolume returns the pointer, vol and vol1 point to the same pb.Volume
		vol1 := controldb.CopyVolume(vol)
		vol1.LastModified = time.Now().UnixNano()
		err = s.createVolume(ctx, vol1)
		if err != nil {
			t.Fatalf("create the existing volume with different mtime, expect success, got %s, rootdir %s", err, rootdir)
		}

		// negative case: create the existing volume with different fields
		vol1.ContainerInstanceID = "ContainerInstanceID-xxx"
		err = s.createVolume(ctx, vol1)
		if err != db.ErrDBConditionalCheckFailed {
			t.Fatalf("create the existing volume, expect error db.ErrDBConditionalCheckFailed, got %s, rootdir %s", err, rootdir)
		}

		// negative: get non-exist vol
		key := &pb.VolumeKey{
			ServiceUUID: serviceUUID,
			VolumeID:    volID + "xxx",
		}
		vol1, err = s.getVolume(ctx, key)
		if err != db.ErrDBRecordNotFound {
			t.Fatalf("get non-exist volume, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
		}
		// get vol
		key = &pb.VolumeKey{
			ServiceUUID: serviceUUID,
			VolumeID:    volID,
		}
		vol1, err = s.getVolume(ctx, key)
		if err != nil {
			t.Fatalf("getVolume error %s key %s rootdir %s", err, key, rootdir)
		}
		if !controldb.EqualVolume(vol, vol1, false) {
			t.Fatalf("getVolume returns %s, expect %s rootdir %s", vol1, vol, rootdir)
		}

		// test update vol
		// copy volume as s.getVolume returns the pointer, vol and vol1 point to the same pb.Volume
		vol2 := controldb.CopyVolume(vol1)
		vol2.VolumeID = volID + "xxxx"
		// negative case: OldVol non-exist volume
		req := &pb.UpdateVolumeRequest{
			OldVol: vol2,
			NewVol: vol2,
		}
		err = s.updateVolume(ctx, req)
		if err != db.ErrDBRecordNotFound {
			t.Fatalf("update non-exist volume, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
		}
		// negative case: OldVol mismatch
		vol2.VolumeID = vol.VolumeID
		vol2.ContainerInstanceID = vol.ContainerInstanceID + updateSuffix
		vol2.ServerInstanceID = vol.ServerInstanceID + updateSuffix
		vol2.TaskID = vol.TaskID + updateSuffix
		req = &pb.UpdateVolumeRequest{
			OldVol: vol2,
			NewVol: vol2,
		}
		err = s.updateVolume(ctx, req)
		if err != db.ErrDBConditionalCheckFailed {
			t.Fatalf("updateVolume expect db.ErrDBConditionalCheckFailed, got %s, rootdir %s", err, rootdir)
		}
		// update vol
		req = &pb.UpdateVolumeRequest{
			OldVol: vol,
			NewVol: vol2,
		}
		err = s.updateVolume(ctx, req)
		if err != nil {
			t.Fatalf("updateVolume error %s rootdir %s", err, rootdir)
		}

		// test vol list
		lreq := &pb.ListVolumeRequest{
			ServiceUUID: serviceUUID,
		}
		vols, err := s.listVolumes(lreq)
		if err != nil {
			t.Fatalf("listVolumes error %s, rootdir %s serviceUUID %s", err, rootdir, serviceUUID)
		}
		if len(vols) != i+1 {
			t.Fatalf("listVolumes expect %d volumes, got %d, rootdir %s, serviceUUID %s",
				i+1, len(vols), rootdir, serviceUUID)
		}
	}

	// delete 6 volumes
	idx := 0
	for i := 0; i < 6; i++ {
		idx += 2
		str := strconv.Itoa(idx)
		volID := volIDPrefix + str

		// negative case: delete non-exist volume
		key := &pb.VolumeKey{
			ServiceUUID: serviceUUID,
			VolumeID:    volID + "xxx",
		}
		err = s.deleteVolume(ctx, key)
		if err != db.ErrDBRecordNotFound {
			t.Fatalf("delete non-exist volume, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
		}
		// delete volume
		key = &pb.VolumeKey{
			ServiceUUID: serviceUUID,
			VolumeID:    volID,
		}
		err = s.deleteVolume(ctx, key)
		if err != nil {
			t.Fatalf("deleteVolume error %s rootdir %s", err, rootdir)
		}

		// test vol list
		lreq := &pb.ListVolumeRequest{
			ServiceUUID: serviceUUID,
		}
		vols, err := s.listVolumes(lreq)
		if err != nil {
			t.Fatalf("listVolumes error %s, rootdir %s serviceUUID %s", err, rootdir, serviceUUID)
		}
		if len(vols) != maxNum-i-1 {
			t.Fatalf("listVolumes expect %d volumes, got %d, rootdir %s, serviceUUID %s",
				maxNum-i-1, len(vols), rootdir, serviceUUID)
		}
	}

	// cleanup
	os.Remove(rootdir)
}

func testVolumeOp(t *testing.T, s *volumeSvc, serviceUUID string, i int, rootdir string) {
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

	ctx := context.Background()
	str := strconv.Itoa(i)
	volID := volIDPrefix + str
	devName := devNamePrefix + str
	taskID := taskIDPrefix + str
	contInsID := contInsIDPrefix + str
	serverInsID := serverInsIDPrefix + str
	memberName := memberNamePrefix + str
	cfg := &pb.MemberConfig{FileID: cfgIDPrefix + str, FileName: cfgNamePrefix + str, FileMD5: cfgMD5Prefix + str}
	cfgs := []*pb.MemberConfig{cfg}

	vol := &pb.Volume{
		ServiceUUID:         serviceUUID,
		VolumeID:            volID,
		DeviceName:          devName,
		AvailableZone:       az,
		TaskID:              taskID,
		ContainerInstanceID: contInsID,
		ServerInstanceID:    serverInsID,
		MemberName:          memberName,
		Configs:             cfgs,
	}

	err := s.CreateVolume(ctx, vol)
	if err != nil {
		t.Fatalf("createVolume error %s rootdir %s", err, rootdir)
	}

	// test the request retry
	err = s.CreateVolume(ctx, vol)
	if err != nil {
		t.Fatalf("createVolume error %s rootdir %s", err, rootdir)
	}

	// negative case: create the existing volume with different fields
	// copy volume as s.createVolume returns the pointer, vol and vol1 point to the same pb.Volume
	vol1 := controldb.CopyVolume(vol)
	vol1.ContainerInstanceID = "ContainerInstanceID-a"
	err = s.CreateVolume(ctx, vol1)
	if err != db.ErrDBConditionalCheckFailed {
		t.Fatalf("create the existing volume, expect error db.ErrDBConditionalCheckFailed, got %s, rootdir %s", err, rootdir)
	}

	// negative: get non-exist vol
	key := &pb.VolumeKey{
		ServiceUUID: serviceUUID,
		VolumeID:    volID + "xxx",
	}
	_, err = s.GetVolume(ctx, key)
	if err != db.ErrDBRecordNotFound {
		t.Fatalf("get non-exist volume, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
	}
	// get vol
	key = &pb.VolumeKey{
		ServiceUUID: serviceUUID,
		VolumeID:    volID,
	}
	vol1, err = s.GetVolume(ctx, key)
	if err != nil {
		t.Fatalf("getVolume error %s key %s rootdir %s", err, key, rootdir)
	}
	if !controldb.EqualVolume(vol, vol1, false) {
		t.Fatalf("getVolume returns %s, expect %s rootdir %s", vol1, vol, rootdir)
	}

	// test update vol
	// copy volume as s.getVolume returns the pointer, vol and vol1 point to the same pb.Volume
	vol2 := controldb.CopyVolume(vol1)
	vol2.VolumeID = volID + "xxxx"
	// negative case: OldVol non-exist volume
	req := &pb.UpdateVolumeRequest{
		OldVol: vol2,
		NewVol: vol2,
	}
	err = s.UpdateVolume(ctx, req)
	if err != db.ErrDBRecordNotFound {
		t.Fatalf("update non-exist volume, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
	}
	// negative case: OldVol mismatch
	vol2.VolumeID = vol.VolumeID
	vol2.ContainerInstanceID = vol.ContainerInstanceID + updateSuffix
	vol2.ServerInstanceID = vol.ServerInstanceID + updateSuffix
	vol2.TaskID = vol.TaskID + updateSuffix
	req = &pb.UpdateVolumeRequest{
		OldVol: vol2,
		NewVol: vol2,
	}
	err = s.UpdateVolume(ctx, req)
	if err != db.ErrDBConditionalCheckFailed {
		t.Fatalf("updateVolume expect db.ErrDBConditionalCheckFailed, got %s, rootdir %s", err, rootdir)
	}
	// update vol
	req = &pb.UpdateVolumeRequest{
		OldVol: vol,
		NewVol: vol2,
	}
	err = s.UpdateVolume(ctx, req)
	if err != nil {
		t.Fatalf("updateVolume error %s rootdir %s", err, rootdir)
	}

	// test vol list
	lreq := &pb.ListVolumeRequest{
		ServiceUUID: serviceUUID,
	}
	vols, err := s.listVolumes(ctx, lreq)
	if err != nil {
		t.Fatalf("listVolumes error %s, rootdir %s serviceUUID %s", err, rootdir, serviceUUID)
	}
	if len(vols) != 1 {
		t.Fatalf("listVolumes expect %d volumes, got %d, rootdir %s, serviceUUID %s",
			1, len(vols), rootdir, serviceUUID)
	}

}

func TestVolumeSvc(t *testing.T) {
	flag.Parse()
	time.Sleep(2 * time.Second)
	ti := time.Now().UnixNano()
	rootdir := "/tmp/fileutil" + strconv.FormatInt(ti, 10)
	// set the max cache size to test lru evict
	maxCacheSize := 5

	s := newVolumeSvcWithMaxCacheSize(rootdir, maxCacheSize)

	ctx := context.Background()

	serviceUUIDPrefix := "serviceuuid-"
	volIDPrefix := "volid-"
	for i := 0; i < maxCacheSize; i++ {
		serviceUUID := serviceUUIDPrefix + strconv.Itoa(i)
		testVolumeOp(t, s, serviceUUID, i, rootdir)

		// vol should be in lruCache
		_, ok := s.lruCache.Get(serviceUUID)
		if !ok {
			t.Fatalf("service %s should be in lruCache", serviceUUID)
		}
	}

	// create 2 more vol
	maxNum := maxCacheSize + 2
	for i := maxCacheSize; i < maxNum; i++ {
		serviceUUID := serviceUUIDPrefix + strconv.Itoa(i)
		testVolumeOp(t, s, serviceUUID, i, rootdir)
	}

	// verify the first 2 service vols are not in lruCache any more
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

	// access the 3rd service vol
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(2)
	key := &pb.VolumeKey{
		ServiceUUID: serviceUUID,
		VolumeID:    volIDPrefix + strconv.Itoa(2),
	}
	_, err := s.GetVolume(ctx, key)
	if err != nil {
		t.Fatalf("getVolume error %s rootdir %s", err, rootdir, serviceUUID)
	}
	// create one more service vol
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(maxNum)
	testVolumeOp(t, s, serviceUUID, maxNum, rootdir)
	// verify the 3rd service vol is still in cache
	_, ok = s.lruCache.Get(serviceUUID)
	if !ok {
		t.Fatalf("service %s should be in lruCache", serviceUUID)
	}
	// verify the 4th service vol is not in lruCache any more
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(3)
	_, ok = s.lruCache.Get(serviceUUID)
	if ok {
		t.Fatalf("service %s should not be in lruCache", serviceUUID)
	}

	// access the first service vol again
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(0)
	key = &pb.VolumeKey{
		ServiceUUID: serviceUUID,
		VolumeID:    volIDPrefix + strconv.Itoa(0),
	}
	_, err = s.GetVolume(ctx, key)
	if err != nil {
		t.Fatalf("getVolume error %s rootdir %s", err, rootdir, serviceUUID)
	}
	// verify the 1st service vol in cache
	_, ok = s.lruCache.Get(serviceUUID)
	if !ok {
		t.Fatalf("service %s should be in lruCache", serviceUUID)
	}

	// delete the 1st service vol
	err = s.DeleteVolume(ctx, key)
	if err != nil {
		t.Fatalf("deleteVolume error %s rootdir %s", err, rootdir, serviceUUID)
	}

	// delete the 2nd service vol
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(1)
	key = &pb.VolumeKey{
		ServiceUUID: serviceUUID,
		VolumeID:    volIDPrefix + strconv.Itoa(1),
	}
	err = s.DeleteVolume(ctx, key)
	if err != nil {
		t.Fatalf("deleteVolume error %s rootdir %s", err, rootdir, serviceUUID)
	}
	// getVolume should return error
	_, err = s.GetVolume(ctx, key)
	if err != db.ErrDBRecordNotFound {
		t.Fatalf("get not exist vol %s, expect error db.ErrDBRecordNotFound, got %s", serviceUUID, err)
	}

	// cleanup
	os.RemoveAll(rootdir)
}
