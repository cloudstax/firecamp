package zkcatalog

import (
	"testing"
)

func TestZkCatalog(t *testing.T) {
	service := "s1"
	domain := "test.com"
	replicas := int64(3)

	expect := "\nserver.1=s1-0.test.com:2888:3888\nserver.2=s1-1.test.com:2888:3888\nserver.3=s1-2.test.com:2888:3888"
	serverList := genServerList(service, domain, replicas)
	if serverList != expect {
		t.Fatalf("expect %s, get %s", expect, serverList)
	}

	replicas = int64(5)
	expect = "\nserver.1=s1-0.test.com:2888:3888\nserver.2=s1-1.test.com:2888:3888\nserver.3=s1-2.test.com:2888:3888\nserver.4=s1-3.test.com:2888:3888\nserver.5=s1-4.test.com:2888:3888"
	serverList = genServerList(service, domain, replicas)
	if serverList != expect {
		t.Fatalf("expect %s, get %s", expect, serverList)
	}

}
