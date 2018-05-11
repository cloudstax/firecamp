package dns

import (
	"strconv"
	"testing"

	"github.com/cloudstax/firecamp/api/common"
)

func TestDomain(t *testing.T) {
	dnsname := "aaa.test.com"
	domain, err := GetDomainNameFromDNSName(dnsname)
	if err != nil || domain != "test.com" {
		t.Fatalf("expect domain test.com for %s, got %s, error %s", dnsname, domain, err)
	}

	dnsname = "test.com"
	domain, err = GetDomainNameFromDNSName(dnsname)
	if err != ErrDomainNotFound {
		t.Fatalf("expect ErrDomainNotFound for test.com, got %s", err)
	}

	addr, err := LookupHost("localhost")
	if err != nil {
		t.Fatalf("LookupHost localhost error %s", err)
	}
	if addr != "127.0.0.1" {
		t.Fatalf("LookupHost localhost expect 127.0.0.1, get %s", addr)
	}
}

func TestMgt(t *testing.T) {
	cluster := "c1"
	tlsEnabled := true
	portStr := strconv.Itoa(common.ManageHTTPServerPort)
	expect := "https://firecamp-manageserver.c1-firecamp.com:" + portStr + "/"
	url := GetDefaultManageServiceURL(cluster, tlsEnabled)
	if url != expect {
		t.Fatalf("GetDefaultManageServiceURL expect %s, got %s", expect, url)
	}
	url = FormatManageServiceURL(expect, tlsEnabled)
	if url != expect {
		t.Fatalf("GetDefaultManageServiceURL expect %s, got %s", expect, url)
	}
	url = FormatManageServiceURL("firecamp-manageserver.c1-firecamp.com:"+portStr, tlsEnabled)
	if url != expect {
		t.Fatalf("GetDefaultManageServiceURL expect %s, got %s", expect, url)
	}
	url = FormatManageServiceURL("https://firecamp-manageserver.c1-firecamp.com:"+portStr, tlsEnabled)
	if url != expect {
		t.Fatalf("GetDefaultManageServiceURL expect %s, got %s", expect, url)
	}

	tlsEnabled = false
	expect = "http://firecamp-manageserver.c1-firecamp.com:" + portStr + "/"
	url = GetDefaultManageServiceURL(cluster, tlsEnabled)
	if url != expect {
		t.Fatalf("GetDefaultManageServiceURL expect %s, got %s", expect, url)
	}
	url = FormatManageServiceURL(expect, tlsEnabled)
	if url != expect {
		t.Fatalf("GetDefaultManageServiceURL expect %s, got %s", expect, url)
	}
	url = FormatManageServiceURL("firecamp-manageserver.c1-firecamp.com:"+portStr, tlsEnabled)
	if url != expect {
		t.Fatalf("GetDefaultManageServiceURL expect %s, got %s", expect, url)
	}
	url = FormatManageServiceURL("http://firecamp-manageserver.c1-firecamp.com:"+portStr, tlsEnabled)
	if url != expect {
		t.Fatalf("GetDefaultManageServiceURL expect %s, got %s", expect, url)
	}
}
