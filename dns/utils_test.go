package dns

import (
	"strconv"
	"testing"

	"github.com/cloudstax/openmanage/common"
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
}

func TestMgt(t *testing.T) {
	cluster := "c1"
	tlsEnabled := true
	portStr := strconv.Itoa(common.ManageHTTPServerPort)
	expect := "https://openmanage-manageserver.c1-openmanage.com:" + portStr + "/"
	url := GetDefaultMgtServiceURL(cluster, tlsEnabled)
	if url != expect {
		t.Fatalf("GetDefaultMgtServiceURL expect %s, got %s", expect, url)
	}
	url = FormatMgtServiceURL(expect, tlsEnabled)
	if url != expect {
		t.Fatalf("GetDefaultMgtServiceURL expect %s, got %s", expect, url)
	}
	url = FormatMgtServiceURL("openmanage-manageserver.c1-openmanage.com:"+portStr, tlsEnabled)
	if url != expect {
		t.Fatalf("GetDefaultMgtServiceURL expect %s, got %s", expect, url)
	}
	url = FormatMgtServiceURL("https://openmanage-manageserver.c1-openmanage.com:"+portStr, tlsEnabled)
	if url != expect {
		t.Fatalf("GetDefaultMgtServiceURL expect %s, got %s", expect, url)
	}

	tlsEnabled = false
	expect = "http://openmanage-manageserver.c1-openmanage.com:" + portStr + "/"
	url = GetDefaultMgtServiceURL(cluster, tlsEnabled)
	if url != expect {
		t.Fatalf("GetDefaultMgtServiceURL expect %s, got %s", expect, url)
	}
	url = FormatMgtServiceURL(expect, tlsEnabled)
	if url != expect {
		t.Fatalf("GetDefaultMgtServiceURL expect %s, got %s", expect, url)
	}
	url = FormatMgtServiceURL("openmanage-manageserver.c1-openmanage.com:"+portStr, tlsEnabled)
	if url != expect {
		t.Fatalf("GetDefaultMgtServiceURL expect %s, got %s", expect, url)
	}
	url = FormatMgtServiceURL("http://openmanage-manageserver.c1-openmanage.com:"+portStr, tlsEnabled)
	if url != expect {
		t.Fatalf("GetDefaultMgtServiceURL expect %s, got %s", expect, url)
	}
}

func TestControldb(t *testing.T) {
	cluster := "cluster"
	portStr := strconv.Itoa(common.ControlDBServerPort)
	expect := "openmanage-controldb.cluster-openmanage.com:" + portStr
	addr := GetDefaultControlDBAddr(cluster)
	if addr != expect {
		t.Fatalf("GetDefaultControlDBAddr expect %s, got %s", expect, addr)
	}

	cluster = "c1"
	expect = "openmanage-controldb.c1-openmanage.com:" + portStr
	addr = GetDefaultControlDBAddr(cluster)
	if addr != expect {
		t.Fatalf("GetDefaultControlDBAddr expect %s, got %s", expect, addr)
	}
}
