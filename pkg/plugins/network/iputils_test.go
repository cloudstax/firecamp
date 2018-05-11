package dockernetwork

import (
	"testing"
)

func TestAddDelIP(t *testing.T) {
	ip := "10.0.0.1"
	ifname := "lo"
	err := AddIP(ip, ifname)
	if err != nil {
		t.Fatalf("add ip %s to lo error %s", ip, err)
	}

	err = DeleteIP(ip, ifname)
	if err != nil {
		t.Fatalf("del ip %s from lo error %s", ip, err)
	}
}
