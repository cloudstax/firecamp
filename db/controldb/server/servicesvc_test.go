package controldbserver

import (
	"flag"
	"os"
	"strconv"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/db/controldb"
	pb "github.com/cloudstax/openmanage/db/controldb/protocols"
	"github.com/cloudstax/openmanage/utils"
)

func TestServiceSvc(t *testing.T) {
	flag.Parse()

	ti := time.Now().UnixNano()
	rootdir := "/tmp/fileutil" + strconv.FormatInt(ti, 10)
	err := utils.CreateDirIfNotExist(rootdir)
	if err != nil {
		t.Fatalf("create dir %s error %s", rootdir, err)
	}

	cluster := "cluster"
	s, err := newServiceSvc(rootdir, cluster)
	if err != nil {
		t.Fatalf("newServiceSvc error %s, rootdir %s cluster %s", err, rootdir, cluster)
	}

	svcprefix := "service-"
	uuidprefix := "uuid-"
	ctx := context.Background()
	num := maxVersions + 5
	for i := 0; i < num; i++ {
		svc := &pb.Service{
			ClusterName: cluster,
			ServiceName: svcprefix + strconv.Itoa(i),
			ServiceUUID: uuidprefix + strconv.Itoa(i),
		}
		err = s.CreateService(ctx, svc)
		if err != nil {
			t.Fatalf("CreateService error %s %d", err, i)
		}

		// test the request retry
		err = s.CreateService(ctx, svc)
		if err != nil {
			t.Fatalf("CreateService error %s %d", err, i)
		}
	}

	// test GetService
	for i := 0; i < num; i++ {
		key := &pb.ServiceKey{
			ClusterName: cluster,
			ServiceName: svcprefix + strconv.Itoa(i),
		}
		svc := &pb.Service{
			ClusterName: cluster,
			ServiceName: svcprefix + strconv.Itoa(i),
			ServiceUUID: uuidprefix + strconv.Itoa(i),
		}
		svc1, err := s.GetService(ctx, key)
		if err != nil {
			t.Fatalf("GetService error %s %d", err, i)
		}
		if !controldb.EqualService(svc, svc1) {
			t.Fatalf("GetService returns %s, expect %s", svc1, svc)
		}
	}

	// test list svcices
	svcs := s.listServices()
	if len(svcs) != num {
		t.Fatalf("list %d svcices, expect %d", len(svcs), num)
	}
	for i, svc := range svcs {
		svc1 := &pb.Service{
			ClusterName: cluster,
			ServiceName: svcprefix + strconv.Itoa(i),
			ServiceUUID: uuidprefix + strconv.Itoa(i),
		}
		if !controldb.EqualService(svc, svc1) {
			t.Fatalf("expect svcice %s got %s index %d", svc1, svc, i)
		}
	}

	// test delete svcice
	delNum := 3
	for i := 0; i < delNum; i++ {
		key := &pb.ServiceKey{
			ClusterName: cluster,
			ServiceName: svcprefix + strconv.Itoa(i),
		}
		err := s.DeleteService(ctx, key)
		if err != nil {
			t.Fatalf("DeleteService error %s %d", err, i)
		}
	}

	// test list svcices again
	svcs = s.listServices()
	if len(svcs) != num-delNum {
		t.Fatalf("list %d svcices, expect %d", len(svcs), num-delNum)
	}
	for i, svc := range svcs {
		svc1 := &pb.Service{
			ClusterName: cluster,
			ServiceName: svcprefix + strconv.Itoa(i+delNum),
			ServiceUUID: uuidprefix + strconv.Itoa(i+delNum),
		}
		if !controldb.EqualService(svc, svc1) {
			t.Fatalf("expect svcice %s got %s index %d", svc1, svc, i)
		}
	}

	// wait till the background cleanup job is done
	i := 0
	for s.store.cleanupRunning {
		time.Sleep(1 * time.Second)
		i++
		if i > 2 {
			t.Fatalf("cleanup job is still running, %d %d", s.store.firstVersion, s.store.currentVersion)
		}
	}

	// create a new version again to run the last cleanup
	s, err = newServiceSvc(rootdir, cluster)
	if err != nil {
		t.Fatalf("newServiceSvc error %s, rootdir %s cluster %s", err, rootdir, cluster)
	}

	// wait till the background cleanup job is done
	i = 0
	for s.store.cleanupRunning {
		time.Sleep(1 * time.Second)
		i++
		if i > 2 {
			t.Fatalf("cleanup job is still running, %d %d", s.store.firstVersion, s.store.currentVersion)
		}
	}

	// verify the first and current version
	if s.store.firstVersion != int64(num+delNum-maxVersions-1) {
		t.Fatalf("expect first version %d, got %d", num+delNum-maxVersions-1, s.store.firstVersion)
	}
	if s.store.currentVersion != int64(num+delNum-1) {
		t.Fatalf("expect current version %d, got %d", num+delNum-1, s.store.currentVersion)
	}

	// cleanup
	s.store.DeleteAll()
	os.Remove(rootdir)
}
