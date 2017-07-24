package mongodbcatalog

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cloudstax/openmanage/catalog"
	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/containersvc"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/log"
	"github.com/cloudstax/openmanage/manage"
	"github.com/cloudstax/openmanage/utils"
)

const (
	// ContainerImage is the main MongoDB running container.
	ContainerImage = common.ContainerNamePrefix + "mongodb:" + common.Version
	// InitContainerImage initializes the MongoDB ReplicaSet.
	InitContainerImage = common.ContainerNamePrefix + "mongodb-init:" + common.Version

	envReplicaSetName = "REPLICA_SET_NAME"
	envAdminUser      = "ADMIN_USER"
	envAdminPassword  = "ADMIN_PASSWORD"
	defaultPort       = int64(27017)

	keyfileName      = "keyfile"
	keyfileRandBytes = 200
	keyfileMode      = 0400
)

// The default MongoDB ReplicaSet catalog service. By default,
// 1) One MongoDB ReplicaSet has 1 primary and 2 secondary replicas across 3 availability zones.
// 2) Listen on the standard port, 27017.
// 3) The ReplicaSetName is the service name.

// GenDefaultCreateServiceRequest returns the default MongoDB ReplicaSet creation request.
func GenDefaultCreateServiceRequest(region string, azs []string, cluster string, service string, replicas int64,
	volSizeGB int64, res *common.Resources) (*manage.CreateServiceRequest, error) {
	// generate service ReplicaConfigs
	replSetName := service
	replicaCfgs, err := GenReplicaConfigs(azs, service, replicas, replSetName, defaultPort, res.MaxMemMB)
	if err != nil {
		return nil, err
	}

	portmapping := common.PortMapping{
		ContainerPort: defaultPort,
		HostPort:      defaultPort,
	}

	req := &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     cluster,
			ServiceName: service,
		},

		Resource: res,

		ContainerImage: ContainerImage,
		Replicas:       replicas,
		VolumeSizeGB:   volSizeGB,
		ContainerPath:  common.DefaultContainerMountPath,
		PortMappings:   []common.PortMapping{portmapping},

		RegisterDNS:    true,
		ReplicaConfigs: replicaCfgs,
	}
	return req, nil
}

// GenDefaultInitTaskRequest returns the default MongoDB ReplicaSet init task request.
func GenDefaultInitTaskRequest(req *manage.ServiceCommonRequest, logConfig *cloudlog.LogConfig,
	serviceUUID string, replicas int64, manageurl string, admin string, adminPass string) *containersvc.RunTaskOptions {
	replSetName := req.ServiceName
	envkvs := GenInitTaskEnvKVPairs(req.Region, req.Cluster, req.ServiceName,
		replSetName, replicas, manageurl, admin, adminPass)

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

// GenReplicaConfigs generates the replica configs.
// Note: if the number of availability zones is less than replicas, 2 or more replicas will run on the same zone.
func GenReplicaConfigs(azs []string, service string, replicas int64, replSetName string, port int64, maxMemMB int64) ([]*manage.ReplicaConfig, error) {
	// generate the keyfile for MongoDB internal auth between members of the replica set.
	// https://docs.mongodb.com/manual/tutorial/enforce-keyfile-access-control-in-existing-replica-set/
	keyfileContent, err := genKeyfileContent()
	if err != nil {
		return nil, err
	}
	keyfileCfg := &manage.ReplicaConfigFile{FileName: keyfileName, FileMode: keyfileMode, Content: keyfileContent}

	// generate the replica configs
	replicaCfgs := make([]*manage.ReplicaConfig, replicas)
	for i := 0; i < int(replicas); i++ {
		// create the sys.conf file
		member := utils.GenServiceMemberName(service, int64(i))
		sysCfg := catalog.CreateSysConfigFile(member)

		// create the mongod.conf file
		index := i % len(azs)
		netcontent := fmt.Sprintf(mongoDBConfNetwork, port)
		replcontent := fmt.Sprintf(mongoDBConfRepl, replSetName)
		var content string
		if maxMemMB == common.DefaultMaxMemoryMB {
			// no max memory limit, mongodb will compute the cache size based on the node's memory.
			content = mongoDBConfHead + mongoDBConfStorage + netcontent + replcontent + mongoDBConfEnd
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
			content = mongoDBConfHead + mongoDBConfStorage + cacheContent + netcontent + replcontent + mongoDBConfEnd
		}
		mongoCfg := &manage.ReplicaConfigFile{
			FileName: mongoDBConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}
		configs := []*manage.ReplicaConfigFile{sysCfg, mongoCfg, keyfileCfg}
		replicaCfg := &manage.ReplicaConfig{Zone: azs[index], Configs: configs}
		replicaCfgs[i] = replicaCfg
	}
	return replicaCfgs, nil
}

func genKeyfileContent() (string, error) {
	b := make([]byte, keyfileRandBytes)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(b), nil
}

// IsMongoDBConfFile checks if the file is the MongoDB config file
func IsMongoDBConfFile(filename string) bool {
	return filename == mongoDBConfFileName
}

// IsAuthEnabled checks if auth is already enabled.
func IsAuthEnabled(content string) bool {
	n := strings.Index(content, mongoDBAuthStr)
	return n != -1
}

// EnableMongoDBAuth enables the MongoDB user authentication, after replset initialized and user created.
func EnableMongoDBAuth(content string) string {
	keyfilePath := filepath.Join(common.DefaultConfigPath, keyfileName)
	seccontent := fmt.Sprintf(mongoDBConfSecurity, keyfilePath)
	return strings.Replace(content, "#security:", seccontent, 1)
}

// GenInitTaskEnvKVPairs generates the environment key-values for the init task.
func GenInitTaskEnvKVPairs(region string, cluster string, service string,
	replSetName string, replicas int64, manageurl string, admin string, adminPass string) []*common.EnvKeyValuePair {
	kvregion := &common.EnvKeyValuePair{Name: common.ENV_REGION, Value: region}
	kvcluster := &common.EnvKeyValuePair{Name: common.ENV_CLUSTER, Value: cluster}
	kvservice := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_NAME, Value: service}
	kvreplSet := &common.EnvKeyValuePair{Name: envReplicaSetName, Value: replSetName}
	kvsvctype := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_TYPE, Value: catalog.CatalogService_MongoDB}
	kvmgtserver := &common.EnvKeyValuePair{Name: common.ENV_MANAGE_SERVER_URL, Value: manageurl}
	kvop := &common.EnvKeyValuePair{Name: common.ENV_OP, Value: manage.CatalogSetServiceInitOp}

	domain := dns.GenDefaultDomainName(cluster)
	masterDNS := dns.GenDNSName(utils.GenServiceMemberName(service, 0), domain)
	kvmaster := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_MASTER, Value: masterDNS}

	kvadminuser := &common.EnvKeyValuePair{Name: envAdminUser, Value: admin}
	// TODO simply pass the password as env variable. The init task should fetch from the manage server.
	kvadminpass := &common.EnvKeyValuePair{Name: envAdminPassword, Value: adminPass}

	envkvs := []*common.EnvKeyValuePair{kvregion, kvcluster, kvservice,
		kvreplSet, kvsvctype, kvmgtserver, kvop, kvmaster, kvadminuser, kvadminpass}
	if replicas == 1 {
		return envkvs
	}

	slaves := dns.GenDNSName(utils.GenServiceMemberName(service, 1), domain)
	for i := int64(2); i < replicas; i++ {
		slaves = slaves + common.ENV_VALUE_SEPARATOR + dns.GenDNSName(utils.GenServiceMemberName(service, i), domain)
	}
	kvslaves := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_MEMBERS, Value: slaves}
	envkvs = append(envkvs, kvslaves)
	return envkvs
}

const (
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
# network interfaces, bind 0.0.0.0
net:
  port: %d
`

	mongoDBConfRepl = `
replication:
  replSetName: %s
`

	mongoDBAuthStr      = "authorization: enabled"
	mongoDBConfSecurity = `
security:
  keyFile: %s
  authorization: enabled
`

	mongoDBConfEnd = `
#sharding:

#processManagement:

#security:

#operationProfiling:

## Enterprise-Only Options:

#auditLog:

#snmp:
`
)
