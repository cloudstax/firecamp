package utils

import (
	"fmt"
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
	i := 0
	for i = 4; i < 9; i++ {
		nextIP, err := GetNextIP(usedIPs, ipnet, lastIP)
		if err != nil {
			t.Fatalf("GetNextIP error %s, last ip %s", err, lastIP)
		}
		expect := ipPrefix + strconv.Itoa(i)
		if nextIP.String() != expect {
			t.Fatalf("expect %s, get %s", expect, nextIP)
		}

		lastIP = nextIP
	}

	for i = 9; i < 21; i++ {
		nextIP, err := GetNextIP(usedIPs, ipnet, lastIP)
		if err != nil {
			t.Fatalf("GetNextIP error %s, last ip %s", err, lastIP)
		}
		expect := ipPrefix + strconv.Itoa(i)
		if nextIP.String() != expect {
			t.Fatalf("expect %s, get %s", expect, nextIP)
		}

		lastIP = nextIP
	}

	usedIPs[ipPrefix+strconv.Itoa(i)] = true
	nextIP, err := GetNextIP(usedIPs, ipnet, lastIP)
	if err != nil {
		t.Fatalf("GetNextIP error %s, last ip %s", err, lastIP)
	}
	expect := ipPrefix + strconv.Itoa(i+1)
	if nextIP.String() != expect {
		t.Fatalf("expect %s, get %s", expect, nextIP)
	}

	fmt.Println("lastIP", lastIP, "skipped", ipPrefix+strconv.Itoa(i), "nextIP", nextIP)
}
