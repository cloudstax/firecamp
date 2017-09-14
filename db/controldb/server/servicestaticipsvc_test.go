package controldbserver

import (
	"flag"
	"os"
	"strconv"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/db/controldb"
	pb "github.com/cloudstax/firecamp/db/controldb/protocols"
	"github.com/cloudstax/firecamp/utils"
)

func TestServiceStaticIPReadWriter(t *testing.T) {
	flag.Parse()
	ctx := context.Background()
	ti := time.Now().UnixNano()
	rootdir := "/tmp/fileutil" + strconv.FormatInt(ti, 10)
	err := utils.CreateDirIfNotExist(rootdir)
	if err != nil {
		t.Fatalf("create root dir error", err, rootdir)
	}

	s := newServiceStaticIPReadWriter(0, rootdir)

	ipPrefix := "ip-"
	serviceUUIDPrefix := "uuid-"
	az := "az-1"
	serverInsIDPrefix := "serverInstanceID-"
	netInterfacePrefix := "netinterface-"
	updateSuffix := "-update"

	maxNum := 7
	for i := 0; i < maxNum; i++ {
		str := strconv.Itoa(i)
		staticIP := ipPrefix + str
		serviceUUID := serviceUUIDPrefix + str
		serverInsID := serverInsIDPrefix + str
		netInterfaceID := netInterfacePrefix + str

		serviceip := &pb.ServiceStaticIP{
			StaticIP:           staticIP,
			ServiceUUID:        serviceUUID,
			AvailableZone:      az,
			ServerInstanceID:   serverInsID,
			NetworkInterfaceID: netInterfaceID,
		}
		err := s.createServiceStaticIP(ctx, serviceip)
		if err != nil {
			t.Fatalf("createServiceStaticIP error %s rootdir %s", err, rootdir)
		}

		// test the request retry
		err = s.createServiceStaticIP(ctx, serviceip)
		if err != nil {
			t.Fatalf("createServiceStaticIP error %s rootdir %s", err, rootdir)
		}

		// negative case: create the existing serviceStaticIP with different fields
		// copy serviceStaticIP as s.createServiceStaticIP returns the pointer, serviceip and serviceip1 point to the same pb.ServiceStaticIP
		serviceip1 := controldb.CopyServiceStaticIP(serviceip)
		serviceip1.ServerInstanceID = "server-xxxx"
		err = s.createServiceStaticIP(ctx, serviceip1)
		if err != db.ErrDBConditionalCheckFailed {
			t.Fatalf("create the existing serviceStaticIP, expect error db.ErrDBConditionalCheckFailed, got %s, rootdir %s", err, rootdir)
		}

		// negative: get non-exist serviceip
		key := &pb.ServiceStaticIPKey{
			StaticIP: "ip-xxxx",
		}
		serviceip1, err = s.getServiceStaticIP(ctx, key)
		if err != db.ErrDBRecordNotFound {
			t.Fatalf("get non-exist serviceStaticIP, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
		}
		// get serviceip
		key = &pb.ServiceStaticIPKey{
			StaticIP: staticIP,
		}
		serviceip1, err = s.getServiceStaticIP(ctx, key)
		if err != nil {
			t.Fatalf("getServiceStaticIP error %s key %s rootdir %s", err, key, rootdir)
		}
		if !controldb.EqualServiceStaticIP(serviceip, serviceip1) {
			t.Fatalf("getServiceStaticIP returns %s, expect %s rootdir %s", serviceip1, serviceip, rootdir)
		}

		// test update serviceip
		// copy serviceStaticIP as s.getServiceStaticIP returns the pointer, serviceip and serviceip1 point to the same pb.ServiceStaticIP
		serviceip2 := controldb.CopyServiceStaticIP(serviceip1)
		serviceip2.StaticIP = "ip-xx"
		// negative case: OldIP non-exist serviceStaticIP
		err = s.updateServiceStaticIP(ctx, serviceip2, serviceip2)
		if err != db.ErrDBRecordNotFound {
			t.Fatalf("update non-exist serviceStaticIP, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
		}

		// negative case: OldIP mismatch
		serviceip2.StaticIP = serviceip.StaticIP
		serviceip2.ServerInstanceID = serviceip.ServerInstanceID + updateSuffix
		serviceip2.NetworkInterfaceID = serviceip.NetworkInterfaceID + updateSuffix
		err = s.updateServiceStaticIP(ctx, serviceip2, serviceip2)
		if err != db.ErrDBConditionalCheckFailed {
			t.Fatalf("updateServiceStaticIP expect db.ErrDBConditionalCheckFailed, got %s, rootdir %s", err, rootdir)
		}

		// update serviceip
		err = s.updateServiceStaticIP(ctx, serviceip, serviceip2)
		if err != nil {
			t.Fatalf("updateServiceStaticIP error %s rootdir %s", err, rootdir)
		}
	}

	// delete serviceStaticIPs
	for i := 0; i < maxNum-2; i++ {
		str := strconv.Itoa(i)
		staticIP := ipPrefix + str

		// negative case: delete non-exist serviceStaticIP
		key := &pb.ServiceStaticIPKey{
			StaticIP: staticIP + "xxx",
		}
		err := s.deleteServiceStaticIP(ctx, key)
		if err != db.ErrDBRecordNotFound {
			t.Fatalf("delete non-exist serviceStaticIP, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
		}

		// delete serviceStaticIP
		key = &pb.ServiceStaticIPKey{
			StaticIP: staticIP,
		}
		err = s.deleteServiceStaticIP(ctx, key)
		if err != nil {
			t.Fatalf("deleteServiceStaticIP error %s rootdir %s", err, rootdir)
		}
	}

	// cleanup
	os.Remove(rootdir)
}

func TestServiceStaticIPSvc(t *testing.T) {
	flag.Parse()
	rootdir := "/tmp/fileutil" + utils.GenUUID()
	err := utils.CreateDirIfNotExist(rootdir)
	if err != nil {
		t.Fatalf("create root dir error", err, rootdir)
	}

	s := newServiceStaticIPSvc(rootdir)

	ctx := context.Background()

	ipPrefix := "ip-"
	serviceUUIDPrefix := "uuid-"
	az := "az-1"
	serverInsIDPrefix := "serverInstanceID-"
	netInterfacePrefix := "netinterface-"
	updateSuffix := "-update"

	for i := 0; i < 3; i++ {
		str := strconv.Itoa(i)
		staticIP := ipPrefix + str
		serviceUUID := serviceUUIDPrefix + str
		serverInsID := serverInsIDPrefix + str
		netInterfaceID := netInterfacePrefix + str

		serviceip := &pb.ServiceStaticIP{
			StaticIP:           staticIP,
			ServiceUUID:        serviceUUID,
			AvailableZone:      az,
			ServerInstanceID:   serverInsID,
			NetworkInterfaceID: netInterfaceID,
		}

		err := s.CreateServiceStaticIP(ctx, serviceip)
		if err != nil {
			t.Fatalf("createServiceStaticIP error %s rootdir %s", err, rootdir)
		}

		// test the request retry
		err = s.CreateServiceStaticIP(ctx, serviceip)
		if err != nil {
			t.Fatalf("createServiceStaticIP error %s rootdir %s", err, rootdir)
		}

		// negative case: create the existing serviceStaticIP with different fields
		// copy serviceStaticIP as s.createServiceStaticIP returns the pointer, serviceip and serviceip1 point to the same pb.ServiceStaticIP
		serviceip1 := controldb.CopyServiceStaticIP(serviceip)
		serviceip1.ServerInstanceID = "server-xxx"
		err = s.CreateServiceStaticIP(ctx, serviceip1)
		if err != db.ErrDBConditionalCheckFailed {
			t.Fatalf("create the existing serviceStaticIP, expect error db.ErrDBConditionalCheckFailed, got %s, rootdir %s", err, rootdir)
		}

		// negative: get non-exist serviceip
		key := &pb.ServiceStaticIPKey{
			StaticIP: staticIP + "-xxx",
		}
		_, err = s.GetServiceStaticIP(ctx, key)
		if err != db.ErrDBRecordNotFound {
			t.Fatalf("get non-exist serviceStaticIP, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
		}
		// get serviceip
		key = &pb.ServiceStaticIPKey{
			StaticIP: staticIP,
		}
		serviceip1, err = s.GetServiceStaticIP(ctx, key)
		if err != nil {
			t.Fatalf("getServiceStaticIP error %s key %s rootdir %s", err, key, rootdir)
		}
		if !controldb.EqualServiceStaticIP(serviceip, serviceip1) {
			t.Fatalf("getServiceStaticIP returns %s, expect %s rootdir %s", serviceip1, serviceip, rootdir)
		}

		// test update serviceip
		// copy serviceStaticIP as s.getServiceStaticIP returns the pointer, serviceip and serviceip1 point to the same pb.ServiceStaticIP
		serviceip2 := controldb.CopyServiceStaticIP(serviceip1)
		serviceip2.StaticIP = serviceip.StaticIP + "xxxx"
		// negative case: OldIP non-exist serviceStaticIP
		req := &pb.UpdateServiceStaticIPRequest{
			OldIP: serviceip2,
			NewIP: serviceip2,
		}
		err = s.UpdateServiceStaticIP(ctx, req)
		if err != db.ErrDBRecordNotFound {
			t.Fatalf("update non-exist serviceStaticIP, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
		}

		// negative case: OldIP mismatch
		serviceip2.StaticIP = serviceip.StaticIP
		serviceip2.ServerInstanceID = serviceip.ServerInstanceID + updateSuffix
		serviceip2.NetworkInterfaceID = serviceip.NetworkInterfaceID + updateSuffix
		req = &pb.UpdateServiceStaticIPRequest{
			OldIP: serviceip2,
			NewIP: serviceip2,
		}
		err = s.UpdateServiceStaticIP(ctx, req)
		if err != db.ErrDBConditionalCheckFailed {
			t.Fatalf("updateServiceStaticIP expect db.ErrDBConditionalCheckFailed, got %s, rootdir %s", err, rootdir)
		}

		// update serviceip
		req = &pb.UpdateServiceStaticIPRequest{
			OldIP: serviceip,
			NewIP: serviceip2,
		}
		err = s.UpdateServiceStaticIP(ctx, req)
		if err != nil {
			t.Fatalf("updateServiceStaticIP error %s rootdir %s", err, rootdir)
		}
	}

	// cleanup
	os.RemoveAll(rootdir)
}
