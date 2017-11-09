package server

import (
	"testing"

	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
)

func TestMemServer(t *testing.T) {
	s := NewMemServer()
	info := NewMockServerInfo()

	ctx := context.Background()
	volSizeGB := int64(1)

	opts := &CreateVolumeOptions{
		AvailabilityZone: info.GetLocalAvailabilityZone(),
		VolumeType:       VolumeTypeGPSSD,
		VolumeSizeGB:     volSizeGB,
	}

	volID1, err := s.CreateVolume(ctx, opts)
	if err != nil {
		t.Fatalf("CreateVolume error %s", err)
	}
	volID2, err := s.CreateVolume(ctx, opts)
	if err != nil {
		t.Fatalf("CreateVolume error %s", err)
	}

	// negative case: delete unexist volume
	err = s.DeleteVolume(ctx, "vol-xxx")
	if err != common.ErrNotFound {
		t.Fatalf("delete unexist volume, expect ErrNotFound, get %s", err)
	}

	err = s.DeleteVolume(ctx, volID1)
	if err != nil {
		t.Fatalf("DeleteVolume %s error %s", volID1, err)
	}
	err = s.DeleteVolume(ctx, volID2)
	if err != nil {
		t.Fatalf("DeleteVolume %s error %s", volID2, err)
	}

	netInterfaces, cidrBlock, err := s.GetNetworkInterfaces(ctx, "cluster", "vpc", info.GetLocalAvailabilityZone())
	if err != nil {
		t.Fatalf("GetNetworkInterfaces error %s", err)
	}
	if len(netInterfaces) != 1 {
		t.Fatalf("expect 1 network interface, get %d", len(netInterfaces))
	}
	if cidrBlock != defaultCidrBlock {
		t.Fatalf("expect cidrBlock %s, get %s", defaultCidrBlock, cidrBlock)
	}
	if len(netInterfaces[0].PrivateIPs) != 0 {
		t.Fatalf("expect 0 private ips, get %s", netInterfaces[0].PrivateIPs)
	}

	// unassign unexist ip
	staticIP1 := "172.31.64.2"
	err = s.UnassignStaticIP(ctx, netInterfaces[0].InterfaceID, staticIP1)
	if err != nil {
		t.Fatalf("unassign ip should return success for unexist ip, get %s", err)
	}

	err = s.AssignStaticIP(ctx, netInterfaces[0].InterfaceID, staticIP1)
	if err != nil {
		t.Fatalf("assign ip %s, expect success, get %s", staticIP1, err)
	}

	staticIP2 := "172.31.64.3"
	err = s.AssignStaticIP(ctx, netInterfaces[0].InterfaceID, staticIP2)
	if err != nil {
		t.Fatalf("assign ip %s, expect success, get %s", staticIP2, err)
	}

	netInterface, err := s.GetInstanceNetworkInterface(ctx, netInterfaces[0].ServerInstanceID)
	if err != nil {
		t.Fatalf("GetInstanceNetworkInterface expect success, get %s", err)
	}
	if len(netInterface.PrivateIPs) != 2 {
		t.Fatalf("expect 2 static ips, get %s", netInterface.PrivateIPs)
	}

	err = s.UnassignStaticIP(ctx, netInterfaces[0].InterfaceID, staticIP1)
	if err != nil {
		t.Fatalf("unassign ip should return success for ip %s, get %s", staticIP1, err)
	}
	err = s.UnassignStaticIP(ctx, netInterfaces[0].InterfaceID, staticIP2)
	if err != nil {
		t.Fatalf("unassign ip should return success for ip %s, get %s", staticIP2, err)
	}
}
