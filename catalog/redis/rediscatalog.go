package rediscatalog

import (
	"fmt"
	"strconv"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/log"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

const (
	// ContainerImage is the main running container.
	ContainerImage = common.ContainerNamePrefix + "redis:" + common.Version
	// InitContainerImage initializes the Redis cluster.
	InitContainerImage = common.ContainerNamePrefix + "redis-init:" + common.Version

	listenPort  = 6379
	clusterPort = 16379

	minClusterShards                 = 3
	invalidShards                    = 2
	minReplTimeoutSecs               = 60
	defaultSlaveClientOutputBufferMB = 512
	shardName                        = "shard"

	redisConfFileName        = "redis.conf"
	redisClusterInfoFileName = "cluster.info"

	maxMemPolicyVolatileLRU    = "volatile-lru"
	maxMemPolicyAllKeysLRU     = "allkeys-lru"
	maxMemPolicyVolatileLFU    = "volatile-lfu"
	maxMemPolicyAllKeysLFU     = "allkeys-lfu"
	maxMemPolicyVolatileRandom = "volatile-random"
	maxMemPolicyAllKeysRandom  = "allkeys-random"
	maxMemPolicyVolatileTTL    = "volatile-ttl"
	maxMemPolicyNoEviction     = "noeviction"

	envShards           = "SHARDS"
	envReplicasPerShard = "REPLICAS_PERSHARD"
	envMasters          = "REDIS_MASTERS"
	envSlaves           = "REDIS_SLAVES"
)

// The default Redis catalog service. By default,
// 1) Distribute the nodes on the availability zones.
// 2) Listen on the standard ports, 6379 and 16379.

// ValidateRequest checks if the request is valid
func ValidateRequest(r *manage.CatalogCreateRedisRequest) error {
	return validateRequest(r.Resource.MaxMemMB, r.Options)
}

func validateRequest(maxMemMB int64, opts *manage.CatalogRedisOptions) error {
	if opts.ReplicasPerShard < 1 || opts.Shards == invalidShards ||
		maxMemMB == common.DefaultMaxMemoryMB ||
		(opts.Shards != 1 && maxMemMB <= defaultSlaveClientOutputBufferMB) {
		return common.ErrInvalidArgs
	}
	switch opts.MaxMemPolicy {
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
func GenDefaultCreateServiceRequest(platform string, region string, azs []string, cluster string,
	service string, res *common.Resources, opts *manage.CatalogRedisOptions) *manage.CreateServiceRequest {
	// generate service ReplicaConfigs
	replicaCfgs := GenReplicaConfigs(platform, cluster, service, azs, res.MaxMemMB, opts)

	portMappings := []common.PortMapping{
		{ContainerPort: listenPort, HostPort: listenPort},
	}
	if opts.Shards >= minClusterShards {
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
		Replicas:       opts.ReplicasPerShard * opts.Shards,
		VolumeSizeGB:   opts.VolumeSizeGB,
		ContainerPath:  common.DefaultContainerMountPath,
		PortMappings:   portMappings,

		RegisterDNS: true,
		// require to assign a static ip for each member
		RequireStaticIP: true,
		ReplicaConfigs:  replicaCfgs,
	}
}

// GenReplicaConfigs generates the replica configs.
func GenReplicaConfigs(platform string, cluster string, service string, azs []string, maxMemMB int64,
	opts *manage.CatalogRedisOptions) []*manage.ReplicaConfig {
	// adjust the replTimeoutSecs if needed
	replTimeoutSecs := opts.ReplTimeoutSecs
	if replTimeoutSecs < minReplTimeoutSecs {
		replTimeoutSecs = minReplTimeoutSecs
	}
	maxMemPolicy := opts.MaxMemPolicy
	if len(maxMemPolicy) == 0 {
		maxMemPolicy = maxMemPolicyNoEviction
	}
	redisMaxMemBytes := maxMemMB * 1024 * 1024
	if opts.ReplicasPerShard != 1 {
		// replication mode needs to reserve some memory for the output buffer to slave
		redisMaxMemBytes -= defaultSlaveClientOutputBufferMB * 1024 * 1024
	}

	domain := dns.GenDefaultDomainName(cluster)

	replicaCfgs := make([]*manage.ReplicaConfig, opts.ReplicasPerShard*opts.Shards)
	for shard := int64(0); shard < opts.Shards; shard++ {
		masterMember := utils.GenServiceMemberName(service, shard*opts.ReplicasPerShard)
		masterMemberHost := dns.GenDNSName(masterMember, domain)

		for i := int64(0); i < opts.ReplicasPerShard; i++ {
			// create the sys.conf file
			// TODO enable custom shard member name
			//member := genServiceShardMemberName(service, shard, i)
			member := utils.GenServiceMemberName(service, shard*opts.ReplicasPerShard+i)
			memberHost := dns.GenDNSName(member, domain)
			sysCfg := catalog.CreateSysConfigFile(platform, memberHost)

			// create the redis.conf file
			redisContent := fmt.Sprintf(redisConfigs, memberHost, listenPort,
				redisMaxMemBytes, maxMemPolicy, replTimeoutSecs, opts.ConfigCmdName)
			if len(opts.AuthPass) != 0 {
				redisContent += fmt.Sprintf(authConfig, opts.AuthPass, opts.AuthPass)
			}
			if !opts.DisableAOF {
				redisContent += aofConfigs
			}
			if opts.Shards == 1 {
				// master-slave mode or single instance mode
				if opts.ReplicasPerShard != 1 && i != int64(0) {
					// master-slave mode, set the master dnsname for the slave
					redisContent += fmt.Sprintf(replConfigs, masterMemberHost, listenPort)
				}
			} else {
				// cluster mode
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

			replicaCfgs[opts.ReplicasPerShard*shard+i] = replicaCfg
		}
	}
	return replicaCfgs
}

// shard member dns name example: service-shard0-0
func genServiceShardMemberName(serviceName string, shard int64, replicasInShard int64) string {
	shardMemberName := serviceName + common.NameSeparator + shardName + strconv.FormatInt(shard, 10)
	return utils.GenServiceMemberName(shardMemberName, replicasInShard)
}

// IsClusterInfoFile checks if the file is the cluster info file
func IsClusterInfoFile(filename string) bool {
	return filename == redisClusterInfoFileName
}

// IsClusterMode checks if the service is created with the cluster mode.
func IsClusterMode(shards int64) bool {
	return shards >= minClusterShards
}

// GenDefaultInitTaskRequest returns the default service init task request.
func GenDefaultInitTaskRequest(req *manage.ServiceCommonRequest, shards int64, replicasPerShard int64,
	logConfig *cloudlog.LogConfig, serviceUUID string, manageurl string) *containersvc.RunTaskOptions {

	// sanity check
	if !IsClusterMode(shards) {
		return nil
	}

	envkvs := GenInitTaskEnvKVPairs(req.Region, req.Cluster, manageurl, req.ServiceName, shards, replicasPerShard)

	commonOpts := &containersvc.CommonOptions{
		Cluster:        req.Cluster,
		ServiceName:    req.ServiceName,
		ServiceUUID:    serviceUUID,
		ContainerImage: InitContainerImage,
		Resource: &common.Resources{
			MaxCPUUnits:     common.DefaultMaxCPUUnits,
			ReserveCPUUnits: common.DefaultReserveCPUUnits,
			MaxMemMB:        common.DefaultMaxMemoryMB,
			ReserveMemMB:    common.DefaultReserveMemoryMB,
		},
		LogConfig: logConfig,
	}

	return &containersvc.RunTaskOptions{
		Common:   commonOpts,
		TaskType: common.TaskTypeInit,
		Envkvs:   envkvs,
	}
}

// GenInitTaskEnvKVPairs generates the environment key-values for the init task.
// The init task is only required for the Redis cluster mode.
func GenInitTaskEnvKVPairs(region string, cluster string, manageurl string,
	service string, shards int64, replicasPerShard int64) []*common.EnvKeyValuePair {

	kvregion := &common.EnvKeyValuePair{Name: common.ENV_REGION, Value: region}
	kvcluster := &common.EnvKeyValuePair{Name: common.ENV_CLUSTER, Value: cluster}
	kvmgtserver := &common.EnvKeyValuePair{Name: common.ENV_MANAGE_SERVER_URL, Value: manageurl}
	kvservice := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_NAME, Value: service}
	kvport := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_PORT, Value: strconv.Itoa(listenPort)}
	kvop := &common.EnvKeyValuePair{Name: common.ENV_OP, Value: manage.CatalogSetRedisInitOp}

	domain := dns.GenDefaultDomainName(cluster)

	masters := ""
	slaves := ""
	for shard := int64(0); shard < shards; shard++ {
		for i := int64(0); i < replicasPerShard; i++ {
			//member := genServiceShardMemberName(service, shard, i)
			member := utils.GenServiceMemberName(service, shard*replicasPerShard+i)
			memberHost := dns.GenDNSName(member, domain)

			if i == int64(0) {
				// the first member in one shard is the master
				if len(masters) == 0 {
					masters += memberHost
				} else {
					masters += common.ENV_VALUE_SEPARATOR + memberHost
				}
			} else {
				// slave member
				if len(slaves) == 0 {
					slaves += memberHost
				} else {
					slaves += common.ENV_VALUE_SEPARATOR + memberHost
				}
			}
		}
	}

	kvshards := &common.EnvKeyValuePair{Name: envShards, Value: strconv.FormatInt(shards, 10)}
	kvreplicaspershard := &common.EnvKeyValuePair{Name: envReplicasPerShard, Value: strconv.FormatInt(replicasPerShard, 10)}
	kvmasters := &common.EnvKeyValuePair{Name: envMasters, Value: masters}
	kvslaves := &common.EnvKeyValuePair{Name: envSlaves, Value: slaves}

	envkvs := []*common.EnvKeyValuePair{kvregion, kvcluster, kvmgtserver, kvservice,
		kvport, kvop, kvshards, kvreplicaspershard, kvmasters, kvslaves}
	return envkvs
}

// CreateClusterInfoFile returns the ReplicaConfigFile for the cluster.info file.
func CreateClusterInfoFile(nodeIDs []string) *manage.ReplicaConfigFile {
	content := ""
	for _, node := range nodeIDs {
		content += fmt.Sprintf("%s\n", node)
	}
	return &manage.ReplicaConfigFile{
		FileName: redisClusterInfoFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}
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

rename-command FLUSHALL ""
rename-command FLUSHDB ""
rename-command SHUTDOWN ""
rename-command CONFIG "%s"
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

	replConfigs = `
slaveof %s %d
`

	clusterConfigs = `
cluster-enabled yes
cluster-config-file /data/redis-node.conf
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
