package rediscatalog

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/manage"
)

func TestRedisConfigs(t *testing.T) {
	region := "region1"
	platform := common.ContainerPlatformECS
	cluster := "c1"
	service := "service1"
	azs := []string{"az1", "az2", "az3"}
	opts := &manage.CatalogRedisOptions{
		Shards:            int64(3),
		ReplicasPerShard:  int64(2),
		MemoryCacheSizeMB: 256,
		Volume: &common.ServiceVolume{
			VolumeType:   common.VolumeTypeGPSSD,
			VolumeSizeGB: int64(1),
		},
		DisableAOF:      false,
		AuthPass:        "pass",
		ReplTimeoutSecs: MinReplTimeoutSecs,
		MaxMemPolicy:    MaxMemPolicyAllKeysLRU,
		ConfigCmdName:   "mycfg",
	}

	// test 3 shards and 2 replicas each shard
	replcfgs := genReplicaConfigs(platform, cluster, service, azs, opts)
	if len(replcfgs) != 6 {
		t.Fatalf("expect 6 replica configs, get %d", len(replcfgs))
	}

	if replcfgs[0].Zone != azs[0] || replcfgs[0].MemberName != "service1-0" ||
		replcfgs[1].Zone != azs[1] || replcfgs[1].MemberName != "service1-1" ||
		replcfgs[2].Zone != azs[1] || replcfgs[2].MemberName != "service1-2" ||
		replcfgs[3].Zone != azs[2] || replcfgs[3].MemberName != "service1-3" ||
		replcfgs[4].Zone != azs[2] || replcfgs[4].MemberName != "service1-4" ||
		replcfgs[5].Zone != azs[0] || replcfgs[5].MemberName != "service1-5" {
		t.Fatalf("replica configs zone mismatch, %s", replcfgs)
	}

	// test init task evn pairs
	envPairs := GenInitTaskEnvKVPairs(region, cluster, "manageurl", service, opts.Shards, opts.ReplicasPerShard)
	if len(envPairs) != 11 {
		t.Fatalf("expect 11 env pairs, get %d", len(envPairs))
	}
	expectMasters := "service1-0.c1-firecamp.com,service1-2.c1-firecamp.com,service1-4.c1-firecamp.com"
	if envPairs[9].Name != envMasters || envPairs[9].Value != expectMasters {
		t.Fatalf("expect masters %s, get %s", expectMasters, envPairs[9].Value)
	}
	expectSlaves := "service1-1.c1-firecamp.com,service1-3.c1-firecamp.com,service1-5.c1-firecamp.com"
	if envPairs[10].Name != envSlaves || envPairs[10].Value != expectSlaves {
		t.Fatalf("expect slaves %s, get %s", expectSlaves, envPairs[9].Value)
	}

	// test 4 shards and 3 replicas each shard
	opts.Shards = 4
	opts.ReplicasPerShard = 3
	replcfgs = genReplicaConfigs(platform, cluster, service, azs, opts)
	if len(replcfgs) != 12 {
		t.Fatalf("expect 12 replica configs, get %d", len(replcfgs))
	}

	if replcfgs[0].Zone != azs[0] || replcfgs[0].MemberName != "service1-0" ||
		replcfgs[1].Zone != azs[1] || replcfgs[1].MemberName != "service1-1" ||
		replcfgs[2].Zone != azs[2] || replcfgs[2].MemberName != "service1-2" ||
		replcfgs[3].Zone != azs[1] || replcfgs[3].MemberName != "service1-3" ||
		replcfgs[4].Zone != azs[2] || replcfgs[4].MemberName != "service1-4" ||
		replcfgs[5].Zone != azs[0] || replcfgs[5].MemberName != "service1-5" ||
		replcfgs[6].Zone != azs[2] || replcfgs[6].MemberName != "service1-6" ||
		replcfgs[7].Zone != azs[0] || replcfgs[7].MemberName != "service1-7" ||
		replcfgs[8].Zone != azs[1] || replcfgs[8].MemberName != "service1-8" ||
		replcfgs[9].Zone != azs[0] || replcfgs[9].MemberName != "service1-9" ||
		replcfgs[10].Zone != azs[1] || replcfgs[10].MemberName != "service1-10" ||
		replcfgs[11].Zone != azs[2] || replcfgs[11].MemberName != "service1-11" {
		t.Fatalf("replica configs zone mismatch, %s", replcfgs)
	}

	// test init task evn pairs
	envPairs = GenInitTaskEnvKVPairs(region, cluster, "manageurl", service, opts.Shards, opts.ReplicasPerShard)
	if len(envPairs) != 11 {
		t.Fatalf("expect 11 env pairs, get %d", len(envPairs))
	}
	expectMasters = "service1-0.c1-firecamp.com,service1-3.c1-firecamp.com,service1-6.c1-firecamp.com,service1-9.c1-firecamp.com"
	if envPairs[9].Name != envMasters || envPairs[9].Value != expectMasters {
		t.Fatalf("expect masters %s, get %s", expectMasters, envPairs[9].Value)
	}
	expectSlaves = "service1-1.c1-firecamp.com,service1-2.c1-firecamp.com,service1-4.c1-firecamp.com,service1-5.c1-firecamp.com,service1-7.c1-firecamp.com,service1-8.c1-firecamp.com,service1-10.c1-firecamp.com,service1-11.c1-firecamp.com"
	if envPairs[10].Name != envSlaves || envPairs[10].Value != expectSlaves {
		t.Fatalf("expect slaves %s, get %s", expectSlaves, envPairs[10].Value)
	}

	// test 1 shards and 3 replicas each shard
	opts.Shards = 1
	opts.ReplicasPerShard = 3
	replcfgs = genReplicaConfigs(platform, cluster, service, azs, opts)
	if len(replcfgs) != 3 {
		t.Fatalf("expect 3 replica configs, get %d", len(replcfgs))
	}

	if replcfgs[0].Zone != azs[0] || replcfgs[0].MemberName != "service1-0" ||
		replcfgs[1].Zone != azs[1] || replcfgs[1].MemberName != "service1-1" ||
		replcfgs[2].Zone != azs[2] || replcfgs[2].MemberName != "service1-2" {
		t.Fatalf("replica configs zone mismatch, %s", replcfgs)
	}

	// test 1 shards and 1 replicas each shard
	opts.Shards = 1
	opts.ReplicasPerShard = 1
	replcfgs = genReplicaConfigs(platform, cluster, service, azs, opts)
	if len(replcfgs) != 1 {
		t.Fatalf("expect 1 replica configs, get %d", len(replcfgs))
	}

	if replcfgs[0].Zone != azs[0] || replcfgs[0].MemberName != "service1-0" {
		t.Fatalf("replica configs zone mismatch, %s", replcfgs)
	}
}

func TestParseRedisConfigs(t *testing.T) {
	platform := "ecs"
	shards := int64(3)
	replPerShard := int64(3)
	memorySize := int64(100)
	disableAof := false
	authPass := "auth"
	replTimeout := int64(10)
	maxMemPolicy := "lru"
	configCmd := ""

	content := fmt.Sprintf(servicefileContent, platform, shards, replPerShard, memorySize,
		strconv.FormatBool(disableAof), authPass, replTimeout, maxMemPolicy, configCmd)
	opts, err := ParseServiceConfigs(content)
	if err != nil {
		t.Fatalf("ParseServiceConfigs error %s", err)
	}
	if opts.Shards != shards || opts.ReplicasPerShard != replPerShard ||
		opts.MemoryCacheSizeMB != memorySize || opts.DisableAOF != disableAof ||
		opts.AuthPass != authPass || opts.ReplTimeoutSecs != replTimeout ||
		opts.MaxMemPolicy != maxMemPolicy || opts.ConfigCmdName != configCmd {
		t.Fatalf("config mismatch, %s", opts)
	}
}
