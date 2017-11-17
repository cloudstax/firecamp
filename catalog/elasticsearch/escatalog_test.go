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
		Replicas: replicas,
		Volume: &common.ServiceVolume{
			VolumeType:   common.VolumeTypeGPSSD,
			VolumeSizeGB: 1,
		},
		DedicatedMasters:       int64(0),
		DisableDedicatedMaster: false,
		DisableForceAwareness:  false,
	}

	unicastHosts, masterNodeNumber, addMasterNodes := getUnicastHostsAndMasterNodes(domain, service, opts)
	expectHosts := service + "-0." + domain
	expectMasterNodes := int64(1)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 0 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 0 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req := GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas || len(req.ReplicaConfigs) != int(replicas) {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	opts.DisableDedicatedMaster = true
	opts.DisableForceAwareness = true
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 0 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 0 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas || len(req.ReplicaConfigs) != int(replicas) {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	opts.DedicatedMasters = 3
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 0 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 0 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas || len(req.ReplicaConfigs) != int(replicas) {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	// 3 replicas
	replicas = int64(3)
	opts.Replicas = replicas
	opts.DedicatedMasters = 3
	opts.DisableDedicatedMaster = true
	opts.DisableForceAwareness = true

	expectHosts = service + "-0." + domain + ", " + service + "-1." + domain
	expectMasterNodes = int64(2)
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 0 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 0 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas || len(req.ReplicaConfigs) != int(replicas) {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	opts.DisableDedicatedMaster = false
	opts.DisableForceAwareness = false

	expectHosts = service + "-0." + domain + ", " + service + "-1." + domain + ", " + service + "-2." + domain
	expectMasterNodes = int64(3)
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 3 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 0 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas+int64(3) || len(req.ReplicaConfigs) != int(replicas)+3 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	opts.DedicatedMasters = 5
	expectHosts = service + "-0." + domain
	for i := 1; i < 5; i++ {
		expectHosts += ", " + service + "-" + strconv.Itoa(i) + "." + domain
	}
	expectMasterNodes = int64(5)
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 5 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 5 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas+int64(5) || len(req.ReplicaConfigs) != int(replicas)+5 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	// 8 replicas
	replicas = int64(8)
	opts.Replicas = replicas
	opts.DedicatedMasters = 3
	opts.DisableDedicatedMaster = true
	opts.DisableForceAwareness = true

	expectHosts = service + "-0." + domain
	for i := 1; i < 5; i++ {
		expectHosts += ", " + service + "-" + strconv.Itoa(i) + "." + domain
	}
	expectMasterNodes = int64(5)
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 0 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 0 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas || len(req.ReplicaConfigs) != int(replicas) {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	opts.DisableDedicatedMaster = false
	opts.DisableForceAwareness = false

	expectHosts = service + "-0." + domain + ", " + service + "-1." + domain + ", " + service + "-2." + domain
	expectMasterNodes = int64(3)
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 3 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 3 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas+int64(3) || len(req.ReplicaConfigs) != int(replicas)+3 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	opts.DedicatedMasters = 5
	expectHosts = service + "-0." + domain
	for i := 1; i < 5; i++ {
		expectHosts += ", " + service + "-" + strconv.Itoa(i) + "." + domain
	}
	expectMasterNodes = int64(5)
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 5 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 5 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas+int64(5) || len(req.ReplicaConfigs) != int(replicas)+5 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	// 12 replicas
	replicas = int64(12)
	opts.Replicas = replicas
	opts.DedicatedMasters = 3
	opts.DisableDedicatedMaster = true
	opts.DisableForceAwareness = true

	expectHosts = service + "-0." + domain + ", " + service + "-1." + domain + ", " + service + "-2." + domain
	expectMasterNodes = int64(3)
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 3 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 3 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas+3 || len(req.ReplicaConfigs) != int(replicas)+3 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	opts.DisableDedicatedMaster = false
	opts.DisableForceAwareness = false

	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 3 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 3 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas+int64(3) || len(req.ReplicaConfigs) != int(replicas)+3 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	opts.DedicatedMasters = 5
	expectMasterNodes = 5
	expectHosts = service + "-0." + domain
	for i := 1; i < 5; i++ {
		expectHosts += ", " + service + "-" + strconv.Itoa(i) + "." + domain
	}
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 5 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 5 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if req.Replicas != replicas+int64(5) || len(req.ReplicaConfigs) != int(replicas)+5 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

}
