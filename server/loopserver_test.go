package server

import (
	"flag"
	"testing"

	"golang.org/x/net/context"
)

func TestLoopServer(t *testing.T) {
	flag.Parse()
	//flag.Set("stderrthreshold", "INFO")

	ctx := context.Background()

	s := NewLoopServer()

	// create 2 volumes
	az := "az-west"
	vol1, err := s.CreateVolume(ctx, az, 1)
	if err != nil {
		t.Fatalf("failed to CreateVolume, error", err)
	}
	vol2, err := s.CreateVolume(ctx, az, 1)
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
