package escatalog

import (
	"strconv"
	"testing"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
)

func TestESCatalog(t *testing.T) {
	platform := common.ContainerPlatformECS
	region := "region1"
	cluster := "ct1"
	service := "svc1"
	azs := []string{"az1", "az2", "az3"}
	res := &common.Resources{}
	domain := dns.GenDefaultDomainName(cluster)

	// 1 replica
	replicas := int64(1)
	opts := &manage.CatalogElasticSearchOptions{
		Replicas:               replicas,
		VolumeSizeGB:           int64(1),
		DisableDedicatedMaster: false,
		DisableForceAwareness:  false,
	}

	unicastHosts, minMasterNodes := getUnicastHostsAndMinMasterNodes(domain, service, opts)
	expectHosts := service + "-0." + domain
	expectMasterNodes := int64(1)
	if unicastHosts != expectHosts || minMasterNodes != expectMasterNodes {
		t.Fatalf("expect hosts %s get %s, expect minMasterNodes %d get %d", expectHosts, unicastHosts, expectMasterNodes, minMasterNodes)
	}

	req := GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas || len(req.ReplicaConfigs) != int(replicas) {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	opts.DisableDedicatedMaster = true
	opts.DisableForceAwareness = true
	unicastHosts, minMasterNodes = getUnicastHostsAndMinMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || minMasterNodes != expectMasterNodes {
		t.Fatalf("expect hosts %s get %s, expect minMasterNodes %d get %d", expectHosts, unicastHosts, expectMasterNodes, minMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas || len(req.ReplicaConfigs) != int(replicas) {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	// 3 replicas
	replicas = int64(3)
	opts.Replicas = replicas
	opts.DisableDedicatedMaster = true
	opts.DisableForceAwareness = true

	unicastHosts, minMasterNodes = getUnicastHostsAndMinMasterNodes(domain, service, opts)
	expectHosts = service + "-0." + domain + ", " + service + "-1." + domain
	expectMasterNodes = int64(2)
	if unicastHosts != expectHosts || minMasterNodes != expectMasterNodes {
		t.Fatalf("expect hosts %s get %s, expect minMasterNodes %d get %d", expectHosts, unicastHosts, expectMasterNodes, minMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas || len(req.ReplicaConfigs) != int(replicas) {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	opts.DisableDedicatedMaster = false
	opts.DisableForceAwareness = false

	unicastHosts, minMasterNodes = getUnicastHostsAndMinMasterNodes(domain, service, opts)
	expectHosts = service + "-0." + domain + ", " + service + "-1." + domain + ", " + service + "-2." + domain
	expectMasterNodes = int64(2)
	if unicastHosts != expectHosts || minMasterNodes != expectMasterNodes {
		t.Fatalf("expect hosts %s get %s, expect minMasterNodes %d get %d", expectHosts, unicastHosts, expectMasterNodes, minMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas+int64(3) || len(req.ReplicaConfigs) != int(replicas)+3 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	// 8 replicas
	replicas = int64(8)
	opts.Replicas = replicas
	opts.DisableDedicatedMaster = true
	opts.DisableForceAwareness = true

	unicastHosts, minMasterNodes = getUnicastHostsAndMinMasterNodes(domain, service, opts)
	expectHosts = service + "-0." + domain
	for i := 1; i < 5; i++ {
		expectHosts += ", " + service + "-" + strconv.Itoa(i) + "." + domain
	}
	expectMasterNodes = int64(5)
	if unicastHosts != expectHosts || minMasterNodes != expectMasterNodes {
		t.Fatalf("expect hosts %s get %s, expect minMasterNodes %d get %d", expectHosts, unicastHosts, expectMasterNodes, minMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas || len(req.ReplicaConfigs) != int(replicas) {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	opts.DisableDedicatedMaster = false
	opts.DisableForceAwareness = false

	unicastHosts, minMasterNodes = getUnicastHostsAndMinMasterNodes(domain, service, opts)
	expectHosts = service + "-0." + domain + ", " + service + "-1." + domain + ", " + service + "-2." + domain
	expectMasterNodes = int64(2)
	if unicastHosts != expectHosts || minMasterNodes != expectMasterNodes {
		t.Fatalf("expect hosts %s get %s, expect minMasterNodes %d get %d", expectHosts, unicastHosts, expectMasterNodes, minMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas+int64(3) || len(req.ReplicaConfigs) != int(replicas)+3 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	// 12 replicas
	replicas = int64(12)
	opts.Replicas = replicas
	opts.DisableDedicatedMaster = true
	opts.DisableForceAwareness = true

	unicastHosts, minMasterNodes = getUnicastHostsAndMinMasterNodes(domain, service, opts)
	expectHosts = service + "-0." + domain + ", " + service + "-1." + domain + ", " + service + "-2." + domain
	expectMasterNodes = int64(2)
	if unicastHosts != expectHosts || minMasterNodes != expectMasterNodes {
		t.Fatalf("expect hosts %s get %s, expect minMasterNodes %d get %d", expectHosts, unicastHosts, expectMasterNodes, minMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas+3 || len(req.ReplicaConfigs) != int(replicas)+3 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	opts.DisableDedicatedMaster = false
	opts.DisableForceAwareness = false

	unicastHosts, minMasterNodes = getUnicastHostsAndMinMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || minMasterNodes != expectMasterNodes {
		t.Fatalf("expect hosts %s get %s, expect minMasterNodes %d get %d", expectHosts, unicastHosts, expectMasterNodes, minMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas+int64(3) || len(req.ReplicaConfigs) != int(replicas)+3 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

}
