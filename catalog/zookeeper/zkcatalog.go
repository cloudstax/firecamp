package zkcatalog

import (
	"fmt"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

const (
	defaultVersion = "3.4"
	// ContainerImage is the main ZooKeeper running container.
	ContainerImage = common.ContainerNamePrefix + "zookeeper:" + defaultVersion

	// ClientPort is the port at which the clients will connect
	ClientPort      = 2181
	peerConnectPort = 2888
	leaderElectPort = 3888

	// DefaultHeapMB is the default zookeeper java heap size
	DefaultHeapMB = 4096

	zooConfFileName     = "zoo.cfg"
	myidConfFileName    = "myid"
	javaEnvConfFileName = "java.env"
	logConfFileName     = "log4j.properties"
)

// The default ZooKeeper catalog service. By default,
// 1) Distribute the node on the availability zones.
// 2) Listen on the standard ports, 2181 2888 3888.

// GenDefaultCreateServiceRequest returns the default service creation request.
func GenDefaultCreateServiceRequest(platform string, region string, azs []string,
	cluster string, service string, opts *manage.CatalogZooKeeperOptions, res *common.Resources) *manage.CreateServiceRequest {
	// generate service ReplicaConfigs
	replicaCfgs := GenReplicaConfigs(platform, region, cluster, service, azs, opts)

	portMappings := []common.PortMapping{
		{ContainerPort: ClientPort, HostPort: ClientPort},
		{ContainerPort: peerConnectPort, HostPort: peerConnectPort},
		{ContainerPort: leaderElectPort, HostPort: leaderElectPort},
	}

	reserveMemMB := res.ReserveMemMB
	if res.ReserveMemMB < opts.HeapSizeMB {
		reserveMemMB = opts.HeapSizeMB
	}

	return &manage.CreateServiceRequest{
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
	}
}

// GenReplicaConfigs generates the replica configs.
func GenReplicaConfigs(platform string, region string, cluster string, service string, azs []string, opts *manage.CatalogZooKeeperOptions) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)
	serverList := genServerList(service, domain, opts.Replicas)

	replicaCfgs := make([]*manage.ReplicaConfig, opts.Replicas)
	for i := 0; i < int(opts.Replicas); i++ {
		// create the sys.conf file
		member := utils.GenServiceMemberName(service, int64(i))
		memberHost := dns.GenDNSName(member, domain)
		sysCfg := catalog.CreateSysConfigFile(platform, memberHost)

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
		content = fmt.Sprintf(javaEnvConfig, opts.HeapSizeMB)
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
