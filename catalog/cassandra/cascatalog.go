package cascatalog

import (
	"errors"
	"fmt"

	"github.com/cloudstax/firecamp/api/catalog"
	"github.com/cloudstax/firecamp/api/manage"
	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/pkg/containersvc"
	"github.com/cloudstax/firecamp/pkg/dns"
	"github.com/cloudstax/firecamp/pkg/log"
	"github.com/cloudstax/firecamp/pkg/utils"
)

const (
	defaultVersion = "3.11"
	// ContainerImage is the main Cassandra running container.
	ContainerImage = common.ContainerNamePrefix + "cassandra:" + defaultVersion
	// InitContainerImage initializes the Cassandra cluster.
	InitContainerImage = common.ContainerNamePrefix + "cassandra-init:" + defaultVersion

	// DefaultHeapMB is the default Cassandra JVM heap size.
	// https://docs.datastax.com/en/cassandra/2.1/cassandra/operations/ops_tune_jvm_c.html
	DefaultHeapMB = 8192
	// MaxHeapMB is the max Cassandra JVM heap size. Cassandra recommends no more than 14GB
	MaxHeapMB = 14 * 1024
	// MinHeapMB is the minimal JVM heap size. If heap < 1024, jvm may stall long time at gc.
	MinHeapMB = 1024

	intraNodePort    = 7000
	tlsIntraNodePort = 7001
	jmxPort          = 7199
	cqlPort          = 9042
	thriftPort       = 9160
	// jolokiaPort is added after release 0.9.4
	jolokiaPort = 8778

	logConfFileName = "logback.xml"
)

// The default Cassandra catalog service. By default,
// 1) Have equal number of nodes on 3 availability zones.
// 2) Listen on the standard ports, 7000 7001 7199 9042 9160.

// ValidateRequest checks if the create request is valid
func ValidateRequest(req *catalog.CatalogCreateCassandraRequest) error {
	if req.Options.HeapSizeMB <= 0 {
		return errors.New("heap size should be larger than 0")
	}
	if req.Options.HeapSizeMB > MaxHeapMB {
		return errors.New("max heap size is 14GB")
	}
	if req.Options.JournalVolume == nil {
		return errors.New("cassandra should have separate volume for journal")
	}
	if req.Options.Replicas == 2 {
		return errors.New("2 nodes are not allowed")
	}

	return nil
}

// GenDefaultCreateServiceRequest returns the default service creation request.
func GenDefaultCreateServiceRequest(platform string, region string, azs []string,
	cluster string, service string, opts *catalog.CatalogCassandraOptions, res *common.Resources) (req *manage.CreateServiceRequest, jmxUser string, jmxPasswd string) {
	// generate service ReplicaConfigs
	if len(opts.JmxRemoteUser) == 0 {
		opts.JmxRemoteUser = catalog.JmxDefaultRemoteUser
	}
	if len(opts.JmxRemotePasswd) == 0 {
		opts.JmxRemotePasswd = utils.GenUUID()
	}

	serviceCfgs := genServiceConfigs(platform, region, cluster, service, azs, opts)

	replicaCfgs := GenReplicaConfigs(platform, cluster, service, azs, opts.Replicas)

	portMappings := []common.PortMapping{
		{ContainerPort: intraNodePort, HostPort: intraNodePort},
		{ContainerPort: tlsIntraNodePort, HostPort: tlsIntraNodePort},
		{ContainerPort: jmxPort, HostPort: jmxPort},
		{ContainerPort: cqlPort, HostPort: cqlPort, IsServicePort: true},
		{ContainerPort: thriftPort, HostPort: thriftPort},
		{ContainerPort: jolokiaPort, HostPort: jolokiaPort},
	}

	reserveMemMB := res.ReserveMemMB
	if res.ReserveMemMB < opts.HeapSizeMB {
		reserveMemMB = opts.HeapSizeMB
	}

	req = &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     cluster,
			ServiceName: service,
		},

		Resource: &common.Resources{
			MaxCPUUnits:     res.MaxCPUUnits,
			ReserveCPUUnits: res.ReserveCPUUnits,
			MaxMemMB:        res.MaxMemMB,
			ReserveMemMB:    reserveMemMB,
		},

		ServiceType:        common.ServiceTypeStateful,
		CatalogServiceType: common.CatalogService_Cassandra,

		ContainerImage: ContainerImage,
		Replicas:       opts.Replicas,
		PortMappings:   portMappings,
		RegisterDNS:    true,

		ServiceConfigs: serviceCfgs,

		Volume:               opts.Volume,
		ContainerPath:        common.DefaultContainerMountPath,
		JournalVolume:        opts.JournalVolume,
		JournalContainerPath: common.DefaultJournalVolumeContainerMountPath,

		ReplicaConfigs: replicaCfgs,
	}
	return req, opts.JmxRemoteUser, opts.JmxRemotePasswd
}

// genServiceConfigs generates the service configs.
func genServiceConfigs(platform string, region string, cluster string, service string, azs []string, opts *catalog.CatalogCassandraOptions) []*manage.ConfigFileContent {
	// create the service.conf file
	domain := dns.GenDefaultDomainName(cluster)
	seeds := genSeedHosts(int64(len(azs)), opts.Replicas, service, domain)
	content := fmt.Sprintf(servicefileContent, platform, region, cluster, seeds, opts.HeapSizeMB, opts.JmxRemoteUser, opts.JmxRemotePasswd)
	serviceCfg := &manage.ConfigFileContent{
		FileName: catalog.SERVICE_FILE_NAME,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	// create the logback.xml file
	logCfg := &manage.ConfigFileContent{
		FileName: logConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  logConfContent,
	}

	configs := []*manage.ConfigFileContent{serviceCfg, logCfg}
	return configs
}

// GenReplicaConfigs generates the replica configs.
func GenReplicaConfigs(platform string, cluster string, service string, azs []string, replicas int64) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)

	replicaCfgs := make([]*manage.ReplicaConfig, replicas)
	for i := int64(0); i < replicas; i++ {
		member := utils.GenServiceMemberName(service, i)
		memberHost := dns.GenDNSName(member, domain)

		rpcAddr := memberHost
		if platform == common.ContainerPlatformSwarm {
			rpcAddr = catalog.BindAllIP
		}

		// create the member.conf file
		index := int(i) % len(azs)
		content := fmt.Sprintf(memberfileContent, azs[index], memberHost, rpcAddr)
		memberCfg := &manage.ConfigFileContent{
			FileName: catalog.MEMBER_FILE_NAME,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		configs := []*manage.ConfigFileContent{memberCfg}
		replicaCfg := &manage.ReplicaConfig{Zone: azs[index], MemberName: member, Configs: configs}
		replicaCfgs[i] = replicaCfg
	}
	return replicaCfgs
}

func genSeedHosts(azCount int64, replicas int64, service string, domain string) string {
	seedCount := azCount
	if seedCount > replicas {
		seedCount = replicas
	}
	seedHostSep := ","
	seedHost := ""
	for i := int64(0); i < seedCount; i++ {
		member := utils.GenServiceMemberName(service, i)
		host := dns.GenDNSName(member, domain)
		if i == (seedCount - 1) {
			seedHost += host
		} else {
			seedHost += host + seedHostSep
		}
	}
	return seedHost
}

// GenDefaultInitTaskRequest returns the default Cassandra init task request.
func GenDefaultInitTaskRequest(req *manage.ServiceCommonRequest, logConfig *cloudlog.LogConfig,
	serviceUUID string, manageurl string) *containersvc.RunTaskOptions {
	envkvs := GenInitTaskEnvKVPairs(req.Region, req.Cluster, req.ServiceName, manageurl)

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
func GenInitTaskEnvKVPairs(region string, cluster string, service string, manageurl string) []*common.EnvKeyValuePair {
	kvregion := &common.EnvKeyValuePair{Name: common.ENV_REGION, Value: region}
	kvcluster := &common.EnvKeyValuePair{Name: common.ENV_CLUSTER, Value: cluster}
	kvmgtserver := &common.EnvKeyValuePair{Name: common.ENV_MANAGE_SERVER_URL, Value: manageurl}
	kvop := &common.EnvKeyValuePair{Name: common.ENV_OP, Value: catalog.CatalogSetServiceInitOp}

	kvservice := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_NAME, Value: service}
	kvsvctype := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_TYPE, Value: common.CatalogService_Cassandra}

	domain := dns.GenDefaultDomainName(cluster)
	dnsname := dns.GenDNSName(utils.GenServiceMemberName(service, 0), domain)
	kvnode := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_NODE, Value: dnsname}

	return []*common.EnvKeyValuePair{kvregion, kvcluster, kvmgtserver, kvop, kvservice, kvsvctype, kvnode}
}

const (
	servicefileContent = `
PLATFORM=%s
REGION=%s
CLUSTER=%s
CASSANDRA_SEEDS=%s
HEAP_SIZE_MB=%d
JMX_REMOTE_USER=%s
JMX_REMOTE_PASSWD=%s
`

	memberfileContent = `
AVAILABILITY_ZONE=%s
SERVICE_MEMBER=%s
RPC_ADDRESS=%s
`

	logConfContent = `
<configuration scan="true">
  <jmxConfigurator />
  <!-- STDOUT console appender to stdout (INFO level) -->

  <appender name="STDOUT" class="ch.qos.logback.core.ConsoleAppender">
    <filter class="ch.qos.logback.classic.filter.ThresholdFilter">
      <level>INFO</level>
    </filter>
    <encoder>
      <pattern>%-5level [%thread] %date{ISO8601} %F:%L - %msg%n</pattern>
    </encoder>
  </appender>

  <root level="INFO">
    <appender-ref ref="STDOUT" />
  </root>

  <logger name="org.apache.cassandra" level="INFO"/>
  <logger name="com.thinkaurelius.thrift" level="ERROR"/>
</configuration>
`
)
