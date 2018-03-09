package zkcatalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
	"github.com/golang/glog"
)

const (
	defaultVersion = "3.4"
	// ContainerImage is the main ZooKeeper running container.
	ContainerImage = common.ContainerNamePrefix + "zookeeper:" + defaultVersion

	// ClientPort is the port at which the clients will connect
	ClientPort      = 2181
	peerConnectPort = 2888
	leaderElectPort = 3888
	jmxPort         = 2191

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

// ValidateUpdateRequest checks if the update request is valid
func ValidateUpdateRequest(req *manage.CatalogUpdateZooKeeperRequest) error {
	if req.HeapSizeMB < 0 {
		return errors.New("heap size should not be less than 0")
	}
	if len(req.JmxRemoteUser) != 0 && len(req.JmxRemotePasswd) == 0 {
		return errors.New("please set the new jmx remote password")
	}
	return nil
}

// GenDefaultCreateServiceRequest returns the default service creation request.
func GenDefaultCreateServiceRequest(platform string, region string, azs []string,
	cluster string, service string, opts *manage.CatalogZooKeeperOptions, res *common.Resources) (*manage.CreateServiceRequest, error) {
	// check and set the jmx remote user and password
	if len(opts.JmxRemoteUser) == 0 {
		opts.JmxRemoteUser = catalog.JmxDefaultRemoteUser
	}
	if len(opts.JmxRemotePasswd) == 0 {
		opts.JmxRemotePasswd = utils.GenUUID()
	}

	// generate service ReplicaConfigs
	replicaCfgs := GenReplicaConfigs(platform, region, cluster, service, azs, opts)

	portMappings := []common.PortMapping{
		{ContainerPort: ClientPort, HostPort: ClientPort, IsServicePort: true},
		{ContainerPort: peerConnectPort, HostPort: peerConnectPort},
		{ContainerPort: leaderElectPort, HostPort: leaderElectPort},
		{ContainerPort: jmxPort, HostPort: jmxPort},
	}

	reserveMemMB := res.ReserveMemMB
	if res.ReserveMemMB < opts.HeapSizeMB {
		reserveMemMB = opts.HeapSizeMB
	}

	userAttr := &common.ZKUserAttr{
		HeapSizeMB:      opts.HeapSizeMB,
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
			ServiceType: common.CatalogService_ZooKeeper,
			AttrBytes:   b,
		},
	}
	return req, nil
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
		// TODO handle upgrade from 0.9.4
		content = fmt.Sprintf(javaEnvConfig, opts.HeapSizeMB, memberHost, jmxPort)
		javaEnvCfg := &manage.ReplicaConfigFile{
			FileName: javaEnvConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		// create the jmxremote.password file
		jmxPasswdCfg := catalog.CreateJmxRemotePasswdConfFile(opts.JmxRemoteUser, opts.JmxRemotePasswd)

		// create the jmxremote.access file
		jmxAccessCfg := catalog.CreateJmxRemoteAccessConfFile(opts.JmxRemoteUser, catalog.JmxReadOnlyAccess)

		// create the log config file
		logCfg := &manage.ReplicaConfigFile{
			FileName: logConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  logConfContent,
		}

		configs := []*manage.ReplicaConfigFile{sysCfg, zooCfg, myidCfg, javaEnvCfg, jmxPasswdCfg, jmxAccessCfg, logCfg}

		index := i % len(azs)
		replicaCfg := &manage.ReplicaConfig{Zone: azs[index], MemberName: member, Configs: configs}
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

// IsJavaEnvFile checks whether the filename is javaEnvConfFileName
func IsJavaEnvFile(filename string) bool {
	return filename == javaEnvConfFileName
}

// UpdateHeapSize updates the heap size in javaEnvConfFileName
func UpdateHeapSize(newHeap int64, oldHeap int64, content string) string {
	old := fmt.Sprintf("-Xmx%dm", oldHeap)
	new := fmt.Sprintf("-Xmx%dm", newHeap)
	return strings.Replace(content, old, new, 1)
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
export JVMFLAGS="-Xmx%dm -Djava.rmi.server.hostname=%s -Dcom.sun.management.jmxremote.password.file=/data/conf/jmxremote.password -Dcom.sun.management.jmxremote.access.file=/data/conf/jmxremote.access"
export JMXPORT=%d
export JMXAUTH=true
export JMXSSL=false
export JMXLOG4J=true
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
