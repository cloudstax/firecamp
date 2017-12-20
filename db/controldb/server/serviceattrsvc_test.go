package controldbserver

import (
	"encoding/json"
	"flag"
	"os"
	"path"
	"strconv"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/db/controldb"
	pb "github.com/cloudstax/firecamp/db/controldb/protocols"
	"github.com/cloudstax/firecamp/utils"
)

func TestAttrReadWriter(t *testing.T) {
	flag.Parse()
	ctx := context.Background()
	ti := time.Now().UnixNano()
	rootdir := "/tmp/fileutil" + strconv.FormatInt(ti, 10)
	serviceUUID := "serviceuuid-1"
	uuidSuffix := "-uuidSuffix"

	s, err := newAttrReadWriterIfExist(ctx, rootdir, serviceUUID)
	if err != db.ErrDBRecordNotFound {
		t.Fatalf("newAttrReadWriterIfExist expect not found error, got error %s, rootdir %s serviceUUID %s", err, rootdir, serviceUUID)
	}

	err = utils.CreateDirIfNotExist(path.Join(rootdir, serviceUUID))
	if err != nil {
		t.Fatalf("create dir %s error %s", rootdir, err)
	}

	s, err = newAttrReadWriterIfExist(ctx, rootdir, serviceUUID)
	if err != nil {
		t.Fatalf("newAttrReadWriterIfExist error %s, rootdir %s serviceUUID %s", err, rootdir, serviceUUID)
	}

	serviceStatus := "CREATING"
	taskCounts := 3
	cluster := "cluster"
	serviceName := "servicename-1"
	devName := "/dev/xvda"
	registerDNS := true
	domain := "domain"
	hostedZone := "zone1"
	requireStaticIP := false

	redisUserAttr := &common.RedisUserAttr{
		Shards:           1,
		ReplicasPerShard: 1,
	}
	b, err := json.Marshal(redisUserAttr)
	if err != nil {
		t.Fatalf("Marshal userattr error %s", err)
	}
	userAttr := &common.ServiceUserAttr{
		ServiceType: common.CatalogService_Redis,
		AttrBytes:   b,
	}
	userAttrBytes, err := json.Marshal(userAttr)
	if err != nil {
		t.Fatalf("Marshal userattr error %s", err)
	}

	attr := &pb.ServiceAttr{
		ServiceUUID:   serviceUUID,
		ServiceStatus: serviceStatus,
		Replicas:      int64(taskCounts),
		ClusterName:   cluster,
		ServiceName:   serviceName,
		Volumes: &pb.ServiceVolumes{
			PrimaryDeviceName: devName,
			PrimaryVolume: &pb.ServiceVolume{
				VolumeType:   common.VolumeTypeGPSSD,
				VolumeSizeGB: 1,
			},
			JournalDeviceName: devName + "journal",
			JournalVolume: &pb.ServiceVolume{
				VolumeType:   common.VolumeTypeIOPSSSD,
				VolumeSizeGB: 1,
			},
		},
		RegisterDNS:     registerDNS,
		DomainName:      domain,
		HostedZoneID:    hostedZone,
		RequireStaticIP: requireStaticIP,
		UserAttrBytes:   userAttrBytes,
		Res:             &pb.Resources{},
	}
	err = s.createAttr(ctx, attr)
	if err != nil {
		t.Fatalf("createAttr error %s rootdir %s", err, rootdir)
	}

	// test the request retry
	err = s.createAttr(ctx, attr)
	if err != db.ErrDBConditionalCheckFailed {
		t.Fatalf("createAttr error %s rootdir %s", err, rootdir)
	}

	// create the service attr again with different mtime
	// copy attr as s.createAttr returns the pointer, attr and attr1 point to the same pb
	attr2 := controldb.CopyServiceAttr(attr)
	attr2.LastModified = time.Now().UnixNano()
	err = s.createAttr(ctx, attr2)
	if err != db.ErrDBConditionalCheckFailed {
		t.Fatalf("createAttr again with different mtime, expect db.ErrDBConditionalCheckFailed, got error %s rootdir %s", err, rootdir)
	}

	// negative case: create the existing service attr with different fields
	attr2.ServiceName = "unknown-service"
	err = s.createAttr(ctx, attr2)
	if err != db.ErrDBConditionalCheckFailed {
		t.Fatalf("createAttr expect db.ErrDBConditionalCheckFailed, got error %s rootdir %s", err, rootdir)
	}

	// negative case: test get non-exist attr
	key := &pb.ServiceAttrKey{
		ServiceUUID: serviceUUID + uuidSuffix,
	}
	_, err = s.getAttr(ctx, key)
	if err != db.ErrDBInvalidRequest {
		t.Fatalf("getAttr error %s rootdir %s", err, rootdir)
	}

	// test get attr
	key = &pb.ServiceAttrKey{
		ServiceUUID: serviceUUID,
	}
	attr1, err := s.getAttr(ctx, key)
	if err != nil {
		t.Fatalf("getAttr error %s rootdir %s", err, rootdir)
	}
	if !controldb.EqualAttr(attr, attr1, false) {
		t.Fatalf("getAttr returns %s, expect %s rootdir %s", attr1, attr, rootdir)
	}

	// copy attr as s.getAttr returns the pointer, attr and attr1 point to the same pb
	// negative case: update attr, OldAttr mismatch
	attr2 = controldb.CopyServiceAttr(attr)
	attr2.ServiceStatus = "ACTIVE"
	req := &pb.UpdateServiceAttrRequest{
		OldAttr: attr2,
		NewAttr: attr2,
	}
	err = s.updateAttr(ctx, req)
	if err != db.ErrDBConditionalCheckFailed {
		t.Fatalf("updateAttr expect db.ErrDBConditionalCheckFailed, got error %s, rootdir %s", err, rootdir)
	}

	// test update attr
	req = &pb.UpdateServiceAttrRequest{
		OldAttr: attr,
		NewAttr: attr2,
	}
	err = s.updateAttr(ctx, req)
	if err != nil {
		t.Fatalf("updateAttr error %s rootdir %s", err, rootdir)
	}

	// verify the first and current version
	if s.store.firstVersion != 0 {
		t.Fatalf("expect first version 0, got %d", s.store.firstVersion)
	}
	if s.store.currentVersion != 1 {
		t.Fatalf("expect current version 1, got %d", s.store.currentVersion)
	}

	// test delete attr
	err = s.deleteAttr(ctx, key)
	if err != nil {
		t.Fatalf("deleteAttr error %s rootdir %s", err, rootdir)
	}

	// cleanup
	os.Remove(rootdir)
}

func testServiceAttrOp(t *testing.T, s *serviceAttrSvc, serviceUUID string, i int, rootdir string) {
	ctx := context.Background()
	serviceStatus := "CREATING"
	cluster := "cluster"
	serviceNamePrefix := "servicename-"
	devNamePrefix := "/dev/xvd"
	registerDNS := true
	domainPrefix := "domain"
	hostedZone := "zone1"
	requireStaticIP := false

	attr := &pb.ServiceAttr{
		ServiceUUID:   serviceUUID,
		ServiceStatus: serviceStatus,
		Replicas:      int64(i),
		ClusterName:   cluster,
		ServiceName:   serviceNamePrefix + strconv.Itoa(i),
		Volumes: &pb.ServiceVolumes{
			PrimaryDeviceName: devNamePrefix + strconv.Itoa(i),
			PrimaryVolume: &pb.ServiceVolume{
				VolumeType:   common.VolumeTypeGPSSD,
				VolumeSizeGB: int64(i),
			},
			JournalDeviceName: devNamePrefix + "journal" + strconv.Itoa(i),
			JournalVolume: &pb.ServiceVolume{
				VolumeType:   common.VolumeTypeGPSSD,
				VolumeSizeGB: int64(i),
			},
		},
		RegisterDNS:     registerDNS,
		DomainName:      domainPrefix + strconv.Itoa(i),
		HostedZoneID:    hostedZone,
		RequireStaticIP: requireStaticIP,
		Res:             &pb.Resources{},
	}
	err := s.CreateServiceAttr(ctx, attr)
	if err != nil {
		t.Fatalf("createAttr error %s rootdir %s", err, rootdir, serviceUUID)
	}

	// test the request retry
	err = s.CreateServiceAttr(ctx, attr)
	if err != db.ErrDBConditionalCheckFailed {
		t.Fatalf("createAttr error %s rootdir %s", err, rootdir, serviceUUID)
	}

	// negative case: create attr with different fields
	attr2 := controldb.CopyServiceAttr(attr)
	attr2.ServiceName = attr.ServiceName + "xxx"
	err = s.CreateServiceAttr(ctx, attr2)
	if err != db.ErrDBConditionalCheckFailed {
		t.Fatalf("createAttr error %s rootdir %s", err, rootdir, serviceUUID)
	}

	// test get attr
	key := &pb.ServiceAttrKey{
		ServiceUUID: serviceUUID,
	}
	attr1, err := s.GetServiceAttr(ctx, key)
	if err != nil {
		t.Fatalf("getAttr error %s rootdir %s", err, rootdir, serviceUUID)
	}
	if !controldb.EqualAttr(attr, attr1, false) {
		t.Fatalf("getAttr returns %s, expect %s rootdir %s", attr1, attr, rootdir, serviceUUID)
	}

	// negative case: get non-exist service
	key = &pb.ServiceAttrKey{
		ServiceUUID: serviceUUID + "xxxx",
	}
	_, err = s.GetServiceAttr(ctx, key)
	if err != db.ErrDBRecordNotFound {
		t.Fatalf("get non-exist attr, expect db.ErrDBRecordNotFound, got error %s rootdir %s", err, rootdir)
	}

	// test update attr
	attr2 = controldb.CopyServiceAttr(attr)
	attr2.ServiceStatus = "ACTIVE"
	// negative case: OldAttr mismatch
	req := &pb.UpdateServiceAttrRequest{
		OldAttr: attr2,
		NewAttr: attr2,
	}
	err = s.UpdateServiceAttr(ctx, req)
	if err != db.ErrDBConditionalCheckFailed {
		t.Fatalf("updateAttr error %s rootdir %s", err, rootdir, serviceUUID)
	}
	// update attr
	req = &pb.UpdateServiceAttrRequest{
		OldAttr: attr,
		NewAttr: attr2,
	}
	err = s.UpdateServiceAttr(ctx, req)
	if err != nil {
		t.Fatalf("updateAttr error %s rootdir %s", err, rootdir, serviceUUID)
	}
}

func TestServiceSvcAttr(t *testing.T) {
	flag.Parse()
	ti := time.Now().UnixNano()
	rootdir := "/tmp/fileutil" + strconv.FormatInt(ti, 10)
	// set the max cache size to test lru evict
	maxCacheSize := 5

	s := newServiceAttrSvcWithMaxCacheSize(rootdir, maxCacheSize)

	ctx := context.Background()

	serviceUUIDPrefix := "serviceuuid-"
	for i := 0; i < maxCacheSize; i++ {
		serviceUUID := serviceUUIDPrefix + strconv.Itoa(i)
		testServiceAttrOp(t, s, serviceUUID, i, rootdir)

		// service attr should be in lruCache
		cacheVal, ok := s.lruCache.Get(serviceUUID)
		if !ok {
			t.Fatalf("service %s should be in lruCache", serviceUUID)
		}
		attrio := cacheVal.(attrCacheValue).attrio
		// verify the first and current version
		if attrio.store.firstVersion != 0 {
			t.Fatalf("expect first version 0, got %d", attrio.store.firstVersion)
		}
		// create then update attr, so currentVersion should be 1
		if attrio.store.currentVersion != 1 {
			t.Fatalf("expect current version 1, got %d", attrio.store.currentVersion)
		}
	}

	// create 2 more service attr
	maxNum := maxCacheSize + 2
	for i := maxCacheSize; i < maxNum; i++ {
		serviceUUID := serviceUUIDPrefix + strconv.Itoa(i)
		testServiceAttrOp(t, s, serviceUUID, i, rootdir)
	}

	// verify the first 2 service attrs are not in lruCache any more
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

	// access the 3rd service attr
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(2)
	key := &pb.ServiceAttrKey{
		ServiceUUID: serviceUUID,
	}
	_, err := s.GetServiceAttr(ctx, key)
	if err != nil {
		t.Fatalf("getAttr error %s rootdir %s", err, rootdir, serviceUUID)
	}
	// create one more service attr
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(maxNum)
	testServiceAttrOp(t, s, serviceUUID, maxNum, rootdir)
	// verify the 3rd service attr is still in cache
	_, ok = s.lruCache.Get(serviceUUID)
	if !ok {
		t.Fatalf("service %s should be in lruCache", serviceUUID)
	}
	// verify the 4th service attr is not in lruCache any more
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(3)
	_, ok = s.lruCache.Get(serviceUUID)
	if ok {
		t.Fatalf("service %s should not be in lruCache", serviceUUID)
	}

	// access the first service attr again
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(0)
	key = &pb.ServiceAttrKey{
		ServiceUUID: serviceUUID,
	}
	_, err = s.GetServiceAttr(ctx, key)
	if err != nil {
		t.Fatalf("getAttr error %s rootdir %s", err, rootdir, serviceUUID)
	}
	// verify the 1st service attr in cache
	_, ok = s.lruCache.Get(serviceUUID)
	if !ok {
		t.Fatalf("service %s should be in lruCache", serviceUUID)
	}

	// delete the 1st service attr
	err = s.DeleteServiceAttr(ctx, key)
	if err != nil {
		t.Fatalf("deleteAttr error %s rootdir %s", err, rootdir, serviceUUID)
	}
	// verify the 1st service attr not in cache
	_, ok = s.lruCache.Get(serviceUUID)
	if ok {
		t.Fatalf("service %s should not be in lruCache", serviceUUID)
	}

	// delete the 2nd service attr
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(1)
	key = &pb.ServiceAttrKey{
		ServiceUUID: serviceUUID,
	}
	err = s.DeleteServiceAttr(ctx, key)
	if err != nil {
		t.Fatalf("deleteAttr error %s rootdir %s", err, rootdir, serviceUUID)
	}
	// verify the service attr not in cache
	_, ok = s.lruCache.Get(serviceUUID)
	if ok {
		t.Fatalf("service %s should not be in lruCache", serviceUUID)
	}
	// getAttr should return error
	_, err = s.GetServiceAttr(ctx, key)
	if err != db.ErrDBRecordNotFound {
		t.Fatalf("get not exist service attr %s, expect error db.ErrDBRecordNotFound, got %s", serviceUUID, err)
	}

	// cleanup
	os.RemoveAll(rootdir)
}
