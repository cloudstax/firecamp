package cascatalog

import (
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
}
