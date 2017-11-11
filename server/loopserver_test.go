package server

import (
	"flag"
	"testing"

	"github.com/cloudstax/firecamp/common"

	"golang.org/x/net/context"
)

func TestLoopServerVolume(t *testing.T) {
	flag.Parse()
	//flag.Set("stderrthreshold", "INFO")

	ctx := context.Background()

	s := NewLoopServer()

	// create 2 volumes
	az := "az-west"
	opts := &CreateVolumeOptions{
		AvailabilityZone: az,
		VolumeType:       common.VolumeTypeGPSSD,
		VolumeSizeGB:     1,
	}
	vol1, err := s.CreateVolume(ctx, opts)
	if err != nil {
		t.Fatalf("failed to CreateVolume, error", err)
	}
	vol2, err := s.CreateVolume(ctx, opts)
	if err != nil {
		t.Fatalf("failed to CreateVolume, error", err)
	}

	// attach 2 volumes
	ins1 := "instanceid-1"
	dev1 := "/dev/loop1"
	err = s.AttachVolume(ctx, vol1, ins1, dev1)
	if err != nil {
		t.Fatalf("failed to attach volume", vol1, "error", err)
	}

	ins2 := "instanceid-2"
	dev2 := "/dev/loop2"
	err = s.AttachVolume(ctx, vol2, ins2, dev2)
	if err != nil {
		t.Fatalf("failed to attach volume", vol2, "error", err)
	}

	// get volume status
	state, err := s.GetVolumeState(ctx, vol1)
	if err != nil {
		t.Fatalf("failed to get volume", vol1, "state", state, "error", err)
	}
	state, err = s.GetVolumeState(ctx, vol2)
	if err != nil {
		t.Fatalf("failed to get volume", vol2, "state", state, "error", err)
	}

	// detach volumes
	err = s.DetachVolume(ctx, vol1, ins1, dev1)
	if err != nil {
		t.Fatalf("failed to detach volume", vol1, ins1, dev1, "error", err)
	}
	err = s.DetachVolume(ctx, vol2, ins2, dev2)
	if err != nil {
		t.Fatalf("failed to detach volume", vol2, ins2, dev2, "error", err)
	}

	// delete volume
	err = s.DeleteVolume(ctx, vol1)
	if err != nil {
		t.Fatalf("failed to DeleteVolume", vol1, "error", err)
	}
	err = s.DeleteVolume(ctx, vol2)
	if err != nil {
		t.Fatalf("failed to DeleteVolume", vol2, "error", err)
	}

	// error cases
	// get unexist volume status
	state, err = s.GetVolumeState(ctx, "vol-x")
	if err == nil {
		t.Fatalf("vol-x should not exist")
	}

	// detach unexist volume
	err = s.DetachVolume(ctx, "vol-x", "ins-x", "dev-x")
	if err == nil {
		t.Fatalf("detach vol-x should fail")
	}
}

func TestLoopServerNetwork(t *testing.T) {
	ctx := context.Background()

	s := NewLoopServer()

	// test network interfaces
	info := NewMockServerInfo()
	netInterfaces, cidrBlock, err := s.GetNetworkInterfaces(ctx, "cluster", "vpc", info.GetLocalAvailabilityZone())
	if err != nil {
		t.Fatalf("GetNetworkInterfaces error %s", err)
	}
	if len(netInterfaces) != 1 {
		t.Fatalf("expect 1 network interface, get %d", len(netInterfaces))
	}
	if cidrBlock != loopCidrBlock {
		t.Fatalf("expect cidrBlock %s, get %s", loopCidrBlock, cidrBlock)
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
