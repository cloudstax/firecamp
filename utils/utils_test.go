package utils

import (
	"net"
	"strconv"
	"testing"
)

func TestCommonUtils(t *testing.T) {
	ipPrefix := "172.31.64."
	cidr := "172.31.64.0/20"

	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("ParseCIDR %s error %s", cidr, err)
	}

	usedIPs := make(map[string]bool)

	lastIP := ip
	for i := 4; i < 9; i++ {
		nextIP, err := GetNextIP(usedIPs, ipnet, lastIP)
		if err != nil {
			t.Fatalf("GetNextIP error %s, ip %s", err, ip)
		}
		expect := ipPrefix + strconv.Itoa(i)
		if nextIP.String() != expect {
			t.Fatalf("expect %s, get %s", expect, nextIP)
		}

		usedIPs[nextIP.String()] = true
		lastIP = nextIP
	}

	lastIP = ip
	for i := 9; i < 21; i++ {
		nextIP, err := GetNextIP(usedIPs, ipnet, lastIP)
		if err != nil {
			t.Fatalf("GetNextIP error %s, ip %s", err, ip)
		}
		expect := ipPrefix + strconv.Itoa(i)
		if nextIP.String() != expect {
			t.Fatalf("expect %s, get %s", expect, nextIP)
		}

		usedIPs[nextIP.String()] = true
		lastIP = nextIP
	}
}
