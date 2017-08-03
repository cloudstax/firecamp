package kafkacatalog

import (
	"fmt"

	"github.com/cloudstax/openmanage/catalog"
	"github.com/cloudstax/openmanage/catalog/zookeeper"
	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/manage"
	"github.com/cloudstax/openmanage/utils"
)

const (
	// ContainerImage is the main running container.
	ContainerImage = common.ContainerNamePrefix + "kafka:" + common.Version

	listenPort = 9092

	// follow http://docs.confluent.io/current/kafka/deployment.html
	// The KAFKA_JVM_PERFORMANCE_OPTS is combined of the default opts in kafka-run-class.sh and
	// http://docs.confluent.io/current/kafka/deployment.html#jvm
	defaultReplFactor     = 3
	defaultInsyncReplicas = 2
	defaultMaxPartitions  = 8
	defaultXmxMB          = 6144

	serverPropConfFileName = "server.properties"
	javaEnvConfFileName    = "java.env"
	logConfFileName        = "log4j.properties"
)

// The default Kafka catalog service. By default,
// 1) Distribute the nodes on the availability zones.
// 2) Listen on the standard ports, 9092.

// GenDefaultCreateServiceRequest returns the default service creation request.
func GenDefaultCreateServiceRequest(region string, azs []string,
	cluster string, service string, replicas int64, volSizeGB int64, res *common.Resources,
	allowTopicDel bool, retentionHours int64, zkattr *common.ServiceAttr) *manage.CreateServiceRequest {
	zkServers := genZkServerList(zkattr)
	// generate service ReplicaConfigs
	replicaCfgs := GenReplicaConfigs(cluster, service, azs, replicas, res.MaxMemMB, allowTopicDel, retentionHours, zkServers)

	portMappings := []common.PortMapping{
		{ContainerPort: listenPort, HostPort: listenPort},
	}

	return &manage.CreateServiceRequest{
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
		PortMappings:   portMappings,

		RegisterDNS:    true,
		ReplicaConfigs: replicaCfgs,
	}
}

// GenReplicaConfigs generates the replica configs.
func GenReplicaConfigs(cluster string, service string, azs []string, replicas int64, maxMemMB int64,
	allowTopicDel bool, retentionHours int64, zkServers string) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)

	// adjust the default configs by the number of members(replicas)
	replFactor := defaultReplFactor
	if int(replicas) < defaultReplFactor {
		replFactor = int(replicas)
	}
	minInsyncReplica := defaultInsyncReplicas
	if int(replicas) < defaultInsyncReplicas {
		minInsyncReplica = int(replicas)
	}
	numPartitions := defaultMaxPartitions
	if int(replicas) < defaultMaxPartitions {
		numPartitions = int(replicas)
	}

	replicaCfgs := make([]*manage.ReplicaConfig, replicas)
	for i := 0; i < int(replicas); i++ {
		// create the sys.conf file
		member := utils.GenServiceMemberName(service, int64(i))
		memberHost := dns.GenDNSName(member, domain)
		sysCfg := catalog.CreateSysConfigFile(memberHost)

		// create the server.properties file
		index := i % len(azs)
		topicDel := "false"
		if allowTopicDel {
			topicDel = "true"
		}
		content := fmt.Sprintf(serverPropConfig, i, azs[i], topicDel, numPartitions, memberHost,
			replFactor, replFactor, replFactor, minInsyncReplica, retentionHours, zkServers)
		serverCfg := &manage.ReplicaConfigFile{
			FileName: serverPropConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		// create the java.env file
		jvmMemMB := maxMemMB
		if maxMemMB == common.DefaultMaxMemoryMB {
			jvmMemMB = defaultXmxMB
		}
		content = fmt.Sprintf(javaEnvConfig, jvmMemMB, jvmMemMB)
		javaEnvCfg := &manage.ReplicaConfigFile{
			FileName: javaEnvConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		// create the log config file
		logCfg := &manage.ReplicaConfigFile{
			FileName: logConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  logConfConfig,
		}

		configs := []*manage.ReplicaConfigFile{sysCfg, serverCfg, javaEnvCfg, logCfg}

		replicaCfg := &manage.ReplicaConfig{Zone: azs[index], Configs: configs}
		replicaCfgs[i] = replicaCfg
	}
	return replicaCfgs
}

// genZkServerList creates the zookeeper server list for zookeeper.connect in server.properties
func genZkServerList(zkattr *common.ServiceAttr) string {
	zkServers := ""
	for i := int64(0); i < zkattr.Replicas; i++ {
		member := utils.GenServiceMemberName(zkattr.ServiceName, i)
		dnsname := dns.GenDNSName(member, zkattr.DomainName)
		if len(zkServers) != 0 {
			zkServers += zkServerSep
		}
		zkServers += fmt.Sprintf("%s:%d", dnsname, zkcatalog.ClientPort)
	}
	return zkServers
}

const (
	zkServerSep = ","

	serverPropConfig = `
# The id of the broker. This must be set to a unique integer for each broker.
broker.id=%d
broker.rack=%s

delete.topic.enable=%s
auto.create.topics.enable=true
num.partitions=%d

listeners=PLAINTEXT://%s:9092

log.dirs=/data/kafka

offsets.topic.replication.factor=%d
transaction.state.log.replication.factor=%d
transaction.state.log.min.isr=%d
min.insync.replicas=%d

unclean.leader.election.enable=false

log.retention.hours=%d

# e.g. "127.0.0.1:3000,127.0.0.1:3001,127.0.0.1:3002"
zookeeper.connect=%s

group.initial.rebalance.delay.ms=3000

`

	javaEnvConfig = `
KAFKA_HEAP_OPTS="-Xmx%dm -Xms%dm"
KAFKA_JVM_PERFORMANCE_OPTS="-server -XX:+UseG1GC -XX:MaxGCPauseMillis=20 -XX:InitiatingHeapOccupancyPercent=35 -XX:+DisableExplicitGC -Djava.awt.headless=true -XX:G1HeapRegionSize=16M -XX:MetaspaceSize=96m -XX:MinMetaspaceFreeRatio=50 -XX:MaxMetaspaceFreeRatio=80"
`

	logConfConfig = `
log4j.rootLogger=INFO, stdout

log4j.appender.stdout=org.apache.log4j.ConsoleAppender
log4j.appender.stdout.layout=org.apache.log4j.PatternLayout
log4j.appender.stdout.layout.ConversionPattern=[%d] %p %m (%c)%n

# Change the two lines below to adjust ZK client logging
log4j.logger.org.I0Itec.zkclient.ZkClient=INFO
log4j.logger.org.apache.zookeeper=INFO

# Change the two lines below to adjust the general broker logging level (output to server.log and stdout)
log4j.logger.kafka=INFO
log4j.logger.org.apache.kafka=INFO

`
)
