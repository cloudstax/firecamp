package firecampdockervolume

import (
	"testing"
)

func TestVolumeIPUtil(t *testing.T) {
	ip := "10.0.0.1"
	err := addIP(ip, "lo")
	if err != nil {
		t.Fatalf("add ip %s to eth0 error %s", ip, err)
	}

	err = delIP(ip, "lo")
	if err != nil {
		t.Fatalf("del ip %s from eth0 error %s", ip, err)
	}
}
