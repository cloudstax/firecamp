package escatalog

import (
	"strconv"
	"strings"
	"testing"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
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

	req, err := GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if err != nil {
		t.Fatalf("GenDefaultCreateServiceRequest error %s", err)
	}
	if req.Replicas != replicas || len(req.ReplicaConfigs) != int(replicas) {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	opts.DisableDedicatedMaster = true
	opts.DisableForceAwareness = true
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 0 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 0 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req, err = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if err != nil {
		t.Fatalf("GenDefaultCreateServiceRequest error %s", err)
	}
	if req.Replicas != replicas || len(req.ReplicaConfigs) != int(replicas) {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}

	opts.DedicatedMasters = 3
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 0 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 0 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req, err = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if err != nil {
		t.Fatalf("GenDefaultCreateServiceRequest error %s", err)
	}
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

	req, err = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if err != nil {
		t.Fatalf("GenDefaultCreateServiceRequest error %s", err)
	}
	if req.Replicas != replicas || len(req.ReplicaConfigs) != int(replicas) {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}
	for i := int64(0); i < replicas; i++ {
		if req.ReplicaConfigs[i].MemberName != utils.GenServiceMemberName(service, i) {
			t.Fatalf("expect replicas member name %s get %%s", utils.GenServiceMemberName(service, i), req.ReplicaConfigs[i].MemberName)
		}
		if !strings.Contains(req.ReplicaConfigs[i].Configs[1].Content, "node.master: true") {
			t.Fatalf("replica %d could become master, get %s", i, req.ReplicaConfigs[i].Configs[1].Content)
		}
	}

	opts.DisableDedicatedMaster = false
	opts.DisableForceAwareness = false

	expectHosts = service + "-master-0." + domain + ", " + service + "-master-1." + domain + ", " + service + "-master-2." + domain
	expectMasterNodes = int64(3)
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 3 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 0 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req, err = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if err != nil {
		t.Fatalf("GenDefaultCreateServiceRequest error %s", err)
	}
	if req.Replicas != replicas+int64(3) || len(req.ReplicaConfigs) != int(replicas)+3 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}
	for i := int64(0); i < expectMasterNodes; i++ {
		if req.ReplicaConfigs[i].MemberName != getDedicatedMasterName(service, i) {
			t.Fatalf("expect master member name %s get %%s", getDedicatedMasterName(service, i), req.ReplicaConfigs[i].MemberName)
		}
		if !strings.Contains(req.ReplicaConfigs[i].Configs[1].Content, "node.master: true") {
			t.Fatalf("master member %d could become master, get %s", i, req.ReplicaConfigs[i].Configs[1].Content)
		}
	}
	for j := int64(0); j < replicas; j++ {
		i := j + expectMasterNodes
		if req.ReplicaConfigs[i].MemberName != utils.GenServiceMemberName(service, j) {
			t.Fatalf("expect replicas member name %s get %%s", utils.GenServiceMemberName(service, j), req.ReplicaConfigs[i].MemberName)
		}
		if !strings.Contains(req.ReplicaConfigs[i].Configs[1].Content, "node.master: false") {
			t.Fatalf("replica %d could not become master, get %s", i, req.ReplicaConfigs[i].Configs[1].Content)
		}
	}

	opts.DedicatedMasters = 5
	expectHosts = service + "-master-0." + domain
	for i := 1; i < 5; i++ {
		expectHosts += ", " + service + "-master-" + strconv.Itoa(i) + "." + domain
	}
	expectMasterNodes = int64(5)
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 5 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 5 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req, err = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if err != nil {
		t.Fatalf("GenDefaultCreateServiceRequest error %s", err)
	}
	if req.Replicas != replicas+int64(5) || len(req.ReplicaConfigs) != int(replicas)+5 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}
	for i := int64(0); i < expectMasterNodes; i++ {
		if req.ReplicaConfigs[i].MemberName != getDedicatedMasterName(service, i) {
			t.Fatalf("expect master member name %s get %%s", getDedicatedMasterName(service, i), req.ReplicaConfigs[i].MemberName)
		}
		if !strings.Contains(req.ReplicaConfigs[i].Configs[1].Content, "node.master: true") {
			t.Fatalf("master member %d could become master, get %s", i, req.ReplicaConfigs[i].Configs[1].Content)
		}
	}
	for j := int64(0); j < replicas; j++ {
		i := j + expectMasterNodes
		if req.ReplicaConfigs[i].MemberName != utils.GenServiceMemberName(service, j) {
			t.Fatalf("expect replicas member name %s get %%s", utils.GenServiceMemberName(service, j), req.ReplicaConfigs[i].MemberName)
		}
		if !strings.Contains(req.ReplicaConfigs[i].Configs[1].Content, "node.master: false") {
			t.Fatalf("replica %d could not become master, get %s", i, req.ReplicaConfigs[i].Configs[1].Content)
		}
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

	req, err = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if err != nil {
		t.Fatalf("GenDefaultCreateServiceRequest error %s", err)
	}
	if req.Replicas != replicas || len(req.ReplicaConfigs) != int(replicas) {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}
	for i := int64(0); i < replicas; i++ {
		if req.ReplicaConfigs[i].MemberName != utils.GenServiceMemberName(service, i) {
			t.Fatalf("expect replicas member name %s get %%s", utils.GenServiceMemberName(service, i), req.ReplicaConfigs[i].MemberName)
		}
		if !strings.Contains(req.ReplicaConfigs[i].Configs[1].Content, "node.master: true") {
			t.Fatalf("replica %d could not become master, get %s", i, req.ReplicaConfigs[i].Configs[1].Content)
		}
	}

	opts.DisableDedicatedMaster = false
	opts.DisableForceAwareness = false

	expectHosts = service + "-master-0." + domain + ", " + service + "-master-1." + domain + ", " + service + "-master-2." + domain
	expectMasterNodes = int64(3)
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 3 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 3 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req, err = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if err != nil {
		t.Fatalf("GenDefaultCreateServiceRequest error %s", err)
	}
	if req.Replicas != replicas+int64(3) || len(req.ReplicaConfigs) != int(replicas)+3 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}
	for i := int64(0); i < expectMasterNodes; i++ {
		if req.ReplicaConfigs[i].MemberName != getDedicatedMasterName(service, i) {
			t.Fatalf("expect master member name %s get %%s", getDedicatedMasterName(service, i), req.ReplicaConfigs[i].MemberName)
		}
		if !strings.Contains(req.ReplicaConfigs[i].Configs[1].Content, "node.master: true") {
			t.Fatalf("master member %d could become master, get %s", i, req.ReplicaConfigs[i].Configs[1].Content)
		}
	}
	for j := int64(0); j < replicas; j++ {
		i := j + expectMasterNodes
		if req.ReplicaConfigs[i].MemberName != utils.GenServiceMemberName(service, j) {
			t.Fatalf("expect replicas member name %s get %%s", utils.GenServiceMemberName(service, j), req.ReplicaConfigs[i].MemberName)
		}
		if !strings.Contains(req.ReplicaConfigs[i].Configs[1].Content, "node.master: false") {
			t.Fatalf("replica %d could not become master, get %s", i, req.ReplicaConfigs[i].Configs[1].Content)
		}
	}

	opts.DedicatedMasters = 5
	expectHosts = service + "-master-0." + domain
	for i := 1; i < 5; i++ {
		expectHosts += ", " + service + "-master-" + strconv.Itoa(i) + "." + domain
	}
	expectMasterNodes = int64(5)
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 5 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 5 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req, err = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if err != nil {
		t.Fatalf("GenDefaultCreateServiceRequest error %s", err)
	}
	if req.Replicas != replicas+int64(5) || len(req.ReplicaConfigs) != int(replicas)+5 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}
	for i := int64(0); i < expectMasterNodes; i++ {
		if req.ReplicaConfigs[i].MemberName != getDedicatedMasterName(service, i) {
			t.Fatalf("expect master member name %s get %%s", getDedicatedMasterName(service, i), req.ReplicaConfigs[i].MemberName)
		}
		if !strings.Contains(req.ReplicaConfigs[i].Configs[1].Content, "node.master: true") {
			t.Fatalf("master member %d could become master, get %s", i, req.ReplicaConfigs[i].Configs[1].Content)
		}
	}
	for j := int64(0); j < replicas; j++ {
		i := j + expectMasterNodes
		if req.ReplicaConfigs[i].MemberName != utils.GenServiceMemberName(service, j) {
			t.Fatalf("expect replicas member name %s get %%s", utils.GenServiceMemberName(service, j), req.ReplicaConfigs[i].MemberName)
		}
		if !strings.Contains(req.ReplicaConfigs[i].Configs[1].Content, "node.master: false") {
			t.Fatalf("replica %d could not become master, get %s", i, req.ReplicaConfigs[i].Configs[1].Content)
		}
	}

	// 12 replicas
	replicas = int64(12)
	opts.Replicas = replicas
	opts.DedicatedMasters = 3
	opts.DisableDedicatedMaster = true
	opts.DisableForceAwareness = true

	expectHosts = service + "-master-0." + domain + ", " + service + "-master-1." + domain + ", " + service + "-master-2." + domain
	expectMasterNodes = int64(3)
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 3 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 3 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req, err = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if err != nil {
		t.Fatalf("GenDefaultCreateServiceRequest error %s", err)
	}
	if req.Replicas != replicas+3 || len(req.ReplicaConfigs) != int(replicas)+3 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}
	for i := int64(0); i < expectMasterNodes; i++ {
		if req.ReplicaConfigs[i].MemberName != getDedicatedMasterName(service, i) {
			t.Fatalf("expect master member name %s get %%s", getDedicatedMasterName(service, i), req.ReplicaConfigs[i].MemberName)
		}
		if !strings.Contains(req.ReplicaConfigs[i].Configs[1].Content, "node.master: true") {
			t.Fatalf("master member %d could become master, get %s", i, req.ReplicaConfigs[i].Configs[1].Content)
		}
	}
	for j := int64(0); j < replicas; j++ {
		i := j + expectMasterNodes
		if req.ReplicaConfigs[i].MemberName != utils.GenServiceMemberName(service, j) {
			t.Fatalf("expect replicas member name %s get %%s", utils.GenServiceMemberName(service, j), req.ReplicaConfigs[i].MemberName)
		}
		if !strings.Contains(req.ReplicaConfigs[i].Configs[1].Content, "node.master: false") {
			t.Fatalf("replica %d could not become master, get %s", i, req.ReplicaConfigs[i].Configs[1].Content)
		}
	}

	opts.DisableDedicatedMaster = false
	opts.DisableForceAwareness = false

	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 3 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 3 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req, err = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if err != nil {
		t.Fatalf("GenDefaultCreateServiceRequest error %s", err)
	}
	if req.Replicas != replicas+int64(3) || len(req.ReplicaConfigs) != int(replicas)+3 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}
	for i := int64(0); i < expectMasterNodes; i++ {
		if req.ReplicaConfigs[i].MemberName != getDedicatedMasterName(service, i) {
			t.Fatalf("expect master member name %s get %%s", getDedicatedMasterName(service, i), req.ReplicaConfigs[i].MemberName)
		}
		if !strings.Contains(req.ReplicaConfigs[i].Configs[1].Content, "node.master: true") {
			t.Fatalf("master member %d could become master, get %s", i, req.ReplicaConfigs[i].Configs[1].Content)
		}
	}
	for j := int64(0); j < replicas; j++ {
		i := j + expectMasterNodes
		if req.ReplicaConfigs[i].MemberName != utils.GenServiceMemberName(service, j) {
			t.Fatalf("expect replicas member name %s get %%s", utils.GenServiceMemberName(service, j), req.ReplicaConfigs[i].MemberName)
		}
		if !strings.Contains(req.ReplicaConfigs[i].Configs[1].Content, "node.master: false") {
			t.Fatalf("replica %d could not become master, get %s", i, req.ReplicaConfigs[i].Configs[1].Content)
		}
	}

	opts.DedicatedMasters = 5
	expectMasterNodes = 5
	expectHosts = service + "-master-0." + domain
	for i := 1; i < 5; i++ {
		expectHosts += ", " + service + "-master-" + strconv.Itoa(i) + "." + domain
	}
	unicastHosts, masterNodeNumber, addMasterNodes = getUnicastHostsAndMasterNodes(domain, service, opts)
	if unicastHosts != expectHosts || masterNodeNumber != expectMasterNodes || addMasterNodes != 5 {
		t.Fatalf("expect hosts %s get %s, expect masterNodes %d get %d, expect addMasterNodes 5 get %d", expectHosts, unicastHosts, expectMasterNodes, masterNodeNumber, addMasterNodes)
	}

	req, err = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if err != nil {
		t.Fatalf("GenDefaultCreateServiceRequest error %s", err)
	}
	if req.Replicas != replicas+int64(5) || len(req.ReplicaConfigs) != int(replicas)+5 {
		t.Fatalf("expect replicas %d get %d, ReplicaConfigs %d", replicas, req.Replicas, len(req.ReplicaConfigs))
	}
	for i := int64(0); i < expectMasterNodes; i++ {
		if req.ReplicaConfigs[i].MemberName != getDedicatedMasterName(service, i) {
			t.Fatalf("expect master member name %s get %%s", getDedicatedMasterName(service, i), req.ReplicaConfigs[i].MemberName)
		}
		if !strings.Contains(req.ReplicaConfigs[i].Configs[1].Content, "node.master: true") {
			t.Fatalf("master member %d could become master, get %s", i, req.ReplicaConfigs[i].Configs[1].Content)
		}
	}
	for j := int64(0); j < replicas; j++ {
		i := j + expectMasterNodes
		if req.ReplicaConfigs[i].MemberName != utils.GenServiceMemberName(service, j) {
			t.Fatalf("expect replicas member name %s get %%s", utils.GenServiceMemberName(service, j), req.ReplicaConfigs[i].MemberName)
		}
		if !strings.Contains(req.ReplicaConfigs[i].Configs[1].Content, "node.master: false") {
			t.Fatalf("replica %d could not become master, get %s", i, req.ReplicaConfigs[i].Configs[1].Content)
		}
	}

}
