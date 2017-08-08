package rediscatalog

import (
	"fmt"
	"strconv"

	"github.com/cloudstax/openmanage/catalog"
	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/manage"
	"github.com/cloudstax/openmanage/utils"
)

const (
	// ContainerImage is the main running container.
	ContainerImage = common.ContainerNamePrefix + "redis:" + common.Version

	listenPort  = 6379
	clusterPort = 16379

	minClusterShards                 = 3
	invalidShards                    = 2
	minReplTimeoutSecs               = 60
	defaultSlaveClientOutputBufferMB = 512
	shardName                        = "shard"

	redisConfFileName = "redis.conf"

	maxMemPolicyVolatileLRU    = "volatile-lru"
	maxMemPolicyAllKeysLRU     = "allkeys-lru"
	maxMemPolicyVolatileLFU    = "volatile-lfu"
	maxMemPolicyAllKeysLFU     = "allkeys-lfu"
	maxMemPolicyVolatileRandom = "volatile-random"
	maxMemPolicyAllKeysRandom  = "allkeys-random"
	maxMemPolicyVolatileTTL    = "volatile-ttl"
	maxMemPolicyNoEviction     = "noeviction"
)

// The default Redis catalog service. By default,
// 1) Distribute the nodes on the availability zones.
// 2) Listen on the standard ports, 6379 and 16379.

// ValidateRequest checks if the request is valid
func ValidateRequest(r *manage.CatalogCreateRedisRequest) error {
	return validateRequest(r.Shards, r.ReplicasPerShard, r.Resource.MaxMemMB, r.MaxMemPolicy)
}

func validateRequest(shards int64, replicasPerShard int64, maxMemMB int64, maxMemPolicy string) error {
	if replicasPerShard < 1 || shards == invalidShards ||
		maxMemMB == common.DefaultMaxMemoryMB ||
		(shards != 1 && maxMemMB < defaultSlaveClientOutputBufferMB) {
		return common.ErrInvalidArgs
	}
	switch maxMemPolicy {
	case "":
		return nil
	case maxMemPolicyAllKeysLRU:
		return nil
	case maxMemPolicyAllKeysLFU:
		return nil
	case maxMemPolicyAllKeysRandom:
		return nil
	case maxMemPolicyVolatileLRU:
		return nil
	case maxMemPolicyVolatileLFU:
		return nil
	case maxMemPolicyVolatileRandom:
		return nil
	case maxMemPolicyVolatileTTL:
		return nil
	case maxMemPolicyNoEviction:
		return nil
	default:
		return common.ErrInvalidArgs
	}
}

// GenDefaultCreateServiceRequest returns the default service creation request.
func GenDefaultCreateServiceRequest(region string, azs []string, cluster string,
	service string, res *common.Resources, shards int64, replicasPerShard int64, volSizeGB int64,
	disableAOF bool, authPass string, replTimeoutSecs int64, maxMemPolicy string) *manage.CreateServiceRequest {
	// generate service ReplicaConfigs
	replicaCfgs := GenReplicaConfigs(cluster, service, azs, res.MaxMemMB, maxMemPolicy,
		shards, replicasPerShard, disableAOF, authPass, replTimeoutSecs)

	portMappings := []common.PortMapping{
		{ContainerPort: listenPort, HostPort: listenPort},
	}
	if shards >= minClusterShards {
		m := common.PortMapping{ContainerPort: clusterPort, HostPort: clusterPort}
		portMappings = append(portMappings, m)
	}

	return &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     cluster,
			ServiceName: service,
		},

		Resource: res,

		ContainerImage: ContainerImage,
		Replicas:       replicasPerShard * shards,
		VolumeSizeGB:   volSizeGB,
		ContainerPath:  common.DefaultContainerMountPath,
		PortMappings:   portMappings,

		RegisterDNS:    true,
		ReplicaConfigs: replicaCfgs,
	}
}

// GenReplicaConfigs generates the replica configs.
func GenReplicaConfigs(cluster string, service string, azs []string, maxMemMB int64, maxMemPolicy string,
	shards int64, replicasPerShard int64, disableAOF bool, authPass string, replTimeoutSecs int64) []*manage.ReplicaConfig {
	// adjust the replTimeoutSecs if needed
	if replTimeoutSecs > minReplTimeoutSecs {
		replTimeoutSecs = minReplTimeoutSecs
	}
	if len(maxMemPolicy) == 0 {
		maxMemPolicy = maxMemPolicyNoEviction
	}

	redisMaxMemBytes := maxMemMB * 1024 * 1024
	if replicasPerShard != 1 {
		// replication mode needs to reserve some memory for the output buffer to slave
		redisMaxMemBytes -= defaultSlaveClientOutputBufferMB * 1024 * 1024
	}

	domain := dns.GenDefaultDomainName(cluster)

	replicaCfgs := make([]*manage.ReplicaConfig, replicasPerShard*shards)
	for shard := int64(0); shard < shards; shard++ {
		for i := int64(0); i < replicasPerShard; i++ {
			// create the sys.conf file
			member := genServiceShardMemberName(service, shard, i)
			memberHost := dns.GenDNSName(member, domain)
			sysCfg := catalog.CreateSysConfigFile(memberHost)

			// create the redis.conf file
			redisContent := fmt.Sprintf(redisConfigs, memberHost, listenPort, redisMaxMemBytes, maxMemPolicy, replTimeoutSecs)
			if len(authPass) != 0 {
				redisContent += fmt.Sprintf(authConfig, authPass, authPass)
			}
			if !disableAOF {
				redisContent += aofConfigs
			}
			if shards != 1 {
				redisContent += clusterConfigs
			}

			redisContent += defaultConfigs

			redisCfg := &manage.ReplicaConfigFile{
				FileName: redisConfFileName,
				FileMode: common.DefaultConfigFileMode,
				Content:  redisContent,
			}

			configs := []*manage.ReplicaConfigFile{sysCfg, redisCfg}

			// distribute the replicas of one shard to different availability zones
			azIndex := int(i) % len(azs)
			replicaCfg := &manage.ReplicaConfig{Zone: azs[azIndex], Configs: configs}

			replicaCfgs[replicasPerShard*shard+i] = replicaCfg
		}
	}
	return replicaCfgs
}

// shard member dns name example: service-shard0-0
func genServiceShardMemberName(serviceName string, shard int64, replicasInShard int64) string {
	shardMemberName := serviceName + common.NameSeparator + shardName + strconv.FormatInt(shard, 10)
	return utils.GenServiceMemberName(shardMemberName, replicasInShard)
}

const (
	redisConfigs = `
bind %s
port %d

# Redis memory cache size in bytes. The total memory will be like maxmemory + slave output buffer.
maxmemory %d
maxmemory-policy %s

# The filename where to dump the DB
dbfilename dump.rdb
# The directory where to dump the DB
dir /data/redis

# The empty string forces Redis to log on the standard output.
logfile ""
loglevel notice

# for how to calculate the desired replication timeout value, check
# https://redislabs.com/blog/top-redis-headaches-for-devops-replication-timeouts/
repl-timeout %d

# https://redislabs.com/blog/top-redis-headaches-for-devops-replication-buffer/
# set both the hard and soft limits to 512MB for slave clients.
# The normal and pubsub clients still use the default.
client-output-buffer-limit normal 0 0 0
client-output-buffer-limit slave 512mb 512mb 0
client-output-buffer-limit pubsub 32mb 8mb 60
`

	authConfig = `
requirepass %s
masterauth %s
`

	aofConfigs = `
# append only file
appendonly yes
appendfilename "appendonly.aof"
appendfsync everysec
`

	clusterConfigs = `
cluster-enabled yes
cluster-config-file /data/redis-cluster.conf
cluster-node-timeout 15000
cluster-slave-validity-factor 10
cluster-migration-barrier 1
cluster-require-full-coverage yes
`

	defaultConfigs = `
# Default configs

protected-mode yes
# TCP listen() backlog.
#
# In high requests-per-second environments you need an high backlog in order
# to avoid slow clients connections issues. Note that the Linux kernel
# will silently truncate it to the value of /proc/sys/net/core/somaxconn so
# make sure to raise both the value of somaxconn and tcp_max_syn_backlog
# in order to get the desired effect.
tcp-backlog 511

# Close the connection after a client is idle for N seconds (0 to disable)
timeout 0

tcp-keepalive 300

daemonize no
supervised no
pidfile /var/run/redis_6379.pid

databases 16

always-show-logo yes

# Save the DB on disk:
#   save <seconds> <changes>
save 900 1
save 300 10
save 60 10000

stop-writes-on-bgsave-error yes
rdbcompression yes
rdbchecksum yes

slave-serve-stale-data yes
slave-read-only yes

# Replication SYNC strategy: disk or socket.
# WARNING: DISKLESS REPLICATION IS EXPERIMENTAL CURRENTLY
repl-diskless-sync no
repl-diskless-sync-delay 5
repl-disable-tcp-nodelay no
# repl-backlog-size 1mb
# repl-backlog-ttl 3600
slave-priority 100
# slave-announce-ip 5.5.5.5
# slave-announce-port 1234


lazyfree-lazy-eviction no
lazyfree-lazy-expire no
lazyfree-lazy-server-del no
slave-lazy-flush no

no-appendfsync-on-rewrite no
auto-aof-rewrite-percentage 100
auto-aof-rewrite-min-size 64mb
aof-load-truncated yes
aof-use-rdb-preamble no

lua-time-limit 5000

slowlog-log-slower-than 10000
slowlog-max-len 128

# Latency monitoring can easily be enabled at runtime using the command
# "CONFIG SET latency-monitor-threshold <milliseconds>" if needed.
latency-monitor-threshold 0

notify-keyspace-events ""

hash-max-ziplist-entries 512
hash-max-ziplist-value 64
list-max-ziplist-size -2
list-compress-depth 0
set-max-intset-entries 512
zset-max-ziplist-entries 128
zset-max-ziplist-value 64
hll-sparse-max-bytes 3000
activerehashing yes
hz 10
aof-rewrite-incremental-fsync yes
`
)
