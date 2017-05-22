package controldbserver

import (
	"flag"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/openconnectio/openmanage/db"
	"github.com/openconnectio/openmanage/db/controldb"
	pb "github.com/openconnectio/openmanage/db/controldb/protocols"
	"github.com/openconnectio/openmanage/utils"
	"golang.org/x/net/context"
)

func TestDeviceSvc(t *testing.T) {
	flag.Parse()

	ti := time.Now().UnixNano()
	rootdir := "/tmp/fileutil" + strconv.FormatInt(ti, 10)
	err := utils.CreateDirIfNotExist(rootdir)
	if err != nil {
		t.Fatalf("create dir %s error %s", rootdir, err)
	}

	cluster := "cluster"
	s, err := newDeviceSvc(rootdir, cluster)
	if err != nil {
		t.Fatalf("newDeviceSvc error %s, rootdir %s cluster %s", err, rootdir, cluster)
	}

	devprefix := "/dev/xvd"
	svcprefix := "service-"
	ctx := context.Background()
	num := maxVersions + 3
	for i := 0; i < num; i++ {
		dev := &pb.Device{
			ClusterName: cluster,
			DeviceName:  devprefix + strconv.Itoa(i),
			ServiceName: svcprefix + strconv.Itoa(i),
		}
		err = s.CreateDevice(ctx, dev)
		if err != nil {
			t.Fatalf("CreateDevice error %s %d", err, i)
		}

		// test the request retry
		err = s.CreateDevice(ctx, dev)
		if err != nil {
			t.Fatalf("CreateDevice error %s %d", err, i)
		}

		// negative case: create device with different field
		dev1 := controldb.CopyDevice(dev)
		dev1.ServiceName = dev.ServiceName + "xxx"
		err = s.CreateDevice(ctx, dev1)
		if err != db.ErrDBConditionalCheckFailed {
			t.Fatalf("CreateDevice error %s %d", err, i)
		}
	}

	// test GetDevice
	for i := 0; i < num; i++ {
		key := &pb.DeviceKey{
			ClusterName: cluster,
			DeviceName:  devprefix + strconv.Itoa(i),
		}
		dev := &pb.Device{
			ClusterName: cluster,
			DeviceName:  devprefix + strconv.Itoa(i),
			ServiceName: svcprefix + strconv.Itoa(i),
		}
		dev1, err := s.GetDevice(ctx, key)
		if err != nil {
			t.Fatalf("GetDevice error %s %d", err, i)
		}
		if !controldb.EqualDevice(dev, dev1) {
			t.Fatalf("GetDevice returns %s, expect %s", dev1, dev)
		}

		// negative casse: get non-exist device
		key.ClusterName = cluster + "xxx"
		_, err = s.GetDevice(ctx, key)
		if err != db.ErrDBInvalidRequest {
			t.Fatalf("GetDevice expect db.ErrDBInvalidRequest, got error %s %d", err, i)
		}
		key.ClusterName = cluster
		key.DeviceName = dev.DeviceName + "xxx"
		_, err = s.GetDevice(ctx, key)
		if err != db.ErrDBRecordNotFound {
			t.Fatalf("GetDevice expect db.ErrDBRecordNotFound, got error %s %d", err, i)
		}
	}

	// test list devices
	devs := s.listDevices()
	if len(devs) != num {
		t.Fatalf("list %d devices, expect %d", len(devs), num)
	}
	for i, dev := range devs {
		dev1 := &pb.Device{
			ClusterName: cluster,
			DeviceName:  devprefix + strconv.Itoa(i),
			ServiceName: svcprefix + strconv.Itoa(i),
		}
		if !controldb.EqualDevice(dev, dev1) {
			t.Fatalf("expect device %s got %s index %d", dev1, dev, i)
		}
	}

	// test delete device
	delNum := 4
	for i := 0; i < delNum; i++ {
		key := &pb.DeviceKey{
			ClusterName: cluster,
			DeviceName:  devprefix + strconv.Itoa(i),
		}
		err := s.DeleteDevice(ctx, key)
		if err != nil {
			t.Fatalf("DeleteDevice error %s %d", err, i)
		}
	}

	// test list devices again
	devs = s.listDevices()
	if len(devs) != num-delNum {
		t.Fatalf("list %d devices, expect %d", len(devs), num-delNum)
	}
	for i, dev := range devs {
		dev1 := &pb.Device{
			ClusterName: cluster,
			DeviceName:  devprefix + strconv.Itoa(i+delNum),
			ServiceName: svcprefix + strconv.Itoa(i+delNum),
		}
		if !controldb.EqualDevice(dev, dev1) {
			t.Fatalf("expect device %s got %s index %d", dev1, dev, i)
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
	s, err = newDeviceSvc(rootdir, cluster)
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
