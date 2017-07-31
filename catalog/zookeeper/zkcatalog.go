package zkcatalog

import (
	"fmt"

	"github.com/cloudstax/openmanage/catalog"
	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/manage"
	"github.com/cloudstax/openmanage/utils"
)

const (
	// ContainerImage is the main ZooKeeper running container.
	ContainerImage = common.ContainerNamePrefix + "zookeeper:" + common.Version

	clientPort      = 2181
	peerConnectPort = 2888
	leaderElectPort = 3888

	defaultXmx = 4096

	zooConfFileName     = "zoo.cfg"
	myidConfFileName    = "myid"
	javaEnvConfFileName = "java.env"
	logConfFileName     = "log4j.properties"
)

// The default ZooKeeper catalog service. By default,
// 1) Distribute the node on the availability zones.
// 2) Listen on the standard ports, 2181 2888 3888.

// GenDefaultCreateServiceRequest returns the default service creation request.
func GenDefaultCreateServiceRequest(region string, azs []string,
	cluster string, service string, replicas int64, volSizeGB int64, res *common.Resources) *manage.CreateServiceRequest {
	// generate service ReplicaConfigs
	replicaCfgs := GenReplicaConfigs(region, cluster, service, azs, replicas, res.MaxMemMB)

	portMappings := []common.PortMapping{
		{ContainerPort: clientPort, HostPort: clientPort},
		{ContainerPort: peerConnectPort, HostPort: peerConnectPort},
		{ContainerPort: leaderElectPort, HostPort: leaderElectPort},
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
func GenReplicaConfigs(region string, cluster string, service string, azs []string, replicas int64, maxMemMB int64) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)
	serverList := genServerList(service, domain, replicas)

	replicaCfgs := make([]*manage.ReplicaConfig, replicas)
	for i := 0; i < int(replicas); i++ {
		// create the sys.conf file
		member := utils.GenServiceMemberName(service, int64(i))
		memberHost := dns.GenDNSName(member, domain)
		sysCfg := catalog.CreateSysConfigFile(memberHost)

		// create the zoo.cfg file
		content := fmt.Sprintf(zooConfigs, serverList)
		zooCfg := &manage.ReplicaConfigFile{
			FileName: zooConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		// create the myid file
		content = fmt.Sprintf(myidConfig, i+1)
		myidCfg := &manage.ReplicaConfigFile{
			FileName: myidConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		// create the java.env file
		jvmMemMB := maxMemMB
		if maxMemMB == common.DefaultMaxMemoryMB {
			jvmMemMB = defaultXmx
		}
		content = fmt.Sprintf(javaEnvConfig, jvmMemMB)
		javaEnvCfg := &manage.ReplicaConfigFile{
			FileName: javaEnvConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		// create the log config file
		logCfg := &manage.ReplicaConfigFile{
			FileName: logConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  logConfContent,
		}

		configs := []*manage.ReplicaConfigFile{sysCfg, zooCfg, myidCfg, javaEnvCfg, logCfg}

		index := i % len(azs)
		replicaCfg := &manage.ReplicaConfig{Zone: azs[index], Configs: configs}
		replicaCfgs[i] = replicaCfg
	}
	return replicaCfgs
}

func genServerList(service string, domain string, replicas int64) string {
	serverList := ""
	for i := 0; i < int(replicas); i++ {
		member := utils.GenServiceMemberName(service, int64(i))
		dnsname := dns.GenDNSName(member, domain)
		server := fmt.Sprintf(serverLine, i+1, dnsname, peerConnectPort, leaderElectPort)
		serverList += server
	}
	return serverList
}

const (
	serverLine = `
server.%d=%s:%d:%d`

	zooConfigs = `
# http://zookeeper.apache.org/doc/r3.4.10/zookeeperAdmin.html

# The number of milliseconds of each tick
tickTime=2000

# The number of ticks that the initial
# synchronization phase can take
initLimit=10

# The number of ticks that can pass between
# sending a request and getting an acknowledgement
syncLimit=5

# the directory where the snapshot is stored.
dataDir=/data

# the port at which the clients will connect
clientPort=2181

# Be sure to read the maintenance section of the
# administrator guide before turning on autopurge.
#
# The number of snapshots to retain in dataDir
#autopurge.snapRetainCount=3

# Purge task interval in hours
# Set to "0" to disable auto purge feature
#autopurge.purgeInterval=1

# all zookeeper members
%s
`

	myidConfig = `%d`

	javaEnvConfig = `
export JVMFLAGS="-Xmx%dm"
`

	logConfContent = `
zookeeper.root.logger=INFO, CONSOLE
zookeeper.console.threshold=INFO

# DEFAULT: console appender only
log4j.rootLogger=${zookeeper.root.logger}

#
# Log INFO level and above messages to the console
#
log4j.appender.CONSOLE=org.apache.log4j.ConsoleAppender
log4j.appender.CONSOLE.Threshold=${zookeeper.console.threshold}
log4j.appender.CONSOLE.layout=org.apache.log4j.PatternLayout
log4j.appender.CONSOLE.layout.ConversionPattern=%d{ISO8601} [myid:%X{myid}] - %-5p [%t:%C{1}@%L] - %m%n
`
)
