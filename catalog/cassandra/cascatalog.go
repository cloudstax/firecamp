package cascatalog

import (
	"errors"
	"fmt"

	"github.com/jazzl0ver/firecamp/api/catalog"
	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/jazzl0ver/firecamp/api/manage"
	"github.com/jazzl0ver/firecamp/pkg/dns"
	"github.com/jazzl0ver/firecamp/pkg/utils"
)

const (
	defaultVersion = "4.1"
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
	if req.Options.AuditLoggingEnabled != "true" && req.Options.AuditLoggingEnabled != "false" {
		return errors.New("AuditLoggingEnabled must be true or false")
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

	if len(opts.AuditLoggingEnabled) == 0 {
		opts.AuditLoggingEnabled = "true"
	}

	if len(opts.AuditLoggingClassName) == 0 {
		opts.AuditLoggingClassName = "FileAuditLogger"
	}

	if len(opts.AuditIncludedKeyspaces) == 0 {
		opts.AuditIncludedKeyspaces = ""
	}

	if len(opts.AuditExcludedKeyspaces) == 0 {
		opts.AuditExcludedKeyspaces = "system, system_schema, system_virtual_schema"
	}

	if len(opts.AuditIncludedCategories) == 0 {
		opts.AuditIncludedCategories = ""
	}

	if len(opts.AuditExcludedCategories) == 0 {
		opts.AuditExcludedCategories = ""
	}

	if len(opts.AuditIncludedUsers) == 0 {
		opts.AuditIncludedUsers = ""
	}

	if len(opts.AuditExcludedUsers) == 0 {
		opts.AuditExcludedUsers = ""
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
			Region:             region,
			Cluster:            cluster,
			ServiceName:        service,
			CatalogServiceType: common.CatalogService_Cassandra,
		},

		Resource: &common.Resources{
			MaxCPUUnits:     res.MaxCPUUnits,
			ReserveCPUUnits: res.ReserveCPUUnits,
			MaxMemMB:        res.MaxMemMB,
			ReserveMemMB:    reserveMemMB,
		},

		ServiceType: common.ServiceTypeStateful,

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
	content := fmt.Sprintf(servicefileContent, platform, region, cluster, seeds, opts.HeapSizeMB, opts.JmxRemoteUser, opts.JmxRemotePasswd,
	    opts.AuditLoggingEnabled,
	    opts.AuditLoggingClassName,
	    opts.AuditIncludedKeyspaces,
	    opts.AuditExcludedKeyspaces,
	    opts.AuditIncludedCategories,
	    opts.AuditExcludedCategories,
	    opts.AuditIncludedUsers,
	    opts.AuditExcludedUsers,
	    )
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
func GenDefaultInitTaskRequest(req *manage.ServiceCommonRequest, manageurl string) *manage.RunTaskRequest {
	envkvs := GenInitTaskEnvKVPairs(req.Region, req.Cluster, req.ServiceName, manageurl)

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
AUDIT_LOGGING_ENABLED="%s"
AUDIT_LOGGING_CLASSNAME="%s"
AUDIT_INCLUDED_KEYSPACES="%s"
AUDIT_EXCLUDED_KEYSPACES="%s"
AUDIT_INCLUDED_CATEGORIES="%s"
AUDIT_EXCLUDED_CATEGORIES="%s"
AUDIT_INCLUDED_USERS="%s"
AUDIT_EXCLUDED_USERS="%s"
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
