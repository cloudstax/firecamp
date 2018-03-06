package kafkacatalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/catalog/zookeeper"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
	"github.com/golang/glog"
)

const (
	defaultVersion = "1.0"
	// ContainerImage is the main running container.
	ContainerImage = common.ContainerNamePrefix + "kafka:" + defaultVersion

	listenPort = 9092
	jmxPort    = 9093

	// follow http://docs.confluent.io/current/kafka/deployment.html
	// The KAFKA_JVM_PERFORMANCE_OPTS is combined of the default opts in kafka-run-class.sh and
	// http://docs.confluent.io/current/kafka/deployment.html#jvm
	defaultReplFactor     = 3
	defaultInsyncReplicas = 2
	defaultMaxPartitions  = 8

	// DefaultHeapMB is the default kafka java heap size
	DefaultHeapMB = 6144
	// DefaultRetentionHours 7 days
	DefaultRetentionHours = 168

	serverPropConfFileName = "server.properties"
	javaEnvConfFileName    = "java.env"
	logConfFileName        = "log4j.properties"
	// jmx file, common in catalog/types.go
)

// The default Kafka catalog service. By default,
// 1) Distribute the nodes on the availability zones.
// 2) Listen on the standard ports, 9092.

// ValidateUpdateRequest checks if the update request is valid
func ValidateUpdateRequest(req *manage.CatalogUpdateKafkaRequest) error {
	if req.HeapSizeMB < 0 || req.RetentionHours < 0 {
		return errors.New("heap size and retention hours should not be less than 0")
	}
	if len(req.JmxRemoteUser) != 0 && len(req.JmxRemotePasswd) == 0 {
		return errors.New("please set the new jmx remote password")
	}
	return nil
}

// GenDefaultCreateServiceRequest returns the default service creation request.
func GenDefaultCreateServiceRequest(platform string, region string, azs []string,
	cluster string, service string, opts *manage.CatalogKafkaOptions, res *common.Resources,
	zkattr *common.ServiceAttr) (*manage.CreateServiceRequest, error) {
	// check and set the jmx remote user and password
	if len(opts.JmxRemoteUser) == 0 {
		opts.JmxRemoteUser = catalog.JmxDefaultRemoteUser
	}
	if len(opts.JmxRemotePasswd) == 0 {
		opts.JmxRemotePasswd = utils.GenUUID()
	}

	// get the zk server list
	zkServers := genZkServerList(zkattr)

	// generate service ReplicaConfigs
	replicaCfgs := GenReplicaConfigs(platform, cluster, service, azs, opts, zkServers)

	portMappings := []common.PortMapping{
		{ContainerPort: listenPort, HostPort: listenPort, IsServicePort: true},
		{ContainerPort: jmxPort, HostPort: jmxPort},
	}

	reserveMemMB := res.ReserveMemMB
	if res.ReserveMemMB < opts.HeapSizeMB {
		reserveMemMB = opts.HeapSizeMB
	}

	userAttr := &common.KafkaUserAttr{
		HeapSizeMB:      opts.HeapSizeMB,
		AllowTopicDel:   opts.AllowTopicDel,
		RetentionHours:  opts.RetentionHours,
		ZkServiceName:   opts.ZkServiceName,
		JmxRemoteUser:   opts.JmxRemoteUser,
		JmxRemotePasswd: opts.JmxRemotePasswd,
	}
	b, err := json.Marshal(userAttr)
	if err != nil {
		glog.Errorln("Marshal UserAttr error", err, opts)
		return nil, err
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
			ReserveMemMB:    reserveMemMB,
		},

		ContainerImage: ContainerImage,
		Replicas:       opts.Replicas,
		Volume:         opts.Volume,
		ContainerPath:  common.DefaultContainerMountPath,
		PortMappings:   portMappings,

		RegisterDNS:    true,
		ReplicaConfigs: replicaCfgs,

		UserAttr: &common.ServiceUserAttr{
			ServiceType: common.CatalogService_Kafka,
			AttrBytes:   b,
		},
	}
	return req, nil
}

// GenReplicaConfigs generates the replica configs.
func GenReplicaConfigs(platform string, cluster string, service string, azs []string, opts *manage.CatalogKafkaOptions, zkServers string) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)

	// adjust the default configs by the number of members(replicas)
	replFactor := defaultReplFactor
	if int(opts.Replicas) < defaultReplFactor {
		replFactor = int(opts.Replicas)
	}
	minInsyncReplica := defaultInsyncReplicas
	if int(opts.Replicas) < defaultInsyncReplicas {
		minInsyncReplica = int(opts.Replicas)
	}
	numPartitions := defaultMaxPartitions
	if int(opts.Replicas) < defaultMaxPartitions {
		numPartitions = int(opts.Replicas)
	}

	replicaCfgs := make([]*manage.ReplicaConfig, opts.Replicas)
	for i := 0; i < int(opts.Replicas); i++ {
		// create the sys.conf file
		member := utils.GenServiceMemberName(service, int64(i))
		memberHost := dns.GenDNSName(member, domain)
		sysCfg := catalog.CreateSysConfigFile(platform, memberHost)

		// create the server.properties file
		index := i % len(azs)
		bind := memberHost
		if platform == common.ContainerPlatformSwarm {
			bind = catalog.BindAllIP
		}
		content := fmt.Sprintf(serverPropConfig, i, azs[index], strconv.FormatBool(opts.AllowTopicDel), numPartitions, bind, memberHost,
			replFactor, replFactor, replFactor, replFactor, minInsyncReplica, opts.RetentionHours, zkServers)
		serverCfg := &manage.ReplicaConfigFile{
			FileName: serverPropConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		// create the java.env file
		content = fmt.Sprintf(javaEnvConfig, opts.HeapSizeMB, opts.HeapSizeMB, memberHost, jmxPort)
		javaEnvCfg := &manage.ReplicaConfigFile{
			FileName: javaEnvConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		// create the jmxremote.password file
		jmxCfg := catalog.CreateJmxRemotePasswdConfFile(opts.JmxRemoteUser, opts.JmxRemotePasswd)

		// create the log config file
		logCfg := &manage.ReplicaConfigFile{
			FileName: logConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  logConfConfig,
		}

		configs := []*manage.ReplicaConfigFile{sysCfg, serverCfg, javaEnvCfg, jmxCfg, logCfg}

		replicaCfg := &manage.ReplicaConfig{Zone: azs[index], MemberName: member, Configs: configs}
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

// IsServerPropConfFile checks if the file is server.properties conf file
func IsServerPropConfFile(filename string) bool {
	return filename == serverPropConfFileName
}

// UpdateServerPropFile updates the server properties file
func UpdateServerPropFile(newua *common.KafkaUserAttr, ua *common.KafkaUserAttr, oldContent string) string {
	newContent := oldContent
	if newua.AllowTopicDel != ua.AllowTopicDel {
		oldstr := fmt.Sprintf("delete.topic.enable=%s", strconv.FormatBool(ua.AllowTopicDel))
		newstr := fmt.Sprintf("delete.topic.enable=%s", strconv.FormatBool(newua.AllowTopicDel))
		newContent = strings.Replace(newContent, oldstr, newstr, 1)
	}
	if newua.RetentionHours != ua.RetentionHours {
		oldstr := fmt.Sprintf("log.retention.hours=%d", ua.RetentionHours)
		newstr := fmt.Sprintf("log.retention.hours=%d", newua.RetentionHours)
		newContent = strings.Replace(newContent, oldstr, newstr, 1)
	}
	return newContent
}

// IsJvmConfFile checks if the file is jvm conf file
func IsJvmConfFile(filename string) bool {
	return filename == javaEnvConfFileName
}

// UpdateHeapSize updates the heap size in the java.env file content
func UpdateHeapSize(newHeapSizeMB int64, oldHeapSizeMB int64, oldContent string) string {
	oldHeap := fmt.Sprintf("-Xmx%dm -Xms%dm", oldHeapSizeMB, oldHeapSizeMB)
	newHeap := fmt.Sprintf("-Xmx%dm -Xms%dm", newHeapSizeMB, newHeapSizeMB)
	return strings.Replace(oldContent, oldHeap, newHeap, 1)
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
advertised.listeners=PLAINTEXT://%s:9092

log.dirs=/data/kafka

offsets.topic.replication.factor=%d
transaction.state.log.replication.factor=%d
transaction.state.log.min.isr=%d
default.replication.factor=%d
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
KAFKA_JMX_OPTS="-Djava.rmi.server.hostname=%s -Dcom.sun.management.jmxremote=true -Dcom.sun.management.jmxremote.password.file=jmxremote.password -Dcom.sun.management.jmxremote.port=%d -Dcom.sun.management.jmxremote.authenticate=true -Dcom.sun.management.jmxremote.ssl=false"
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
