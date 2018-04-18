package rediscatalog

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/log"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

const (
	defaultVersion = "4.0"
	// ContainerImage is the main running container.
	ContainerImage = common.ContainerNamePrefix + "redis:" + defaultVersion
	// InitContainerImage initializes the Redis cluster.
	InitContainerImage = common.ContainerNamePrefix + "redis-init:" + defaultVersion

	listenPort  = 6379
	clusterPort = 16379

	minClusterShards = 3
	invalidShards    = 2
	// MinReplTimeoutSecs is the minimal ReplTimeout
	MinReplTimeoutSecs = 60
	shardName          = "shard"

	redisConfFileName = "redis.conf"

	// MaxMemPolicies define the MaxMemory eviction policies
	MaxMemPolicyVolatileLRU    = "volatile-lru"
	MaxMemPolicyAllKeysLRU     = "allkeys-lru"
	MaxMemPolicyVolatileLFU    = "volatile-lfu"
	MaxMemPolicyAllKeysLFU     = "allkeys-lfu"
	MaxMemPolicyVolatileRandom = "volatile-random"
	MaxMemPolicyAllKeysRandom  = "allkeys-random"
	MaxMemPolicyVolatileTTL    = "volatile-ttl"
	MaxMemPolicyNoEviction     = "noeviction"

	envShards           = "SHARDS"
	envReplicasPerShard = "REPLICAS_PERSHARD"
	envMasters          = "REDIS_MASTERS"
	envSlaves           = "REDIS_SLAVES"
	envAuth             = "REDIS_AUTH"
)

// The default Redis catalog service. By default,
// 1) Distribute the nodes on the availability zones.
// 2) Listen on the standard ports, 6379 and 16379.

// ValidateRequest checks if the request is valid
func ValidateRequest(r *manage.CatalogCreateRedisRequest) error {
	if r.Options.ReplicasPerShard < 1 {
		return errors.New("The replicas pershard should not be less than 1")
	}
	if r.Options.Shards == invalidShards {
		return errors.New("Redis cluster mode requires at least 3 shards")
	}
	if r.Resource.MaxMemMB != common.DefaultMaxMemoryMB && r.Resource.MaxMemMB <= r.Options.MemoryCacheSizeMB {
		return errors.New("The container max memory should be larger than Redis memory cache size")
	}
	if r.Options.ReplTimeoutSecs < MinReplTimeoutSecs {
		return fmt.Errorf("The minimal ReplTimeout is %d", MinReplTimeoutSecs)
	}
	if r.Options.MemoryCacheSizeMB <= 0 {
		return errors.New("Please specify the memory cache size")
	}
	return validateMemPolicy(r.Options.MaxMemPolicy)
}

func validateMemPolicy(maxMemPolicy string) error {
	switch maxMemPolicy {
	case MaxMemPolicyAllKeysLRU:
		return nil
	case MaxMemPolicyAllKeysLFU:
		return nil
	case MaxMemPolicyAllKeysRandom:
		return nil
	case MaxMemPolicyVolatileLRU:
		return nil
	case MaxMemPolicyVolatileLFU:
		return nil
	case MaxMemPolicyVolatileRandom:
		return nil
	case MaxMemPolicyVolatileTTL:
		return nil
	case MaxMemPolicyNoEviction:
		return nil
	default:
		return errors.New("Invalid Redis max memory policy")
	}
}

// ValidateUpdateRequest validates the update request
func ValidateUpdateRequest(r *manage.CatalogUpdateRedisRequest) error {
	if r.ReplTimeoutSecs != 0 && r.ReplTimeoutSecs < MinReplTimeoutSecs {
		return fmt.Errorf("The minimal ReplTimeout is %d", MinReplTimeoutSecs)
	}
	if r.MemoryCacheSizeMB < 0 {
		return errors.New("Invalid memory cache size")
	}
	if len(r.MaxMemPolicy) != 0 {
		return validateMemPolicy(r.MaxMemPolicy)
	}
	return nil
}

// GenDefaultCreateServiceRequest returns the default service creation request.
func GenDefaultCreateServiceRequest(platform string, region string, azs []string, cluster string,
	service string, res *common.Resources, opts *manage.CatalogRedisOptions) (*manage.CreateServiceRequest, error) {
	// generate service configs
	serviceCfgs := genServiceConfigs(opts)

	// generate service ReplicaConfigs
	replicaCfgs := genReplicaConfigs(platform, cluster, service, azs, opts)

	portMappings := []common.PortMapping{
		{ContainerPort: listenPort, HostPort: listenPort, IsServicePort: true},
	}
	if opts.Shards >= minClusterShards {
		m := common.PortMapping{ContainerPort: clusterPort, HostPort: clusterPort}
		portMappings = append(portMappings, m)
	}

	req := &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     cluster,
			ServiceName: service,
			ServiceType: common.ServiceTypeStateful,
		},

		Resource: &common.Resources{
			MaxCPUUnits:     res.MaxCPUUnits,
			ReserveCPUUnits: res.ReserveCPUUnits,
			MaxMemMB:        res.MaxMemMB,
			ReserveMemMB:    opts.MemoryCacheSizeMB,
		},

		ContainerImage: ContainerImage,
		Replicas:       opts.ReplicasPerShard * opts.Shards,
		PortMappings:   portMappings,
		RegisterDNS:    true,
		// require to assign a static ip for each member
		RequireStaticIP: true,

		ServiceConfigs: serviceCfgs,

		Volume:        opts.Volume,
		ContainerPath: common.DefaultContainerMountPath,

		ReplicaConfigs: replicaCfgs,
	}
	return req, nil
}

func genServiceConfigs(opts *manage.CatalogRedisOptions) []*manage.ConfigFileContent {
	// create service.conf file
	content := fmt.Sprintf(servicefileContent, opts.Shards, opts.ReplicasPerShard, opts.MemoryCacheSizeMB,
		strconv.FormatBool(opts.DisableAOF), opts.AuthPass, opts.ReplTimeoutSecs, opts.MaxMemPolicy, opts.ConfigCmdName)
	serviceCfg := &manage.ConfigFileContent{
		FileName: catalog.SERVICE_FILE_NAME,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	// create the default redis.conf file
	redisCfg := &manage.ConfigFileContent{
		FileName: redisConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  redisConfigs,
	}

	return []*manage.ConfigFileContent{serviceCfg, redisCfg}
}

// genReplicaConfigs generates the replica configs.
func genReplicaConfigs(platform string, cluster string, service string, azs []string, opts *manage.CatalogRedisOptions) []*manage.ReplicaConfig {
	memBytes := catalog.MBToBytes(opts.MemoryCacheSizeMB)

	domain := dns.GenDefaultDomainName(cluster)

	replicaCfgs := []*manage.ReplicaConfig{}
	for shard := int64(0); shard < opts.Shards; shard++ {
		masterMember := genServiceShardMemberName(service, opts.ReplicasPerShard, shard, 0)
		masterMemberHost := dns.GenDNSName(masterMember, domain)

		for i := int64(0); i < opts.ReplicasPerShard; i++ {
			// distribute the masters to different availability zones and
			// distribute the slaves of one master to different availability zones
			azIndex := int(shard+i) % len(azs)

			// create the member.conf file
			member := genServiceShardMemberName(service, opts.ReplicasPerShard, shard, i)
			memberHost := dns.GenDNSName(member, domain)

			bindIP := memberHost
			if platform == common.ContainerPlatformSwarm {
				bindIP = catalog.BindAllIP
			}

			content := fmt.Sprintf(memberfileContent, azs[azIndex], memberHost, i, bindIP, masterMemberHost)
			memberCfg := &manage.ConfigFileContent{
				FileName: catalog.MEMBER_FILE_NAME,
				FileMode: common.DefaultConfigFileMode,
				Content:  content,
			}

			configs := []*manage.ConfigFileContent{memberCfg}
			replicaCfg := &manage.ReplicaConfig{Zone: azs[azIndex], MemberName: member, Configs: configs}
			replicaCfgs = append(replicaCfgs, replicaCfg)
		}
	}
	return replicaCfgs
}

// shard member name, currently simply service-index, such as service-0.
// ideally the member name would be service-shard0-0. while, currently Redis may adjust the
// master/slaves relationship internally, and assign the shard0's slave to shard1's master.
// see catalog/redis/README.md, redis issue https://github.com/antirez/redis/issues/2462
func genServiceShardMemberName(serviceName string, replicasPerShard int64, shard int64, replicasInShard int64) string {
	return utils.GenServiceMemberName(serviceName, shard*replicasPerShard+replicasInShard)
	//return serviceName + common.NameSeparator + shardName + strconv.FormatInt(shard, 10) + common.NameSeparator + strconv.FormatInt(replicasInShard, 10)
}

// IsClusterMode checks if the service is created with the cluster mode.
func IsClusterMode(shards int64) bool {
	return shards >= minClusterShards
}

// IsRedisConfFile checks if the file is redis.conf
func IsRedisConfFile(filename string) bool {
	return filename == redisConfFileName
}

// NeedToEnableAuth checks whether needs to enable auth in redis.conf
func NeedToEnableAuth(content string) bool {
	// Currently if auth pass is not required, redis.conf will not have #requirepass
	disabledIdx := strings.Index(content, "#requirepass")
	if disabledIdx != -1 {
		return true
	}
	// auth is either not required or already enabled
	return false
}

// EnableRedisAuth enables the Redis access authentication, after cluster is initialized.
func EnableRedisAuth(content string) string {
	content = strings.Replace(content, "#requirepass", "requirepass", 1)
	return strings.Replace(content, "#masterauth", "masterauth", 1)
}

// GenDefaultInitTaskRequest returns the default service init task request.
func GenDefaultInitTaskRequest(req *manage.ServiceCommonRequest, logConfig *cloudlog.LogConfig,
	shards int64, replicasPerShard int64, serviceUUID string, manageurl string) (*containersvc.RunTaskOptions, error) {

	// sanity check
	if !IsClusterMode(shards) {
		return nil, fmt.Errorf("redis service is not cluster mode, shards %d", shards)
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

	taskOpts := &containersvc.RunTaskOptions{
		Common:   commonOpts,
		TaskType: common.TaskTypeInit,
		Envkvs:   envkvs,
	}

	return taskOpts, nil
}

// GenInitTaskEnvKVPairs generates the environment key-values for the init task.
// The init task is only required for the Redis cluster mode.
func GenInitTaskEnvKVPairs(region string, cluster string, manageurl string,
	service string, shards int64, replicasPerShard int64) []*common.EnvKeyValuePair {

	kvregion := &common.EnvKeyValuePair{Name: common.ENV_REGION, Value: region}
	kvcluster := &common.EnvKeyValuePair{Name: common.ENV_CLUSTER, Value: cluster}
	kvmgtserver := &common.EnvKeyValuePair{Name: common.ENV_MANAGE_SERVER_URL, Value: manageurl}
	kvservice := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_NAME, Value: service}
	kvsvctype := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_TYPE, Value: common.CatalogService_Redis}
	kvport := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_PORT, Value: strconv.Itoa(listenPort)}
	kvop := &common.EnvKeyValuePair{Name: common.ENV_OP, Value: manage.CatalogSetRedisInitOp}

	domain := dns.GenDefaultDomainName(cluster)

	masters := ""
	slaves := ""
	for shard := int64(0); shard < shards; shard++ {
		for i := int64(0); i < replicasPerShard; i++ {
			member := genServiceShardMemberName(service, replicasPerShard, shard, i)
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

	envkvs := []*common.EnvKeyValuePair{kvregion, kvcluster, kvmgtserver, kvservice, kvsvctype,
		kvport, kvop, kvshards, kvreplicaspershard, kvmasters, kvslaves}
	return envkvs
}

// IsConfigChanged checks if the config changes.
func IsConfigChanged(ua *common.RedisUserAttr, req *manage.CatalogUpdateRedisRequest) bool {
	return ((req.AuthPass != "" && req.AuthPass != ua.AuthPass) ||
		(req.MemoryCacheSizeMB != 0 && req.MemoryCacheSizeMB != ua.MemoryCacheSizeMB) ||
		(req.ReplTimeoutSecs != 0 && req.ReplTimeoutSecs != ua.ReplTimeoutSecs) ||
		(req.MaxMemPolicy != "" && req.MaxMemPolicy != ua.MaxMemPolicy) ||
		(req.ConfigCmdName != nil && *req.ConfigCmdName != ua.ConfigCmdName))
}

// UpdateRedisUserAttr updates RedisUserAttr
func UpdateRedisUserAttr(ua *common.RedisUserAttr, req *manage.CatalogUpdateRedisRequest) *common.RedisUserAttr {
	newua := db.CopyRedisUserAttr(ua)
	if req.AuthPass != "" && req.AuthPass != ua.AuthPass {
		newua.AuthPass = req.AuthPass
	}
	if req.MemoryCacheSizeMB != 0 && req.MemoryCacheSizeMB != ua.MemoryCacheSizeMB {
		newua.MemoryCacheSizeMB = req.MemoryCacheSizeMB
	}
	if req.ReplTimeoutSecs != 0 && req.ReplTimeoutSecs != ua.ReplTimeoutSecs {
		newua.ReplTimeoutSecs = req.ReplTimeoutSecs
	}
	if req.MaxMemPolicy != "" && req.MaxMemPolicy != ua.MaxMemPolicy {
		newua.MaxMemPolicy = req.MaxMemPolicy
	}
	if req.ConfigCmdName != nil && *req.ConfigCmdName != ua.ConfigCmdName {
		newua.ConfigCmdName = *req.ConfigCmdName
	}
	return newua
}

// UpdateRedisConfig updates the redis content
func UpdateRedisConfig(content string, ua *common.RedisUserAttr, req *manage.CatalogUpdateRedisRequest) string {
	newContent := content
	if req.AuthPass != "" && req.AuthPass != ua.AuthPass {
		authContent := fmt.Sprintf(authConfig, req.AuthPass, req.AuthPass)
		authContent = EnableRedisAuth(authContent)
		if ua.AuthPass == "" {
			// check if the content is already updated. this is possible if the update request
			// is retried as the manage server is restarted.
			idx := strings.Index(newContent, authContent)
			if idx == -1 {
				// auth is not enabled, add authConfig
				newContent += authContent
			}
		} else {
			// auth is enabled, replace the pass
			oldAuthContent := fmt.Sprintf(authConfig, ua.AuthPass, ua.AuthPass)
			oldAuthContent = EnableRedisAuth(oldAuthContent)
			newContent = strings.Replace(newContent, oldAuthContent, authContent, 1)
		}
	}
	if req.MemoryCacheSizeMB != 0 && req.MemoryCacheSizeMB != ua.MemoryCacheSizeMB {
		newMem := fmt.Sprintf("maxmemory %d", catalog.MBToBytes(req.MemoryCacheSizeMB))
		oldMem := fmt.Sprintf("maxmemory %d", catalog.MBToBytes(ua.MemoryCacheSizeMB))
		newContent = strings.Replace(newContent, oldMem, newMem, 1)
	}
	if req.ReplTimeoutSecs != 0 && req.ReplTimeoutSecs != ua.ReplTimeoutSecs {
		newCont := fmt.Sprintf("repl-timeout %d", req.ReplTimeoutSecs)
		oldCont := fmt.Sprintf("repl-timeout %d", ua.ReplTimeoutSecs)
		newContent = strings.Replace(newContent, oldCont, newCont, 1)
	}
	if req.MaxMemPolicy != "" && req.MaxMemPolicy != ua.MaxMemPolicy {
		newPolicy := fmt.Sprintf("maxmemory-policy %s", req.MaxMemPolicy)
		oldPolicy := fmt.Sprintf("maxmemory-policy %s", ua.MaxMemPolicy)
		newContent = strings.Replace(newContent, oldPolicy, newPolicy, 1)
	}
	if req.ConfigCmdName != nil && *req.ConfigCmdName != ua.ConfigCmdName {
		newCfgCmd := fmt.Sprintf("rename-command CONFIG \"%s\"", *req.ConfigCmdName)
		oldCfgCmd := fmt.Sprintf("rename-command CONFIG \"%s\"", ua.ConfigCmdName)
		if ua.ConfigCmdName == "" {
			oldCfgCmd = "rename-command CONFIG \"\""
		}
		newContent = strings.Replace(newContent, oldCfgCmd, newCfgCmd, 1)
	}
	return newContent
}

const (
	servicefileContent = `
SHARDS=%d
REPLICAS_PERSHARD=%d
MEMORY_CACHE_MB=%d
DISABLE_AOF=%s
AUTH_PASS=%s
REPL_TIMEOUT_SECS=%d
MAX_MEMORY_POLICY=%s
CONFIG_CMD_NAME=%s
`

	memberfileContent = `
AVAILABILITY_ZONE=%s
SERVICE_MEMBER=%s
MEMBER_INDEX_INSHARD=%d
BIND_IP=%s
MASTER_MEMBER=%s
`

	redisConfigs = `
bind 0.0.0.0
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
rename-command CONFIG ""
`
)
