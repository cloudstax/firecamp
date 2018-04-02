package escatalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
	"github.com/golang/glog"
)

const (
	defaultVersion = "5.6"
	// ContainerImage is the main running container.
	ContainerImage = common.ContainerNamePrefix + "elasticsearch:" + defaultVersion

	// DefaultHeapMB is the default elasticsearch java heap size
	DefaultHeapMB = 2048

	// https://www.elastic.co/guide/en/elasticsearch/reference/current/modules-network.html
	// suggests 9200-9300 for http.port and 9300-9400 for transport.tcp.port.
	// The Dockerfile expose 9200 and 9300.
	HTTPPort         = 9200
	TransportTCPPort = 9300

	// https://www.elastic.co/guide/en/elasticsearch/reference/current/heap-size.html
	// "26 GB is safe on most systems".
	maxHeapMB = 26 * 1024

	// DefaultMasterNumber is the default dedicated master nodes.
	DefaultMasterNumber = int64(3)

	// if there are more than 10 data nodes, DisableDedicatedMaster will be skipped,
	// and dedicated master nodes will be created
	dataNodeThreshold = int64(10)

	esConfFileName  = "elasticsearch.yml"
	jvmConfFileName = "jvm.options"
)

// ValidateRequest checks if the request is valid
func ValidateRequest(r *manage.CatalogCreateElasticSearchRequest) error {
	if r.Options.HeapSizeMB <= 0 {
		return errors.New("heap size should be larger than 0")
	}
	if r.Options.HeapSizeMB > maxHeapMB {
		return errors.New("max heap size is 26GB")
	}
	if r.Options.Replicas == int64(2) && r.Options.DisableDedicatedMaster {
		return errors.New("2 nodes without the dedicated masters are not allowed as it could hit split-brain issue")
	}
	if !r.Options.DisableDedicatedMaster && r.Options.DedicatedMasters != 0 {
		if r.Options.DedicatedMasters < DefaultMasterNumber {
			return errors.New("the cluster should have at least 3 dedicated masters")
		}
		if (r.Options.DedicatedMasters % 2) == 0 {
			return errors.New("should have the odd number dedicated masters")
		}
	}
	return nil
}

// GenDefaultCreateServiceRequest returns the default service creation request.
func GenDefaultCreateServiceRequest(platform string, region string, azs []string, cluster string,
	service string, res *common.Resources, opts *manage.CatalogElasticSearchOptions) (*manage.CreateServiceRequest, error) {
	// generate service ReplicaConfigs
	replicaCfgs := GenReplicaConfigs(platform, cluster, service, azs, opts)

	portMappings := []common.PortMapping{
		{ContainerPort: HTTPPort, HostPort: HTTPPort, IsServicePort: true},
		{ContainerPort: TransportTCPPort, HostPort: TransportTCPPort},
	}

	reserveMemMB := res.ReserveMemMB
	if res.ReserveMemMB < opts.HeapSizeMB {
		reserveMemMB = opts.HeapSizeMB
	}

	userAttr := &common.ESUserAttr{
		HeapSizeMB:             opts.HeapSizeMB,
		DataNodes:              opts.Replicas,
		DedicatedMasters:       opts.DedicatedMasters,
		DisableForceAwareness:  opts.DisableForceAwareness,
		DisableDedicatedMaster: opts.DisableDedicatedMaster,
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
		Replicas:       int64(len(replicaCfgs)),
		Volume:         opts.Volume,
		ContainerPath:  common.DefaultContainerMountPath,
		PortMappings:   portMappings,

		RegisterDNS:    true,
		ReplicaConfigs: replicaCfgs,

		UserAttr: &common.ServiceUserAttr{
			ServiceType: common.CatalogService_ElasticSearch,
			AttrBytes:   b,
		},
	}
	return req, nil
}

// GenReplicaConfigs generates the replica configs.
func GenReplicaConfigs(platform string, cluster string, service string, azs []string, opts *manage.CatalogElasticSearchOptions) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)
	unicastHosts, masterNodes, dedicateMasterNodes := getUnicastHostsAndMasterNodes(domain, service, opts)

	minMasterNodes := masterNodes/2 + 1
	replicas := opts.Replicas + dedicateMasterNodes
	replicaCfgs := make([]*manage.ReplicaConfig, replicas)

	// generate the dedicate master node configs
	dedicateMaster := true
	masterEligible := true
	for i := int64(0); i < dedicateMasterNodes; i++ {
		member := getDedicatedMasterName(service, i)
		replicaCfgs[i] = genReplicaConfig(platform, service, domain, member, azs,
			i, unicastHosts, minMasterNodes, dedicateMaster, masterEligible, opts)
	}

	// generate the data node configs
	dedicateMaster = false
	if dedicateMasterNodes > 0 {
		masterEligible = false
	}
	for i := int64(0); i < opts.Replicas; i++ {
		member := utils.GenServiceMemberName(service, i)
		replicaCfgs[i+dedicateMasterNodes] = genReplicaConfig(platform, service, domain,
			member, azs, i, unicastHosts, minMasterNodes, dedicateMaster, masterEligible, opts)
	}
	return replicaCfgs
}

func genReplicaConfig(platform string, service string, domain string, member string, azs []string, idx int64, unicastHosts string,
	minMasterNodes int64, dedicateMaster bool, masterEligible bool, opts *manage.CatalogElasticSearchOptions) *manage.ReplicaConfig {

	// create the sys.conf file
	memberHost := dns.GenDNSName(member, domain)
	sysCfg := catalog.CreateSysConfigFile(platform, memberHost)

	azIndex := int(idx) % len(azs)
	az := azs[azIndex]

	// create the elasticsearch.yml file
	bind := memberHost
	if platform == common.ContainerPlatformSwarm {
		bind = catalog.BindAllIP
	}
	content := fmt.Sprintf(esConfigs, service, member, az, bind, memberHost, unicastHosts, minMasterNodes)

	if dedicateMaster {
		content += fmt.Sprintf(nodeConfigs, "true", "false", "false")
	} else {
		if masterEligible {
			content += fmt.Sprintf(nodeConfigs, "true", "true", "true")
		} else {
			content += fmt.Sprintf(nodeConfigs, "false", "true", "true")
		}
	}

	if !opts.DisableForceAwareness {
		forceazs := ""
		for i, az := range azs {
			if i == 0 {
				forceazs = az
			} else {
				forceazs += esSep + az
			}
		}
		content += fmt.Sprintf(forceZone, forceazs)
	}

	esCfg := &manage.ReplicaConfigFile{
		FileName: esConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	// create the jvm.options file
	content = fmt.Sprintf(ESJvmHeapConfigs, opts.HeapSizeMB, opts.HeapSizeMB)
	content += ESJvmConfigs
	jvmCfg := &manage.ReplicaConfigFile{
		FileName: jvmConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	configs := []*manage.ReplicaConfigFile{sysCfg, esCfg, jvmCfg}
	return &manage.ReplicaConfig{Zone: az, MemberName: member, Configs: configs}
}

// getUnicastHostsAndMasterNodes returns the unicast hosts, the targe master node number and how many master nodes will be added.
func getUnicastHostsAndMasterNodes(domain string, service string, opts *manage.CatalogElasticSearchOptions) (unicastHosts string, masterNodes int64, dedicateMasterNodes int64) {
	// check whether adds the dedicate master nodes.
	// if only has 1 node, no need to add the dedicate master nodes. Assume 1 node is for test only.
	// if the number of nodes is within threshold, add the dedicate master nodes by default, while, allow to disable it.
	// enforce to have dedicate master nodes if the number of nodes exceed the threshold,
	if opts.Replicas > dataNodeThreshold || (!opts.DisableDedicatedMaster && opts.Replicas != 1) {
		// dedicate master nodes
		if opts.DedicatedMasters < DefaultMasterNumber {
			masterNodes = DefaultMasterNumber
		} else {
			masterNodes = opts.DedicatedMasters
		}
		dedicateMasterNodes = masterNodes

		for i := int64(0); i < masterNodes; i++ {
			member := getDedicatedMasterName(service, i)
			memberHost := dns.GenDNSName(member, domain)
			if i == int64(0) {
				unicastHosts = memberHost
			} else {
				unicastHosts += esSep + memberHost
			}
		}

		return unicastHosts, masterNodes, dedicateMasterNodes
	}

	// no dedicate master nodes, every node is eligible to become master.
	masterNodes = opts.Replicas/2 + 1
	for i := int64(0); i < masterNodes; i++ {
		member := utils.GenServiceMemberName(service, i)
		memberHost := dns.GenDNSName(member, domain)
		if i == int64(0) {
			unicastHosts = memberHost
		} else {
			unicastHosts += esSep + memberHost
		}
	}
	return unicastHosts, masterNodes, 0
}

// GenDataNodesURIs creates the list of uris for the Elasticsearch data nodes.
// example: http://myes-0.t1-firecamp.com:9200,http://myes-1.t1-firecamp.com:9200
func GenDataNodesURIs(attr *common.ServiceAttr) (uris string, err error) {
	if attr.UserAttr.ServiceType != common.CatalogService_ElasticSearch {
		return "", fmt.Errorf("service %s is not an ElasticSearch service, type %s", attr.ServiceName, attr.UserAttr.ServiceType)
	}

	ua := &common.ESUserAttr{}
	err = json.Unmarshal(attr.UserAttr.AttrBytes, ua)
	if err != nil {
		glog.Errorln("Unmarshal user attr error", err, attr)
		return "", err
	}

	uris = catalog.GenServiceMemberURIs(attr.ClusterName, attr.ServiceName, ua.DataNodes, HTTPPort)
	return uris, nil
}

// GetFirstMemberHost returns the first member's dns name
func GetFirstMemberHost(domain string, service string) string {
	member := utils.GenServiceMemberName(service, 0)
	memberHost := dns.GenDNSName(member, domain)
	return memberHost
}

func getDedicatedMasterName(service string, idx int64) string {
	return service + common.NameSeparator + "master" + common.NameSeparator + strconv.FormatInt(idx, 10)
}

const (
	// https://www.elastic.co/guide/en/elasticsearch/reference/current/modules-network.html
	//
	// https://www.elastic.co/guide/en/elasticsearch/reference/current/important-settings.html
	// discovery.zen.minimum_master_nodes = (master_eligible_nodes / 2) + 1, to avoid split brain.
	//
	// example unicast.hosts: ["host1", "host2:port"]
	esConfigs = `
cluster.name: %s

node.name: %s
node.attr.zone: %s
cluster.routing.allocation.awareness.attributes: zone

network.bind_host: %s
network.publish_host: %s
discovery.zen.ping.unicast.hosts: [%s]

path.data: /data/elasticsearch

discovery.zen.minimum_master_nodes: %d

xpack.security.enabled: false
`

	esSep = ", "

	forceZone = `
cluster.routing.allocation.awareness.force.zone.values: %s
`

	// https://www.elastic.co/guide/en/elasticsearch/reference/current/modules-node.html
	nodeConfigs = `
node.master: %s
node.data: %s
node.ingest: %s
`

	// ESJvmHeapConfigs includes the jvm Xms and Xmx size
	// https://www.elastic.co/guide/en/elasticsearch/reference/current/heap-size.html
	ESJvmHeapConfigs = `
-Xms%dm
-Xmx%dm
`

	// ESJvmConfigs includes other default jvm configs
	// for cgroups setting, see the detail explanations in the bin/es-docker file of the elasticsearch docker 5.6.3.
	ESJvmConfigs = `
# The virtual file /proc/self/cgroup should list the current cgroup
# membership. For each hierarchy, you can follow the cgroup path from
# this file to the cgroup filesystem (usually /sys/fs/cgroup/) and
# introspect the statistics for the cgroup for the given
# hierarchy. Alas, Docker breaks this by mounting the container
# statistics at the root while leaving the cgroup paths as the actual
# paths. Therefore, Elasticsearch provides a mechanism to override
# reading the cgroup path from /proc/self/cgroup and instead uses the
# cgroup path defined the JVM system property
# es.cgroups.hierarchy.override. Therefore, we set this value here so
# that cgroup statistics are available for the container this process
# will run in.
-Des.cgroups.hierarchy.override=/

-Des.enforce.bootstrap.checks=true

## Expert settings

## GC configuration
-XX:+UseConcMarkSweepGC
-XX:CMSInitiatingOccupancyFraction=75
-XX:+UseCMSInitiatingOccupancyOnly

## optimizations

# pre-touch memory pages used by the JVM during initialization
-XX:+AlwaysPreTouch

## basic

# force the server VM (remove on 32-bit client JVMs)
-server

# explicitly set the stack size (reduce to 320k on 32-bit client JVMs)
-Xss1m

# set to headless, just in case
-Djava.awt.headless=true

# ensure UTF-8 encoding by default (e.g. filenames)
-Dfile.encoding=UTF-8

# use our provided JNA always versus the system one
-Djna.nosys=true

# use old-style file permissions on JDK9
-Djdk.io.permissionsUseCanonicalPath=true

# flags to configure Netty
-Dio.netty.noUnsafe=true
-Dio.netty.noKeySetOptimization=true
-Dio.netty.recycler.maxCapacityPerThread=0

# log4j 2
-Dlog4j.shutdownHookEnabled=false
-Dlog4j2.disable.jmx=true
-Dlog4j.skipJansi=true

## heap dumps

# generate a heap dump when an allocation from the Java heap fails
# heap dumps are created in the working directory of the JVM
-XX:+HeapDumpOnOutOfMemoryError

# specify an alternative path for heap dumps
# ensure the directory exists and has sufficient space
-XX:HeapDumpPath=/data/es.hprof

## GC logging
-XX:+PrintGCDetails
-XX:+PrintGCTimeStamps
#-XX:+PrintGCDateStamps
#-XX:+PrintClassHistogram
#-XX:+PrintTenuringDistribution
-XX:+PrintGCApplicationStoppedTime

# log GC status to a file with time stamps
# ensure the directory exists
-Xloggc:/data/ecgc-%t.log

# By default, the GC log file will not rotate.
# By uncommenting the lines below, the GC log file
# will be rotated every 64MB at most 8 times.
-XX:+UseGCLogFileRotation
-XX:NumberOfGCLogFiles=8
-XX:GCLogFileSize=64M
`
)
