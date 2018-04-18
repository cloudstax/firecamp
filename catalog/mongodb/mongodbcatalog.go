package mongodbcatalog

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/log"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

const (
	defaultVersion = "3.4"
	// ContainerImage is the main MongoDB running container.
	ContainerImage = common.ContainerNamePrefix + "mongodb:" + defaultVersion
	// InitContainerImage initializes the MongoDB ReplicaSet.
	InitContainerImage = common.ContainerNamePrefix + "mongodb-init:" + defaultVersion

	maxMembers = 50

	mongoPort        = 27017
	shardPort        = 27018
	configServerPort = 27019

	keyfileName      = "keyfile"
	keyfileRandBytes = 200
	keyfileMode      = 0400

	configServerName = "config"
	shardName        = "shard"

	emptyRole  = ""
	configRole = "configsvr"
	shardRole  = "shardsvr"

	envReplicaSetOnly      = "REPLICASET_ONLY"
	envConfigServerMembers = "CONFIG_SERVER_MEMBERS"
	envShardMembers        = "SHARD_MEMBERS"
)

// The default MongoDB ReplicaSet catalog service. By default,
// 1) One MongoDB ReplicaSet has 1 primary and 2 secondary replicas across 3 availability zones.
// 2) Listen on the standard port, 27017.
// 3) The ReplicaSetName is the service name.

// ValidateRequest checks if the request is valid
func ValidateRequest(req *manage.CatalogCreateMongoDBRequest) error {
	if req.Options.JournalVolume == nil {
		return errors.New("mongodb should have separate volume for journal")
	}
	if !req.Options.ReplicaSetOnly && (req.Options.ConfigServers <= 0 || req.Options.ConfigServers%2 != 1) {
		return errors.New("only allow odd number of config servers")
	}
	if req.Options.ReplicasPerShard%2 != 1 {
		return errors.New("only allow odd number of members in one replica set")
	}
	if req.Options.ReplicasPerShard > maxMembers || req.Options.ConfigServers > maxMembers {
		return errors.New("one replica set or config server can not have more than 50 members")
	}

	return nil
}

// GenDefaultCreateServiceRequest returns the default MongoDB ReplicaSet creation request.
func GenDefaultCreateServiceRequest(platform string, region string, azs []string, cluster string,
	service string, keyfileContent string, opts *manage.CatalogMongoDBOptions, res *common.Resources) *manage.CreateServiceRequest {

	serviceCfgs := genServiceConfigs(platform, res.MaxMemMB, opts, keyfileContent)

	replicaCfgs := genReplicaConfigs(platform, azs, cluster, service, res.MaxMemMB, opts)

	portmapping := common.PortMapping{
		ContainerPort: mongoPort,
		HostPort:      mongoPort,
		IsServicePort: true,
	}
	portmaps := []common.PortMapping{portmapping}
	if opts.Shards != 1 || !opts.ReplicaSetOnly {
		shardportmap := common.PortMapping{ContainerPort: shardPort, HostPort: shardPort, IsServicePort: true}
		configportmap := common.PortMapping{ContainerPort: configServerPort, HostPort: configServerPort, IsServicePort: true}
		portmaps = append(portmaps, shardportmap, configportmap)
	}

	req := &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     cluster,
			ServiceName: service,
			ServiceType: common.ServiceTypeStateful,
		},

		Resource: res,

		CatalogServiceType: common.CatalogService_MongoDB,

		ContainerImage: ContainerImage,
		Replicas:       int64(len(replicaCfgs)),
		PortMappings:   portmaps,
		RegisterDNS:    true,

		ServiceConfigs: serviceCfgs,

		Volume:               opts.Volume,
		ContainerPath:        common.DefaultContainerMountPath,
		JournalVolume:        opts.JournalVolume,
		JournalContainerPath: common.DefaultJournalVolumeContainerMountPath,

		ReplicaConfigs: replicaCfgs,
	}
	return req
}

// GenDefaultInitTaskRequest returns the default MongoDB ReplicaSet init task request.
func GenDefaultInitTaskRequest(req *manage.ServiceCommonRequest, logConfig *cloudlog.LogConfig,
	serviceUUID string, manageurl string, opts *manage.CatalogMongoDBOptions) *containersvc.RunTaskOptions {

	envkvs := GenInitTaskEnvKVPairs(req.Region, req.Cluster, req.ServiceName, manageurl, opts)

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

// genServiceConfigs generates the service configs.
func genServiceConfigs(platform string, maxMemMB int64, opts *manage.CatalogMongoDBOptions, keyfileContent string) []*manage.ConfigFileContent {
	// create the service.conf file
	content := fmt.Sprintf(servicefileContent, platform, opts.Shards, opts.ReplicasPerShard, opts.ReplicaSetOnly, opts.ConfigServers)
	serviceCfg := &manage.ConfigFileContent{
		FileName: catalog.SERVICE_FILE_NAME,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	// create the key file
	keyfileCfg := &manage.ConfigFileContent{
		FileName: keyfileName,
		FileMode: keyfileMode,
		Content:  keyfileContent,
	}

	// create the default mongod.conf file
	// docker entrypoint script will update the corresponding fields for the service member
	mongodCfg := createMongodConf(maxMemMB)

	configs := []*manage.ConfigFileContent{serviceCfg, keyfileCfg, mongodCfg}
	return configs
}

func createMongodConf(maxMemMB int64) *manage.ConfigFileContent {
	// create the mongod.conf file
	content := mongoDBConfHead

	if maxMemMB == common.DefaultMaxMemoryMB {
		// no max memory limit, mongodb will compute the cache size based on the node's memory.
		content += mongoDBConfStorage
	} else {
		// max memory is limited. compute cache size, max(50% of max memory - 1GB, 256MB).
		// https://docs.mongodb.com/manual/reference/configuration-options/#storage-options
		defaultCacheSizeMB := int64(256)
		cacheSizeMB := maxMemMB/2 - 1024
		if cacheSizeMB < defaultCacheSizeMB {
			cacheSizeMB = defaultCacheSizeMB
		}
		// align cache size to 256MB
		cacheSizeGB := float64(cacheSizeMB/defaultCacheSizeMB) * 0.25
		cacheContent := fmt.Sprintf(mongoDBConfCache, cacheSizeGB)

		content += mongoDBConfStorage + cacheContent
	}

	content += mongoDBConfNetwork + mongoDBConfRepl + mongoDBConfEnd

	return &manage.ConfigFileContent{
		FileName: mongoDBConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}
}

// genReplicaConfigs generates the replica configs.
// Note: if the number of availability zones is less than replicas, 2 or more replicas will run on the same zone.
func genReplicaConfigs(platform string, azs []string, cluster string, service string, maxMemMB int64, opts *manage.CatalogMongoDBOptions) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)
	replicaCfgs := []*manage.ReplicaConfig{}

	if opts.Shards == 1 && opts.ReplicaSetOnly {
		// single shard and ReplicaSetOnly, create a replica set only
		for i := int64(0); i < opts.ReplicasPerShard; i++ {
			replSetName := service
			member := utils.GenServiceMemberName(service, i)
			index := int(i) % len(azs)
			az := azs[index]
			replicaCfg := genReplicaConfig(platform, domain, member, replSetName, emptyRole, az)
			replicaCfgs = append(replicaCfgs, replicaCfg)
		}

		return replicaCfgs
	}

	// create a sharded cluster.

	// generate the Config Server replica configs
	for i := int64(0); i < opts.ConfigServers; i++ {
		configServerName := getConfigServerName(service)
		member := utils.GenServiceMemberName(configServerName, i)
		index := int(i) % len(azs)
		az := azs[index]
		replicaCfg := genReplicaConfig(platform, domain, member, configServerName, configRole, az)
		replicaCfgs = append(replicaCfgs, replicaCfg)
	}

	// generate the shard replica configs
	for shard := int64(0); shard < opts.Shards; shard++ {
		shardName := getShardName(service, shard)
		for i := int64(0); i < opts.ReplicasPerShard; i++ {
			member := utils.GenServiceMemberName(shardName, i)
			index := int(shard+i) % len(azs)
			az := azs[index]
			replicaCfg := genReplicaConfig(platform, domain, member, shardName, shardRole, az)
			replicaCfgs = append(replicaCfgs, replicaCfg)
		}
	}

	return replicaCfgs
}

func getConfigServerName(service string) string {
	return service + common.NameSeparator + configServerName
}

func getShardName(service string, shardNumber int64) string {
	return service + common.NameSeparator + shardName + strconv.FormatInt(shardNumber, 10)
}

func genReplicaConfig(platform string, domain string, member string, replSetName string, role string, az string) *manage.ReplicaConfig {
	// create the service member.conf file
	memberHost := dns.GenDNSName(member, domain)
	bindip := memberHost
	if platform == common.ContainerPlatformSwarm {
		bindip = catalog.BindAllIP
	}
	content := fmt.Sprintf(memberfileContent, az, memberHost, bindip, role, replSetName)

	memberCfg := &manage.ConfigFileContent{
		FileName: catalog.MEMBER_FILE_NAME,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	configs := []*manage.ConfigFileContent{memberCfg}
	return &manage.ReplicaConfig{Zone: az, MemberName: member, Configs: configs}
}

// GenKeyfileContent generates the keyfile for MongoDB internal auth between members of the replica set.
// https://docs.mongodb.com/manual/tutorial/enforce-keyfile-access-control-in-existing-replica-set/
func GenKeyfileContent() (string, error) {
	b := make([]byte, keyfileRandBytes)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(b), nil
}

// EnableMongoDBAuth enables the MongoDB user authentication, after replset initialized and user created.
func EnableMongoDBAuth(content string) string {
	return strings.Replace(content, "ENABLE_SECURITY=false", "ENABLE_SECURITY=true", 1)
}

// GenInitTaskEnvKVPairs generates the environment key-values for the init task.
func GenInitTaskEnvKVPairs(region string, cluster string, service string, manageurl string, opts *manage.CatalogMongoDBOptions) []*common.EnvKeyValuePair {

	kvregion := &common.EnvKeyValuePair{Name: common.ENV_REGION, Value: region}
	kvcluster := &common.EnvKeyValuePair{Name: common.ENV_CLUSTER, Value: cluster}
	kvservice := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_NAME, Value: service}
	kvsvctype := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_TYPE, Value: common.CatalogService_MongoDB}
	kvmgtserver := &common.EnvKeyValuePair{Name: common.ENV_MANAGE_SERVER_URL, Value: manageurl}
	kvop := &common.EnvKeyValuePair{Name: common.ENV_OP, Value: manage.CatalogSetServiceInitOp}

	kvadminuser := &common.EnvKeyValuePair{Name: common.ENV_ADMIN, Value: opts.Admin}
	// TODO simply pass the password as env variable. The init task should fetch from the manage server.
	kvadminpass := &common.EnvKeyValuePair{Name: common.ENV_ADMIN_PASSWORD, Value: opts.AdminPasswd}

	kvshards := &common.EnvKeyValuePair{Name: common.ENV_SHARDS, Value: strconv.FormatInt(opts.Shards, 10)}
	kvreplpershard := &common.EnvKeyValuePair{Name: common.ENV_REPLICAS_PERSHARD, Value: strconv.FormatInt(opts.ReplicasPerShard, 10)}
	kvreplsetonly := &common.EnvKeyValuePair{Name: envReplicaSetOnly, Value: strconv.FormatBool(opts.ReplicaSetOnly)}

	envkvs := []*common.EnvKeyValuePair{kvregion, kvcluster, kvservice, kvsvctype, kvmgtserver, kvop, kvadminuser, kvadminpass, kvshards, kvreplpershard, kvreplsetonly}

	domain := dns.GenDefaultDomainName(cluster)
	if opts.Shards == 1 && opts.ReplicaSetOnly {
		// sigle replica set
		members := dns.GenDNSName(utils.GenServiceMemberName(service, 0), domain)
		for i := int64(1); i < opts.ReplicasPerShard; i++ {
			member := dns.GenDNSName(utils.GenServiceMemberName(service, i), domain)
			members = members + common.ENV_VALUE_SEPARATOR + member
		}
		kvmembers := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_MEMBERS, Value: members}
		envkvs = append(envkvs, kvmembers)
	} else {
		// shard cluster

		// config server replica set
		configServerName := getConfigServerName(service)
		configMembers := dns.GenDNSName(utils.GenServiceMemberName(configServerName, 0), domain)
		for i := int64(1); i < opts.ConfigServers; i++ {
			configMember := dns.GenDNSName(utils.GenServiceMemberName(configServerName, i), domain)
			configMembers = configMembers + common.ENV_VALUE_SEPARATOR + configMember
		}
		kvConfigMembers := &common.EnvKeyValuePair{Name: envConfigServerMembers, Value: configMembers}

		// shard replica set
		shardMembers := ""
		for shard := int64(0); shard < opts.Shards; shard++ {
			shardName := getShardName(service, shard)
			if shard != 0 {
				shardMembers += common.ENV_SHARD_SEPARATOR
			}
			shardMembers += dns.GenDNSName(utils.GenServiceMemberName(shardName, 0), domain)
			for i := int64(1); i < opts.ReplicasPerShard; i++ {
				shardMember := dns.GenDNSName(utils.GenServiceMemberName(shardName, i), domain)
				shardMembers = shardMembers + common.ENV_VALUE_SEPARATOR + shardMember
			}
		}
		kvShardMembers := &common.EnvKeyValuePair{Name: envShardMembers, Value: shardMembers}

		envkvs = append(envkvs, kvConfigMembers, kvShardMembers)
	}

	return envkvs
}

const (
	// ENABLE_SECURITY is set to false at creation. After the cluster is initialized,
	// the init task will set it to true.
	servicefileContent = `
PLATFORM=%s
SHARDS=%d
REPLICAS_PERSHARD=%d
REPLICASET_ONLY=%s
CONFIG_SERVERS=%d
ENABLE_SECURITY=false
`

	// per member config file
	memberfileContent = `
AVAILABILITY_ZONE=%s
SERVICE_MEMBER=%s
BIND_IP=%s
CLUSTER_ROLE=%s
REPLICASET_NAME=%s
`

	mongoDBConfFileName = "mongod.conf"

	mongoDBConfHead = `
# mongod.conf

# for documentation of all options, see:
#   http://docs.mongodb.org/manual/reference/configuration-options/
`

	mongoDBConfStorage = `
# Where and how to store data.
storage:
  dbPath: /data/db
  journal:
    enabled: true
  engine: wiredTiger
`

	mongoDBConfCache = `
  wiredTiger:
    engineConfig:
      cacheSizeGB: %v
`

	// leave systemLog.destination to empty, so MongoDB will send log to stdout.
	// The container log driver will handle the log.
	//	mongoDBConfLog = `
	//# where to write logging data.
	//systemLog:
	//  destination: file
	//  path: /var/log/mongodb/mongod.log
	//`

	mongoDBConfNetwork = `
net:
  bindIp: 0.0.0.0
  port: 27017
`

	mongoDBConfRepl = `
replication:
  replSetName: default
`

	mongoDBAuthStr      = "authorization: enabled"
	mongoDBConfSecurity = `
security:
  keyFile: %s
  authorization: enabled
`

	mongoDBConfSharding = `
sharding:
  clusterRole: %s
`

	mongoDBConfEnd = `
#processManagement:

#security:

#operationProfiling:

## Enterprise-Only Options:

#auditLog:

#snmp:
`
)
