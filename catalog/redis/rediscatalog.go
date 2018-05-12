package rediscatalog

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudstax/firecamp/api/catalog"
	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/api/manage"
	"github.com/cloudstax/firecamp/pkg/dns"
	"github.com/cloudstax/firecamp/pkg/utils"
	"github.com/golang/glog"
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

type RedisOptions struct {
	MemoryCacheSizeMB int64 // 0 == no change
	// empty == no change. if auth is enabled, it could not be disabled.
	AuthPass        string
	ReplTimeoutSecs int64  // 0 == no change
	MaxMemPolicy    string // empty == no change
	// nil == no change. to disable config cmd, set empty string
	ConfigCmdName *string
}

// The default Redis catalog service. By default,
// 1) Distribute the nodes on the availability zones.
// 2) Listen on the standard ports, 6379 and 16379.

// ValidateRequest checks if the request is valid
func ValidateRequest(r *catalog.CatalogCreateRedisRequest) error {
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

// ValidateUpdateOptions validates the update options
func ValidateUpdateOptions(r *RedisOptions) error {
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
	service string, res *common.Resources, opts *catalog.CatalogRedisOptions) *manage.CreateServiceRequest {
	// generate service configs
	serviceCfgs := genServiceConfigs(platform, opts)

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
		},

		Resource: &common.Resources{
			MaxCPUUnits:     res.MaxCPUUnits,
			ReserveCPUUnits: res.ReserveCPUUnits,
			MaxMemMB:        res.MaxMemMB,
			ReserveMemMB:    opts.MemoryCacheSizeMB,
		},

		ServiceType:        common.ServiceTypeStateful,
		CatalogServiceType: common.CatalogService_Redis,

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
	return req
}

func genServiceConfigs(platform string, opts *catalog.CatalogRedisOptions) []*manage.ConfigFileContent {
	// create service.conf file

	// for redis cluster, disable auth first as Redis cluster setup tool, redis-trib.rb, does not
	// support auth pass yet. See Redis issue [2866](https://github.com/antirez/redis/issues/2866)
	// and [4288](https://github.com/antirez/redis/pull/4288).
	// After redis cluster is initialized, update service.conf to enable auth.
	enableAuth := "true"
	if IsClusterMode(opts.Shards) {
		enableAuth = "false"
	}

	content := fmt.Sprintf(servicefileContent, platform, opts.Shards, opts.ReplicasPerShard,
		opts.MemoryCacheSizeMB, strconv.FormatBool(opts.DisableAOF), opts.AuthPass, enableAuth,
		opts.ReplTimeoutSecs, opts.MaxMemPolicy, opts.ConfigCmdName)
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
func genReplicaConfigs(platform string, cluster string, service string, azs []string, opts *catalog.CatalogRedisOptions) []*manage.ReplicaConfig {
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

			// static ip is not created yet
			staticip := ""
			content := fmt.Sprintf(memberfileContent, azs[azIndex], memberHost, i, bindIP, staticip, masterMemberHost)
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

// EnableRedisAuth enables the Redis access authentication, after cluster is initialized.
func EnableRedisAuth(content string) string {
	return strings.Replace(content, "ENABLE_AUTH=false", "ENABLE_AUTH=true", 1)
}

// GenDefaultInitTaskRequest returns the default service init task request.
func GenDefaultInitTaskRequest(req *manage.ServiceCommonRequest, shards int64,
	replicasPerShard int64, manageurl string) *manage.RunTaskRequest {
	envkvs := GenInitTaskEnvKVPairs(req.Region, req.Cluster, manageurl, req.ServiceName, shards, replicasPerShard)

	return &manage.RunTaskRequest{
		Service: req,
		Resource: &common.Resources{
			MaxCPUUnits:     common.DefaultMaxCPUUnits,
			ReserveCPUUnits: common.DefaultReserveCPUUnits,
			MaxMemMB:        common.DefaultMaxMemoryMB,
			ReserveMemMB:    common.DefaultReserveMemoryMB,
		},
		ContainerImage: InitContainerImage,
		TaskType:       common.TaskTypeInit,
		Envkvs:         envkvs,
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
	kvsvctype := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_TYPE, Value: common.CatalogService_Redis}
	kvport := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_PORT, Value: strconv.Itoa(listenPort)}
	kvop := &common.EnvKeyValuePair{Name: common.ENV_OP, Value: catalog.CatalogSetServiceInitOp}

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

// SetMemberStaticIP sets the static ip in member.conf
func SetMemberStaticIP(oldContent string, staticIP string) string {
	newstr := fmt.Sprintf("MEMBER_STATIC_IP=%s", staticIP)
	return strings.Replace(oldContent, "MEMBER_STATIC_IP=", newstr, 1)
}

// UpdateServiceConfigs update the redis configs
func UpdateServiceConfigs(oldContent string, opts *RedisOptions) string {
	content := oldContent
	lines := strings.Split(oldContent, "\n")
	for _, line := range lines {
		fields := strings.Split(line, "=")
		switch fields[0] {
		case "AUTH_PASS":
			if opts.AuthPass != "" {
				newstr := fmt.Sprintf("AUTH_PASS=%s", opts.AuthPass)
				content = strings.Replace(content, line, newstr, 1)
			}
		case "MEMORY_CACHE_MB":
			if opts.MemoryCacheSizeMB != 0 {
				newstr := fmt.Sprintf("MEMORY_CACHE_MB=%d", opts.MemoryCacheSizeMB)
				content = strings.Replace(content, line, newstr, 1)
			}

		case "REPL_TIMEOUT_SECS":
			if opts.ReplTimeoutSecs != 0 {
				newstr := fmt.Sprintf("REPL_TIMEOUT_SECS=%d", opts.ReplTimeoutSecs)
				content = strings.Replace(content, line, newstr, 1)
			}

		case "MAX_MEMORY_POLICY":
			if opts.MaxMemPolicy != "" {
				newstr := fmt.Sprintf("MAX_MEMORY_POLICY=%s", opts.MaxMemPolicy)
				content = strings.Replace(content, line, newstr, 1)
			}

		case "CONFIG_CMD_NAME":
			if opts.ConfigCmdName != nil {
				newstr := fmt.Sprintf("CONFIG_CMD_NAME=%s", *opts.ConfigCmdName)
				content = strings.Replace(content, line, newstr, 1)
			}
		}
	}
	return content
}

func ParseServiceConfigs(content string) (*catalog.CatalogRedisOptions, error) {
	opts := &catalog.CatalogRedisOptions{}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		fields := strings.Split(line, "=")
		switch fields[0] {
		case "SHARDS":
			shards, err := strconv.Atoi(fields[1])
			if err != nil {
				glog.Errorln("parse shards error", err, line)
				return nil, err
			}
			opts.Shards = int64(shards)

		case "REPLICAS_PERSHARD":
			replPerShard, err := strconv.Atoi(fields[1])
			if err != nil {
				glog.Errorln("parse replicas pershard error", err, line)
				return nil, err
			}
			opts.ReplicasPerShard = int64(replPerShard)

		case "MEMORY_CACHE_MB":
			cacheSize, err := strconv.Atoi(fields[1])
			if err != nil {
				glog.Errorln("parse memory cache size error", err, line)
				return nil, err
			}
			opts.MemoryCacheSizeMB = int64(cacheSize)

		case "DISABLE_AOF":
			disableAOF, err := strconv.ParseBool(fields[1])
			if err != nil {
				glog.Errorln("parse disable aof error", err, line)
				return nil, err
			}
			opts.DisableAOF = disableAOF

		case "AUTH_PASS":
			opts.AuthPass = fields[1]

		case "REPL_TIMEOUT_SECS":
			replTimeout, err := strconv.Atoi(fields[1])
			if err != nil {
				glog.Errorln("parse replication timeout error", err, line)
				return nil, err
			}
			opts.ReplTimeoutSecs = int64(replTimeout)

		case "MAX_MEMORY_POLICY":
			opts.MaxMemPolicy = fields[1]

		case "CONFIG_CMD_NAME":
			opts.ConfigCmdName = fields[1]
		}
	}
	return opts, nil
}

const (
	servicefileContent = `
PLATFORM=%s
SHARDS=%d
REPLICAS_PERSHARD=%d
MEMORY_CACHE_MB=%d
DISABLE_AOF=%s
AUTH_PASS=%s
ENABLE_AUTH=%s
REPL_TIMEOUT_SECS=%d
MAX_MEMORY_POLICY=%s
CONFIG_CMD_NAME=%s
`

	memberfileContent = `
AVAILABILITY_ZONE=%s
SERVICE_MEMBER=%s
MEMBER_INDEX_INSHARD=%d
BIND_IP=%s
MEMBER_STATIC_IP=%s
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
