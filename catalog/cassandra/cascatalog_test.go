package cascatalog

import (
	"fmt"
	"testing"
)

func TestCasCatalog(t *testing.T) {
	service := "s"
	domain := "test.com"

	azCount := int64(3)
	replicas := int64(6)
	expectSeeds := "s-0.test.com,s-1.test.com,s-2.test.com"
	seeds := genSeedHosts(azCount, replicas, service, domain)
	if seeds != expectSeeds {
		t.Fatalf("expect seeds %s, get %s", expectSeeds, seeds)
	}

	azCount = int64(1)
	replicas = int64(3)
	expectSeeds = "s-0.test.com"
	seeds = genSeedHosts(azCount, replicas, service, domain)
	if seeds != expectSeeds {
		t.Fatalf("expect seeds %s, get %s", expectSeeds, seeds)
	}

	azCount = int64(3)
	replicas = int64(1)
	expectSeeds = "s-0.test.com"
	seeds = genSeedHosts(azCount, replicas, service, domain)
	if seeds != expectSeeds {
		t.Fatalf("expect seeds %s, get %s", expectSeeds, seeds)
	}

	azCount = int64(3)
	replicas = int64(2)
	expectSeeds = "s-0.test.com,s-1.test.com"
	seeds = genSeedHosts(azCount, replicas, service, domain)
	if seeds != expectSeeds {
		t.Fatalf("expect seeds %s, get %s", expectSeeds, seeds)
	}

	azCount = int64(6)
	replicas = int64(3)
	expectSeeds = "s-0.test.com,s-1.test.com,s-2.test.com"
	seeds = genSeedHosts(azCount, replicas, service, domain)
	if seeds != expectSeeds {
		t.Fatalf("expect seeds %s, get %s", expectSeeds, seeds)
	}

	// test update service.conf
	region := "region"
	cluster := "cluster1"
	opts := &CasOptions{
		HeapSizeMB:      256,
		JmxRemoteUser:   "jmxuser",
		JmxRemotePasswd: "jmxpasswd",
	}
	content := fmt.Sprintf(servicefileContent, region, cluster, seeds, opts.HeapSizeMB, opts.JmxRemoteUser, opts.JmxRemotePasswd)

	opts.HeapSizeMB = 300
	content1 := fmt.Sprintf(servicefileContent, region, cluster, seeds, opts.HeapSizeMB, opts.JmxRemoteUser, opts.JmxRemotePasswd)
	content2 := UpdateServiceConfig(content, opts)
	if content1 != content2 {
		t.Fatalf("expect service configs\n %s, get\n %s", content1, content2)
	}

	opts.JmxRemoteUser = "newjmxuser"
	opts.JmxRemotePasswd = "newjmxpd"
	content1 = fmt.Sprintf(servicefileContent, region, cluster, seeds, opts.HeapSizeMB, opts.JmxRemoteUser, opts.JmxRemotePasswd)
	content2 = UpdateServiceConfig(content, opts)
	if content1 != content2 {
		t.Fatalf("expect service configs\n %s, get\n %s", content1, content2)
	}
}
