package rediscatalog

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudstax/firecamp/catalog"
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
	replcfgs := GenReplicaConfigs(platform, cluster, service, azs, opts)
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

	content := strings.TrimSuffix(replcfgs[0].Configs[1].Content, defaultConfigs)
	content = EnableRedisAuth(content)
	content = SetClusterAnnounceIP(content, "ip1")
	content1 := fmt.Sprintf(redisConfigs1, "service1-0.c1-firecamp.com")
	if content != content1 {
		t.Fatalf("redis conf content mismatch, %s \nexpect %s", content, content1)
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
	replcfgs = GenReplicaConfigs(platform, cluster, service, azs, opts)
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

	content = strings.TrimSuffix(replcfgs[1].Configs[1].Content, defaultConfigs)
	content = EnableRedisAuth(content)
	content = SetClusterAnnounceIP(content, "ip1")
	content1 = fmt.Sprintf(redisConfigs1, "service1-1.c1-firecamp.com")
	if content != content1 {
		t.Fatalf("redis conf content mismatch, %s \nexpect\n %s", content, content1)
	}
	content = strings.TrimSuffix(replcfgs[4].Configs[1].Content, defaultConfigs)
	content = EnableRedisAuth(content)
	content = SetClusterAnnounceIP(content, "ip1")
	content1 = fmt.Sprintf(redisConfigs1, "service1-4.c1-firecamp.com")
	if content != content1 {
		t.Fatalf("redis conf content mismatch, %s \nexpect\n %s", content, content1)
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
	replcfgs = GenReplicaConfigs(platform, cluster, service, azs, opts)
	if len(replcfgs) != 3 {
		t.Fatalf("expect 3 replica configs, get %d", len(replcfgs))
	}

	if replcfgs[0].Zone != azs[0] || replcfgs[0].MemberName != "service1-0" ||
		replcfgs[1].Zone != azs[1] || replcfgs[1].MemberName != "service1-1" ||
		replcfgs[2].Zone != azs[2] || replcfgs[2].MemberName != "service1-2" {
		t.Fatalf("replica configs zone mismatch, %s", replcfgs)
	}

	content = strings.TrimSuffix(replcfgs[1].Configs[1].Content, defaultConfigs)
	content = EnableRedisAuth(content)
	content1 = fmt.Sprintf(redisConfigs2, "service1-1.c1-firecamp.com", 268435456, "\nslaveof service1-0.c1-firecamp.com 6379")
	if content != content1 {
		t.Fatalf("redis conf content mismatch, %s \nexpect\n %s", content, content1)
	}

	// test 1 shards and 1 replicas each shard
	opts.Shards = 1
	opts.ReplicasPerShard = 1
	replcfgs = GenReplicaConfigs(platform, cluster, service, azs, opts)
	if len(replcfgs) != 1 {
		t.Fatalf("expect 1 replica configs, get %d", len(replcfgs))
	}

	if replcfgs[0].Zone != azs[0] || replcfgs[0].MemberName != "service1-0" {
		t.Fatalf("replica configs zone mismatch, %s", replcfgs)
	}

	content = strings.TrimSuffix(replcfgs[0].Configs[1].Content, defaultConfigs)
	content = EnableRedisAuth(content)
	content += "\n"
	content1 = fmt.Sprintf(redisConfigs2, "service1-0.c1-firecamp.com", 268435456, "")
	if content != content1 {
		t.Fatalf("redis conf content mismatch, %s \nexpect\n %s", content, content1)
	}

}

func TestRedisUpdateFuncs(t *testing.T) {
	ua := &common.RedisUserAttr{
		Shards:            1,
		ReplicasPerShard:  1,
		MemoryCacheSizeMB: 100,
		DisableAOF:        false,
		AuthPass:          "changeme",
		ReplTimeoutSecs:   MinReplTimeoutSecs,
		MaxMemPolicy:      MaxMemPolicyAllKeysLRU,
		ConfigCmdName:     "newcfg",
	}

	newcfg := "newcfg"
	req := &manage.CatalogUpdateRedisRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      "region",
			Cluster:     "c1",
			ServiceName: "s1",
		},
		MemoryCacheSizeMB: 100,
		AuthPass:          "changeme",
		ReplTimeoutSecs:   MinReplTimeoutSecs,
		MaxMemPolicy:      MaxMemPolicyAllKeysLRU,
		ConfigCmdName:     &newcfg,
	}

	if IsConfigChanged(ua, req) {
		t.Fatalf("config not changed")
	}

	emptyStr := ""
	req.ConfigCmdName = &emptyStr
	if !IsConfigChanged(ua, req) {
		t.Fatalf("config changed")
	}
}

func TestRedisUpdate(t *testing.T) {
	region := "region1"
	platform := common.ContainerPlatformECS
	cluster := "c1"
	service := "service1"
	azs := []string{"az1", "az2", "az3"}
	res := &common.Resources{
		MaxCPUUnits:     common.DefaultMaxCPUUnits,
		ReserveCPUUnits: common.DefaultReserveCPUUnits,
		MaxMemMB:        common.DefaultMaxMemoryMB,
		ReserveMemMB:    common.DefaultReserveMemoryMB,
	}
	opts := &manage.CatalogRedisOptions{
		Shards:            int64(1),
		ReplicasPerShard:  int64(1),
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

	crReq, err := GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if err != nil {
		t.Fatalf("GenDefaultCreateServiceRequest error %s", err)
	}

	replcfgs := crReq.ReplicaConfigs
	if replcfgs[0].Zone != azs[0] || replcfgs[0].MemberName != "service1-0" {
		t.Fatalf("replica configs zone mismatch, %s", replcfgs)
	}

	content := strings.TrimSuffix(replcfgs[0].Configs[1].Content, defaultConfigs)
	content = EnableRedisAuth(content)
	content += "\n"
	content1 := fmt.Sprintf(redisConfigs2, "service1-0.c1-firecamp.com", 268435456, "")
	if content != content1 {
		t.Fatalf("redis conf content mismatch, %s \nexpect\n %s", content, content1)
	}

	ua := &common.RedisUserAttr{}
	err = json.Unmarshal(crReq.UserAttr.AttrBytes, ua)
	if err != nil {
		t.Fatalf("Unmarshal RedisUserAttr error %s", err)
	}

	// update content
	upReq := &manage.CatalogUpdateRedisRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     cluster,
			ServiceName: service,
		},
	}

	if IsConfigChanged(ua, upReq) {
		t.Fatalf("config is not changed")
	}

	// disable config cmd
	emptyStr := ""
	upReq.ConfigCmdName = &emptyStr
	if !IsConfigChanged(ua, upReq) {
		t.Fatalf("config is changed")
	}

	newContent := UpdateRedisConfig(content, ua, upReq)
	content1 = fmt.Sprintf(redisConfigs, "service1-0.c1-firecamp.com", listenPort,
		catalog.MBToBytes(ua.MemoryCacheSizeMB), ua.MaxMemPolicy, ua.ReplTimeoutSecs, *upReq.ConfigCmdName)
	content1 += fmt.Sprintf(authConfig, ua.AuthPass, ua.AuthPass)
	content1 += aofConfigs
	content1 = EnableRedisAuth(content1)
	content1 += "\n"
	if newContent != content1 {
		t.Fatalf("content mismatch, expect %s, get %s", content1, newContent)
	}

	// change MemoryCacheSizeMB only
	upReq.ConfigCmdName = &(opts.ConfigCmdName)
	upReq.MemoryCacheSizeMB = 100
	if !IsConfigChanged(ua, upReq) {
		t.Fatalf("config is changed")
	}

	newContent = UpdateRedisConfig(content, ua, upReq)
	content1 = fmt.Sprintf(redisConfigs, "service1-0.c1-firecamp.com", listenPort,
		catalog.MBToBytes(upReq.MemoryCacheSizeMB), ua.MaxMemPolicy, ua.ReplTimeoutSecs, ua.ConfigCmdName)
	content1 += fmt.Sprintf(authConfig, ua.AuthPass, ua.AuthPass)
	content1 += aofConfigs
	content1 = EnableRedisAuth(content1)
	content1 += "\n"
	if newContent != content1 {
		t.Fatalf("new memory size mismatch, expect %s, get %s", content1, newContent)
	}

	// change AuthPass only
	upReq.MemoryCacheSizeMB = opts.MemoryCacheSizeMB
	upReq.AuthPass = "newpass"
	if !IsConfigChanged(ua, upReq) {
		t.Fatalf("config is changed")
	}

	newContent = UpdateRedisConfig(content, ua, upReq)
	content1 = fmt.Sprintf(redisConfigs, "service1-0.c1-firecamp.com", listenPort,
		catalog.MBToBytes(upReq.MemoryCacheSizeMB), ua.MaxMemPolicy, ua.ReplTimeoutSecs, ua.ConfigCmdName)
	content1 += fmt.Sprintf(authConfig, upReq.AuthPass, upReq.AuthPass)
	content1 += aofConfigs
	content1 = EnableRedisAuth(content1)
	content1 += "\n"
	if newContent != content1 {
		t.Fatalf("new AuthPass mismatch, expect %s, get %s", content1, newContent)
	}

	// update all parameters
	upReq.MemoryCacheSizeMB = 100
	upReq.AuthPass = "newpass"
	upReq.MaxMemPolicy = MaxMemPolicyAllKeysLFU
	upReq.ReplTimeoutSecs = 100
	newcfg := "newcfg"
	upReq.ConfigCmdName = &newcfg
	if !IsConfigChanged(ua, upReq) {
		t.Fatalf("config is changed")
	}

	newContent = UpdateRedisConfig(content, ua, upReq)

	content1 = fmt.Sprintf(redisConfigs, "service1-0.c1-firecamp.com", listenPort,
		catalog.MBToBytes(upReq.MemoryCacheSizeMB), upReq.MaxMemPolicy, upReq.ReplTimeoutSecs, *upReq.ConfigCmdName)
	content1 += fmt.Sprintf(authConfig, upReq.AuthPass, upReq.AuthPass)
	content1 += aofConfigs
	content1 = EnableRedisAuth(content1)
	content1 += "\n"

	if newContent != content1 {
		t.Fatalf("content mismatch, expect %s, get %s", content1, newContent)
	}

	// AuthPass not enabled at creation, update AuthPass
	opts = &manage.CatalogRedisOptions{
		Shards:            int64(1),
		ReplicasPerShard:  int64(1),
		MemoryCacheSizeMB: 256,
		Volume: &common.ServiceVolume{
			VolumeType:   common.VolumeTypeGPSSD,
			VolumeSizeGB: int64(1),
		},
		DisableAOF:      false,
		AuthPass:        "",
		ReplTimeoutSecs: MinReplTimeoutSecs,
		MaxMemPolicy:    MaxMemPolicyAllKeysLRU,
		ConfigCmdName:   "",
	}

	crReq, err = GenDefaultCreateServiceRequest(platform, region, azs, cluster, service, res, opts)
	if err != nil {
		t.Fatalf("GenDefaultCreateServiceRequest error %s", err)
	}

	content = crReq.ReplicaConfigs[0].Configs[1].Content

	ua = &common.RedisUserAttr{}
	err = json.Unmarshal(crReq.UserAttr.AttrBytes, ua)
	if err != nil {
		t.Fatalf("Unmarshal RedisUserAttr error %s", err)
	}

	upReq.MemoryCacheSizeMB = 100
	upReq.AuthPass = "newpass"
	upReq.MaxMemPolicy = MaxMemPolicyAllKeysLFU
	upReq.ReplTimeoutSecs = 100
	upReq.ConfigCmdName = &newcfg
	if !IsConfigChanged(ua, upReq) {
		t.Fatalf("config is changed")
	}

	newContent = UpdateRedisConfig(content, ua, upReq)

	content1 = fmt.Sprintf(redisConfigs, "service1-0.c1-firecamp.com", listenPort,
		catalog.MBToBytes(upReq.MemoryCacheSizeMB), upReq.MaxMemPolicy, upReq.ReplTimeoutSecs, *upReq.ConfigCmdName)
	content1 += aofConfigs
	content1 += defaultConfigs
	content1 += fmt.Sprintf(authConfig, upReq.AuthPass, upReq.AuthPass)
	content1 = EnableRedisAuth(content1)

	if newContent != content1 {
		t.Fatalf("content mismatch, expect %s, get %s", content1, newContent)
	}
}

const redisConfigs1 = `
bind %s
port 6379

# Redis memory cache size in bytes. The total memory will be like maxmemory + slave output buffer.
maxmemory 268435456
maxmemory-policy allkeys-lru

# The filename where to dump the DB
dbfilename dump.rdb
# The directory where to dump the DB
dir /data/redis

# The empty string forces Redis to log on the standard output.
logfile ""
loglevel notice

# for how to calculate the desired replication timeout value, check
# https://redislabs.com/blog/top-redis-headaches-for-devops-replication-timeouts/
repl-timeout 60

# https://redislabs.com/blog/top-redis-headaches-for-devops-replication-buffer/
# set both the hard and soft limits to 512MB for slave clients.
# The normal and pubsub clients still use the default.
client-output-buffer-limit normal 0 0 0
client-output-buffer-limit slave 512mb 512mb 0
client-output-buffer-limit pubsub 32mb 8mb 60

rename-command FLUSHALL ""
rename-command FLUSHDB ""
rename-command SHUTDOWN ""
rename-command CONFIG "mycfg"

requirepass pass
masterauth pass

# append only file
appendonly yes
appendfilename "appendonly.aof"
appendfsync everysec

cluster-enabled yes
cluster-config-file /data/redis-node.conf

cluster-announce-ip ip1

cluster-node-timeout 15000
cluster-slave-validity-factor 10
cluster-migration-barrier 1
cluster-require-full-coverage yes
`

const redisConfigs2 = `
bind %s
port 6379

# Redis memory cache size in bytes. The total memory will be like maxmemory + slave output buffer.
maxmemory %d
maxmemory-policy allkeys-lru

# The filename where to dump the DB
dbfilename dump.rdb
# The directory where to dump the DB
dir /data/redis

# The empty string forces Redis to log on the standard output.
logfile ""
loglevel notice

# for how to calculate the desired replication timeout value, check
# https://redislabs.com/blog/top-redis-headaches-for-devops-replication-timeouts/
repl-timeout 60

# https://redislabs.com/blog/top-redis-headaches-for-devops-replication-buffer/
# set both the hard and soft limits to 512MB for slave clients.
# The normal and pubsub clients still use the default.
client-output-buffer-limit normal 0 0 0
client-output-buffer-limit slave 512mb 512mb 0
client-output-buffer-limit pubsub 32mb 8mb 60

rename-command FLUSHALL ""
rename-command FLUSHDB ""
rename-command SHUTDOWN ""
rename-command CONFIG "mycfg"

requirepass pass
masterauth pass

# append only file
appendonly yes
appendfilename "appendonly.aof"
appendfsync everysec
%s
`
