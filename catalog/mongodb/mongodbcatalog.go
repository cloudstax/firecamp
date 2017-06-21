package mongodbcatalog

import (
	"fmt"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/containersvc"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/manage"
	"github.com/cloudstax/openmanage/server"
	"github.com/cloudstax/openmanage/utils"
)

const (
	ContainerImage     = common.ContainerNamePrefix + "mongodb"
	InitContainerImage = common.ContainerNamePrefix + "mongodb-init"
	EnvReplicaSetName  = "REPLICA_SET_NAME"
	DefaultPort        = int64(27017)
	defaultReplicas    = int64(3)
)

// The default MongoDB ReplicaSet catalog service. By default,
// 1) One MongoDB ReplicaSet has 1 primary and 2 secondary replicas across 3 availability zones.
// 2) Listen on the standard MongoDB port, 27017.
// 3) The ReplicaSetName is the service name.

// GenDefaultCreateServiceRequest returns the default MongoDB ReplicaSet creation request.
func GenDefaultCreateServiceRequest(cluster string, service string, volSizeGB int64,
	res *common.Resources, serverInfo server.Info) *manage.CreateServiceRequest {
	azs := serverInfo.GetLocalRegionAZs()

	// generate service ReplicaConfigs
	replSetName := service
	replicaCfgs := GenReplicaConfigs(azs, defaultReplicas, replSetName, DefaultPort)

	return &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      serverInfo.GetLocalRegion(),
			Cluster:     cluster,
			ServiceName: service,
		},

		Resource: res,

		ContainerImage: ContainerImage,
		Replicas:       defaultReplicas,
		VolumeSizeGB:   volSizeGB,
		ContainerPath:  common.DefaultContainerMountPath,
		Port:           DefaultPort,

		HasMembership:  true,
		ReplicaConfigs: replicaCfgs,
	}
}

// GenDefaultInitTaskRequest returns the default MongoDB ReplicaSet init task request.
func GenDefaultInitTaskRequest(req *manage.ServiceCommonRequest, serviceUUID string, manageurl string) *containersvc.RunTaskOptions {
	replSetName := req.ServiceName
	envkvs := GenInitTaskEnvKVPairs(req.Region, req.Cluster, req.ServiceName, replSetName, defaultReplicas, manageurl)

	commonOpts := &containersvc.CommonOptions{
		Cluster:        req.Cluster,
		ServiceName:    req.ServiceName,
		ServiceUUID:    serviceUUID,
		ContainerImage: InitContainerImage,
		Resource: &common.Resources{
			MaxCPUUnits:     common.DefaultCpuUnits,
			ReserveCPUUnits: common.DefaultCpuUnits,
			MaxMemMB:        common.DefaultMemoryMBMax,
			ReserveMemMB:    common.DefaultMemoryMBReserve,
		},
	}

	return &containersvc.RunTaskOptions{
		Common:   commonOpts,
		TaskType: common.TaskTypeInit,
		Envkvs:   envkvs,
	}
}

// GenReplicaConfigs generates the replica configs.
// Note: if the number of availability zones is less than replicas, 2 or more replicas will run on the same zone.
func GenReplicaConfigs(azs []string, replicas int64, replSetName string, port int64) []*manage.ReplicaConfig {
	replicaCfgs := make([]*manage.ReplicaConfig, replicas)
	for i := 0; i < int(replicas); i++ {
		index := i % len(azs)
		netcontent := fmt.Sprintf(mongoDBConfNetwork, port)
		replcontent := fmt.Sprintf(mongoDBConfRepl, replSetName)
		content := mongoDBConfHead + mongoDBConfStorage + mongoDBConfLog + netcontent + replcontent + mongoDBConfEnd
		cfg := &manage.ReplicaConfigFile{FileName: mongoDBConfFileName, Content: content}
		configs := []*manage.ReplicaConfigFile{cfg}
		replicaCfg := &manage.ReplicaConfig{Zone: azs[index], Configs: configs}
		replicaCfgs[i] = replicaCfg
	}
	return replicaCfgs
}

// GenInitTaskEnvKVPairs generates the environment key-values for the init task.
func GenInitTaskEnvKVPairs(region string, cluster string, service string,
	replSetName string, replicas int64, manageurl string) []*common.EnvKeyValuePair {
	kvregion := &common.EnvKeyValuePair{Name: common.ENV_REGION, Value: region}
	kvcluster := &common.EnvKeyValuePair{Name: common.ENV_CLUSTER, Value: cluster}
	kvservice := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_NAME, Value: service}
	kvreplSet := &common.EnvKeyValuePair{Name: EnvReplicaSetName, Value: replSetName}
	kvmgtserver := &common.EnvKeyValuePair{Name: common.ENV_MANAGE_SERVER_URL, Value: manageurl}
	kvop := &common.EnvKeyValuePair{Name: common.ENV_OP, Value: manage.ServiceInitializedOp}

	domain := dns.GenDefaultDomainName(cluster)
	masterDNS := dns.GenDNSName(utils.GenServiceMemberName(service, 0), domain)
	kvmaster := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_MASTER, Value: masterDNS}

	envkvs := []*common.EnvKeyValuePair{kvregion, kvcluster, kvservice, kvreplSet, kvmgtserver, kvop, kvmaster}
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
#  mmapv1:
  wiredTiger:
    engineConfig:
      cacheSizeGB: 1
`

	mongoDBConfLog = `
# where to write logging data.
systemLog:
  destination: file
  path: /var/log/mongodb/mongod.log
`

	mongoDBConfNetwork = `
# network interfaces, bind 0.0.0.0
net:
  port: %d
`

	mongoDBConfRepl = `
replication:
  replSetName: %s
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
