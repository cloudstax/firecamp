package mongodbcatalog

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudstax/firecamp/api/catalog"
	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/pkg/dns"
)

func TestMongoDBReplicaConfig(t *testing.T) {
	region := "reg1"
	platform := "ecs"
	cluster := "t1"
	domain := "t1-firecamp.com"
	manageurl := "mgt." + domain
	service := "s1"
	member := "m1"
	az := "az1"
	azs := []string{"az1", "az2", "az3"}
	maxMemMB := int64(256)

	replSetName := getConfigServerName(service)
	role := configRole
	cfg := genReplicaConfig(platform, domain, member, replSetName, role, az)
	if cfg.Zone != az || cfg.MemberName != member || len(cfg.Configs) != 1 {
		t.Fatalf("expect zone %s member name %s 1 configs, get %s %s %d", az, member, cfg.Zone, cfg.MemberName, len(cfg.Configs))
	}
	if !strings.Contains(cfg.Configs[0].Content, configRole) {
		t.Fatalf("expect configsvr role for config server, get %s", cfg.Configs[0].Content)
	}

	replSetName = getShardName(service, 0)
	role = shardRole
	cfg = genReplicaConfig(platform, domain, member, replSetName, role, az)
	if cfg.Zone != az || cfg.MemberName != member || len(cfg.Configs) != 1 {
		t.Fatalf("expect zone %s member name %s 1 configs, get %s %s %d", az, member, cfg.Zone, cfg.MemberName, len(cfg.Configs))
	}
	if !strings.Contains(cfg.Configs[0].Content, shardRole) {
		t.Fatalf("expect shardsvr role for shard, get %s", cfg.Configs[0].Content)
	}

	replSetName = service
	role = emptyRole
	cfg = genReplicaConfig(platform, domain, member, replSetName, role, az)
	if cfg.Zone != az || cfg.MemberName != member || len(cfg.Configs) != 1 {
		t.Fatalf("expect zone %s member name %s 1 configs, get %s %s %d", az, member, cfg.Zone, cfg.MemberName, len(cfg.Configs))
	}
	if strings.Contains(cfg.Configs[0].Content, "clusterRole:") {
		t.Fatalf("not expect cluster role for replica set, get %s", cfg.Configs[0].Content)
	}

	// test service configs
	opts := &catalog.CatalogMongoDBOptions{
		Shards:           1,
		ReplicasPerShard: 1,
		ReplicaSetOnly:   true,
	}

	keyfileContent, err := GenKeyfileContent()
	if err != nil {
		t.Fatalf("genKeyfileContent error %s", err)
	}

	serviceCfgs := genServiceConfigs(platform, maxMemMB, opts, keyfileContent)
	if !strings.Contains(serviceCfgs[0].Content, "SHARDS=1") {
		t.Fatalf("expect 1 shards, get %s", cfg.Configs[0].Content)
	}

	// test replica configs
	member = service + "-0"
	replcfgs := genReplicaConfigs(platform, azs, cluster, service, maxMemMB, opts)
	if replcfgs[0].Zone != azs[0] || replcfgs[0].MemberName != member || len(replcfgs) != 1 || len(replcfgs[0].Configs) != 1 {
		t.Fatalf("expect zone %s member name %s 1 replcfg 3 configs, get %s %s %d %d", azs[0], member, replcfgs[0].Zone, replcfgs[0].MemberName, len(replcfgs), len(replcfgs[0].Configs))
	}
	if strings.Contains(replcfgs[0].Configs[0].Content, "clusterRole:") {
		t.Fatalf("not expect cluster role for replica set, get %s", replcfgs[0].Configs[0].Content)
	}
	members := dns.GenDNSName(service+"-0", domain)
	kvs := GenInitTaskEnvKVPairs(region, cluster, service, manageurl, opts)
	if len(kvs) != 12 || kvs[11].Name != common.ENV_SERVICE_MEMBERS || kvs[11].Value != members {
		t.Fatalf("expect 12 init kvs get %d, expect name %s value %s, get %s", len(kvs), common.ENV_SERVICE_MEMBERS, members, kvs[11])
	}

	opts.ReplicasPerShard = 3
	member = service + "-0"
	replcfgs = genReplicaConfigs(platform, azs, cluster, service, maxMemMB, opts)
	if replcfgs[0].Zone != azs[0] || replcfgs[0].MemberName != member || len(replcfgs) != 3 || len(replcfgs[0].Configs) != 1 {
		t.Fatalf("expect zone %s member name %s 3 replcfg 3 configs, get %s %s %d", az, member, replcfgs[0].Zone, replcfgs[0].MemberName, len(replcfgs), len(replcfgs[0].Configs))
	}
	if strings.Contains(replcfgs[1].Configs[0].Content, "clusterRole:") {
		t.Fatalf("not expect cluster role for replica set, get %s", replcfgs[0].Configs[0].Content)
	}
	members = dns.GenDNSName(service+"-0", domain) + "," + dns.GenDNSName(service+"-1", domain) + "," + dns.GenDNSName(service+"-2", domain)
	kvs = GenInitTaskEnvKVPairs(region, cluster, service, manageurl, opts)
	if len(kvs) != 12 || kvs[11].Name != common.ENV_SERVICE_MEMBERS || kvs[11].Value != members {
		t.Fatalf("expect 12 init kvs get %d, expect name %s value %s, get %s", common.ENV_SERVICE_MEMBERS, members, kvs[11])
	}

	opts.ReplicasPerShard = 1
	opts.ReplicaSetOnly = false
	opts.ConfigServers = 1
	member = service + "-config-0"
	replcfgs = genReplicaConfigs(platform, azs, cluster, service, maxMemMB, opts)
	if replcfgs[0].Zone != azs[0] || replcfgs[0].MemberName != member || len(replcfgs) != 2 || len(replcfgs[0].Configs) != 1 {
		t.Fatalf("expect zone %s member name %s 2 replcfg 3 configs, get %s %s %d", az, member, replcfgs[0].Zone, replcfgs[0].MemberName, len(replcfgs), len(replcfgs[0].Configs))
	}
	if strings.Contains(replcfgs[0].Configs[0].Content, "clusterrole: configsvr") {
		t.Fatalf("expect cluster role configsvr for replica set, get %s", replcfgs[0].Configs[0].Content)
	}
	if strings.Contains(replcfgs[1].Configs[0].Content, "clusterrole:") {
		t.Fatalf("not expect cluster role for replica set, get %s", replcfgs[1].Configs[0].Content)
	}
	kvs = GenInitTaskEnvKVPairs(region, cluster, service, manageurl, opts)
	members = dns.GenDNSName(service+"-config-0", domain)
	if len(kvs) != 13 || kvs[11].Name != envConfigServerMembers || kvs[11].Value != members {
		t.Fatalf("expect 13 init kvs get %d, expect name %s value %s, get %s", envConfigServerMembers, members, kvs[11])
	}
	members = dns.GenDNSName(service+"-shard0-0", domain)
	if kvs[12].Name != envShardMembers || kvs[12].Value != members {
		t.Fatalf("expect name %s value %s, get %s", envShardMembers, members, kvs[12])
	}

	opts.ReplicasPerShard = 3
	opts.ReplicaSetOnly = false
	opts.ConfigServers = 1
	member = service + "-config-0"
	replcfgs = genReplicaConfigs(platform, azs, cluster, service, maxMemMB, opts)
	if replcfgs[0].Zone != azs[0] || replcfgs[0].MemberName != member || len(replcfgs) != 4 || len(replcfgs[0].Configs) != 1 {
		t.Fatalf("expect zone %s member name %s 4 replcfg 3 configs, get %s %s %d", az, member, replcfgs[0].Zone, replcfgs[0].MemberName, len(replcfgs), len(replcfgs[0].Configs))
	}
	if strings.Contains(replcfgs[0].Configs[0].Content, "clusterrole: configsvr") {
		t.Fatalf("expect cluster role configsvr for replica set, get %s", replcfgs[0].Configs[0].Content)
	}
	if strings.Contains(replcfgs[1].Configs[0].Content, "clusterrole:") {
		t.Fatalf("not expect cluster role for replica set, get %s", replcfgs[1].Configs[0].Content)
	}
	kvs = GenInitTaskEnvKVPairs(region, cluster, service, manageurl, opts)
	members = dns.GenDNSName(service+"-config-0", domain)
	if len(kvs) != 13 || kvs[11].Name != envConfigServerMembers || kvs[11].Value != members {
		t.Fatalf("expect 13 init kvs get %d, expect name %s value %s, get %s", envConfigServerMembers, members, kvs[11])
	}
	members = dns.GenDNSName(service+"-shard0-0", domain) + "," + dns.GenDNSName(service+"-shard0-1", domain) + "," + dns.GenDNSName(service+"-shard0-2", domain)
	if kvs[12].Name != envShardMembers || kvs[12].Value != members {
		t.Fatalf("expect name %s value %s, get %s", envShardMembers, members, kvs[12])
	}

	opts.ReplicasPerShard = 3
	opts.ReplicaSetOnly = false
	opts.ConfigServers = 3
	member = service + "-config-0"
	replcfgs = genReplicaConfigs(platform, azs, cluster, service, maxMemMB, opts)
	if replcfgs[0].Zone != azs[0] || replcfgs[0].MemberName != member || len(replcfgs) != 6 || len(replcfgs[0].Configs) != 1 {
		t.Fatalf("expect zone %s member name %s 6 replcfg 3 configs, get %s %s %d", az, member, replcfgs[0].Zone, replcfgs[0].MemberName, len(replcfgs), len(replcfgs[0].Configs))
	}
	if strings.Contains(replcfgs[0].Configs[0].Content, "clusterrole: configsvr") {
		t.Fatalf("expect cluster role configsvr for replica set, get %s", replcfgs[0].Configs[0].Content)
	}
	if strings.Contains(replcfgs[3].Configs[0].Content, "clusterrole:") {
		t.Fatalf("not expect cluster role for replica set, get %s", replcfgs[3].Configs[0].Content)
	}
	kvs = GenInitTaskEnvKVPairs(region, cluster, service, manageurl, opts)
	members = dns.GenDNSName(service+"-config-0", domain) + "," + dns.GenDNSName(service+"-config-1", domain) + "," + dns.GenDNSName(service+"-config-2", domain)
	if len(kvs) != 13 || kvs[11].Name != envConfigServerMembers || kvs[11].Value != members {
		t.Fatalf("expect 13 init kvs get %d, expect name %s value %s, get %s", envConfigServerMembers, members, kvs[11])
	}
	members = dns.GenDNSName(service+"-shard0-0", domain) + "," + dns.GenDNSName(service+"-shard0-1", domain) + "," + dns.GenDNSName(service+"-shard0-2", domain)
	if kvs[12].Name != envShardMembers || kvs[12].Value != members {
		t.Fatalf("expect name %s value %s, get %s", envShardMembers, members, kvs[12])
	}

	opts.Shards = 2
	opts.ReplicasPerShard = 3
	opts.ReplicaSetOnly = false
	opts.ConfigServers = 3
	member = service + "-config-0"
	replcfgs = genReplicaConfigs(platform, azs, cluster, service, maxMemMB, opts)
	if replcfgs[0].Zone != azs[0] || replcfgs[0].MemberName != member || len(replcfgs) != 9 || len(replcfgs[0].Configs) != 1 {
		t.Fatalf("expect zone %s member name %s 9 replcfg 3 configs, get %s %s %d", az, member, replcfgs[0].Zone, replcfgs[0].MemberName, len(replcfgs), len(replcfgs[0].Configs))
	}
	// 3 config servers
	for i := int64(0); i < opts.ConfigServers; i++ {
		idx := int(i) % len(azs)
		if replcfgs[i].Zone != azs[idx] {
			t.Fatalf("expect zone %s for replica config %d, get zone %s", azs[idx], i, replcfgs[i].Zone)
		}
		member = service + "-config-" + strconv.FormatInt(i, 10)
		if replcfgs[i].MemberName != member {
			t.Fatalf("expect config member name %s for %d, get %s", member, i, replcfgs[i].MemberName)
		}
	}
	// shards
	for shard := int64(0); shard < opts.Shards; shard++ {
		for i := int64(0); i < opts.ReplicasPerShard; i++ {
			idx := int(shard+i) % len(azs)
			replIdx := opts.ConfigServers + shard*opts.ReplicasPerShard + i
			if replcfgs[replIdx].Zone != azs[idx] {
				t.Fatalf("expect zone %s for replica config %d, get zone %s", azs[idx], i, replcfgs[replIdx].Zone)
			}
			if shard == 0 {
				member = service + "-shard0-" + strconv.FormatInt(i, 10)
				if replcfgs[replIdx].MemberName != member {
					t.Fatalf("expect config member name %s for %d, get %s", member, i, replcfgs[replIdx].MemberName)
				}
			} else {
				member = service + "-shard1-" + strconv.FormatInt(i, 10)
				if replcfgs[replIdx].MemberName != member {
					t.Fatalf("expect config member name %s for %d, get %s", member, i, replcfgs[replIdx].MemberName)
				}
			}
		}
	}

	if strings.Contains(replcfgs[0].Configs[0].Content, "clusterrole: configsvr") {
		t.Fatalf("expect cluster role configsvr for replica set, get %s", replcfgs[0].Configs[0].Content)
	}
	if strings.Contains(replcfgs[3].Configs[0].Content, "clusterrole:") {
		t.Fatalf("not expect cluster role for replica set, get %s", replcfgs[3].Configs[0].Content)
	}
	kvs = GenInitTaskEnvKVPairs(region, cluster, service, manageurl, opts)
	members = dns.GenDNSName(service+"-config-0", domain) + "," + dns.GenDNSName(service+"-config-1", domain) + "," + dns.GenDNSName(service+"-config-2", domain)
	if len(kvs) != 13 || kvs[11].Name != envConfigServerMembers || kvs[11].Value != members {
		t.Fatalf("expect 13 init kvs get %d, expect name %s value %s, get %s", envConfigServerMembers, members, kvs[11])
	}
	members = dns.GenDNSName(service+"-shard0-0", domain) + "," + dns.GenDNSName(service+"-shard0-1", domain) + "," + dns.GenDNSName(service+"-shard0-2", domain)
	members += ";" + dns.GenDNSName(service+"-shard1-0", domain) + "," + dns.GenDNSName(service+"-shard1-1", domain) + "," + dns.GenDNSName(service+"-shard1-2", domain)
	if kvs[12].Name != envShardMembers || kvs[12].Value != members {
		t.Fatalf("expect name %s value %s, get %s", envShardMembers, members, kvs[12])
	}

}

func TestParseServiceConfigs(t *testing.T) {
	shards := int64(2)
	replPerShard := int64(3)
	replSetOnly := false
	configServers := int64(3)
	admin := "admin"
	pass := "pass"
	content := fmt.Sprintf(servicefileContent, "ecs", shards, replPerShard,
		strconv.FormatBool(replSetOnly), configServers, admin, pass)

	opts, err := ParseServiceConfigs(content)
	if err != nil {
		t.Fatalf("ParseServiceConfigs expect success, get error %s", err)
	}
	if opts.Shards != shards || opts.ReplicasPerShard != replPerShard ||
		opts.ReplicaSetOnly != replSetOnly || opts.ConfigServers != configServers ||
		opts.Admin != admin || opts.AdminPasswd != pass {
		t.Fatalf("config mismatch, get %s", opts)
	}
}
