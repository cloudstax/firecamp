package rediscatalog

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudstax/openmanage/manage"
)

func TestRedisCatalogFuncs(t *testing.T) {
	cluster := "c1"
	service := "service1"
	azs := []string{"az1", "az2", "az3"}
	maxMemMB := int64(1024)
	opts := &manage.CatalogRedisOptions{
		Shards:           int64(3),
		ReplicasPerShard: int64(2),
		VolumeSizeGB:     int64(1),
		DisableAOF:       false,
		AuthPass:         "pass",
		ReplTimeoutSecs:  int64(30),
		MaxMemPolicy:     "",
		ConfigCmdName:    "",
	}

	// test 3 shards and 2 replicas each shard
	replcfgs := GenReplicaConfigs(cluster, service, azs, maxMemMB, opts)
	if len(replcfgs) != 6 {
		t.Fatalf("expect 6 replica configs, get %d", len(replcfgs))
	}

	if replcfgs[0].Zone != azs[0] || replcfgs[1].Zone != azs[1] ||
		replcfgs[2].Zone != azs[0] || replcfgs[3].Zone != azs[1] ||
		replcfgs[4].Zone != azs[0] || replcfgs[5].Zone != azs[1] {
		t.Fatalf("replica configs zone mismatch, %s", replcfgs)
	}

	content := strings.TrimSuffix(replcfgs[0].Configs[1].Content, defaultConfigs)
	//content1 := fmt.Sprintf(redisConfigs1, "service1-shard0-0.c1-openmanage.com")
	content1 := fmt.Sprintf(redisConfigs1, "service1-0.c1-openmanage.com")
	if content != content1 {
		t.Fatalf("redis conf content mismatch, %s \nexpect %s", content, content1)
	}

	// test 4 shards and 3 replicas each shard
	opts.Shards = 4
	opts.ReplicasPerShard = 3
	replcfgs = GenReplicaConfigs(cluster, service, azs, maxMemMB, opts)
	if len(replcfgs) != 12 {
		t.Fatalf("expect 12 replica configs, get %d", len(replcfgs))
	}

	if replcfgs[0].Zone != azs[0] || replcfgs[1].Zone != azs[1] || replcfgs[2].Zone != azs[2] ||
		replcfgs[3].Zone != azs[0] || replcfgs[4].Zone != azs[1] || replcfgs[5].Zone != azs[2] ||
		replcfgs[6].Zone != azs[0] || replcfgs[7].Zone != azs[1] || replcfgs[8].Zone != azs[2] ||
		replcfgs[9].Zone != azs[0] || replcfgs[10].Zone != azs[1] || replcfgs[11].Zone != azs[2] {
		t.Fatalf("replica configs zone mismatch, %s", replcfgs)
	}

	content = strings.TrimSuffix(replcfgs[1].Configs[1].Content, defaultConfigs)
	//content1 = fmt.Sprintf(redisConfigs1, "service1-shard0-1.c1-openmanage.com")
	content1 = fmt.Sprintf(redisConfigs1, "service1-1.c1-openmanage.com")
	if content != content1 {
		t.Fatalf("redis conf content mismatch, %s \nexpect\n %s", content, content1)
	}
	content = strings.TrimSuffix(replcfgs[4].Configs[1].Content, defaultConfigs)
	//content1 = fmt.Sprintf(redisConfigs1, "service1-shard1-1.c1-openmanage.com")
	content1 = fmt.Sprintf(redisConfigs1, "service1-4.c1-openmanage.com")
	if content != content1 {
		t.Fatalf("redis conf content mismatch, %s \nexpect\n %s", content, content1)
	}

	// test 1 shards and 3 replicas each shard
	opts.Shards = 1
	opts.ReplicasPerShard = 3
	replcfgs = GenReplicaConfigs(cluster, service, azs, maxMemMB, opts)
	if len(replcfgs) != 3 {
		t.Fatalf("expect 3 replica configs, get %d", len(replcfgs))
	}

	if replcfgs[0].Zone != azs[0] || replcfgs[1].Zone != azs[1] || replcfgs[2].Zone != azs[2] {
		t.Fatalf("replica configs zone mismatch, %s", replcfgs)
	}

	content = strings.TrimSuffix(replcfgs[1].Configs[1].Content, defaultConfigs)
	//content1 = fmt.Sprintf(redisConfigs2, "service1-shard0-1.c1-openmanage.com", 536870912)
	content1 = fmt.Sprintf(redisConfigs2, "service1-1.c1-openmanage.com", 536870912, "\nslaveof service1-0.c1-openmanage.com 6379")
	if content != content1 {
		t.Fatalf("redis conf content mismatch, %s \nexpect\n %s", content, content1)
	}

	// test 1 shards and 1 replicas each shard
	opts.Shards = 1
	opts.ReplicasPerShard = 1
	replcfgs = GenReplicaConfigs(cluster, service, azs, maxMemMB, opts)
	if len(replcfgs) != 1 {
		t.Fatalf("expect 1 replica configs, get %d", len(replcfgs))
	}

	if replcfgs[0].Zone != azs[0] {
		t.Fatalf("replica configs zone mismatch, %s", replcfgs)
	}

	content = strings.TrimSuffix(replcfgs[0].Configs[1].Content, defaultConfigs)
	content += "\n"
	//content1 = fmt.Sprintf(redisConfigs2, "service1-shard0-0.c1-openmanage.com", 1073741824)
	content1 = fmt.Sprintf(redisConfigs2, "service1-0.c1-openmanage.com", 1073741824, "")
	if content != content1 {
		t.Fatalf("redis conf content mismatch, %s \nexpect\n %s", content, content1)
	}

}

const redisConfigs1 = `
bind %s
port 6379

# Redis memory cache size in bytes. The total memory will be like maxmemory + slave output buffer.
maxmemory 536870912
maxmemory-policy noeviction

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
rename-command CONFIG ""

requirepass pass
masterauth pass

# append only file
appendonly yes
appendfilename "appendonly.aof"
appendfsync everysec

cluster-enabled yes
cluster-config-file /data/redis-cluster.conf
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
maxmemory-policy noeviction

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
rename-command CONFIG ""

requirepass pass
masterauth pass

# append only file
appendonly yes
appendfilename "appendonly.aof"
appendfsync everysec
%s
`
