package escatalog

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
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

	esConfFileName = "elasticsearch.yml"
	roleMaster     = "master"
	roleData       = "data"
	roleMix        = "mix"
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
	service string, res *common.Resources, opts *manage.CatalogElasticSearchOptions) *manage.CreateServiceRequest {
	domain := dns.GenDefaultDomainName(cluster)
	unicastHosts, masterNodes, dedicateMasterNodes := getUnicastHostsAndMasterNodes(domain, service, opts)
	minMasterNodes := masterNodes/2 + 1
	replicas := opts.Replicas + dedicateMasterNodes

	// generate service configs
	serviceCfgs := genServiceConfigs(platform, service, azs, unicastHosts, minMasterNodes, opts)

	// generate member configs
	replicaCfgs := GenReplicaConfigs(platform, cluster, service, azs,
		unicastHosts, minMasterNodes, dedicateMasterNodes, opts)

	portMappings := []common.PortMapping{
		{ContainerPort: HTTPPort, HostPort: HTTPPort, IsServicePort: true},
		{ContainerPort: TransportTCPPort, HostPort: TransportTCPPort},
	}

	reserveMemMB := res.ReserveMemMB
	if res.ReserveMemMB < opts.HeapSizeMB {
		reserveMemMB = opts.HeapSizeMB
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

		ServiceType:        common.ServiceTypeStateful,
		CatalogServiceType: common.CatalogService_ElasticSearch,

		ContainerImage: ContainerImage,
		Replicas:       replicas,
		PortMappings:   portMappings,
		RegisterDNS:    true,

		ServiceConfigs: serviceCfgs,

		Volume:        opts.Volume,
		ContainerPath: common.DefaultContainerMountPath,

		ReplicaConfigs: replicaCfgs,
	}
	return req
}

func genServiceConfigs(platform string, service string, azs []string, unicastHosts string,
	minMasterNodes int64, opts *manage.CatalogElasticSearchOptions) []*manage.ConfigFileContent {
	// create the service.conf file
	content := fmt.Sprintf(servicefileContent, platform, opts.HeapSizeMB,
		opts.DedicatedMasters, strconv.FormatBool(opts.DisableDedicatedMaster),
		strconv.FormatBool(opts.DisableForceAwareness), opts.Replicas)

	serviceCfg := &manage.ConfigFileContent{
		FileName: catalog.SERVICE_FILE_NAME,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	// create the default elasticsearch.yml file
	content = fmt.Sprintf(esConfigs, service, unicastHosts, minMasterNodes)

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

	esCfg := &manage.ConfigFileContent{
		FileName: esConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	return []*manage.ConfigFileContent{serviceCfg, esCfg}
}

// GenReplicaConfigs generates the replica configs.
func GenReplicaConfigs(platform string, cluster string, service string, azs []string,
	unicastHosts string, minMasterNodes int64, dedicateMasterNodes int64,
	opts *manage.CatalogElasticSearchOptions) []*manage.ReplicaConfig {

	domain := dns.GenDefaultDomainName(cluster)

	replicaCfgs := []*manage.ReplicaConfig{}
	// generate the dedicate master node configs
	for i := int64(0); i < dedicateMasterNodes; i++ {
		// create the member.conf file
		member := getDedicatedMasterName(service, i)
		memberHost := dns.GenDNSName(member, domain)

		azIndex := int(i) % len(azs)
		az := azs[azIndex]

		bind := memberHost
		if platform == common.ContainerPlatformSwarm {
			bind = catalog.BindAllIP
		}

		content := fmt.Sprintf(memberfileContent, az, member, memberHost, bind, roleMaster)
		memberCfg := &manage.ConfigFileContent{
			FileName: catalog.MEMBER_FILE_NAME,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		configs := []*manage.ConfigFileContent{memberCfg}
		replCfg := &manage.ReplicaConfig{Zone: az, MemberName: member, Configs: configs}
		replicaCfgs = append(replicaCfgs, replCfg)
	}

	// generate the data node configs
	role := roleData
	if dedicateMasterNodes == 0 {
		role = roleMix
	}
	for i := int64(0); i < opts.Replicas; i++ {
		// create the member.conf file
		member := utils.GenServiceMemberName(service, i)
		memberHost := dns.GenDNSName(member, domain)

		azIndex := int(i) % len(azs)
		az := azs[azIndex]

		bind := memberHost
		if platform == common.ContainerPlatformSwarm {
			bind = catalog.BindAllIP
		}

		content := fmt.Sprintf(memberfileContent, az, member, memberHost, bind, role)
		memberCfg := &manage.ConfigFileContent{
			FileName: catalog.MEMBER_FILE_NAME,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		configs := []*manage.ConfigFileContent{memberCfg}
		replCfg := &manage.ReplicaConfig{Zone: az, MemberName: member, Configs: configs}
		replicaCfgs = append(replicaCfgs, replCfg)
	}
	return replicaCfgs
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
func GenDataNodesURIs(cluster string, service string, dataNodes int64) string {
	return catalog.GenServiceMemberURIs(cluster, service, dataNodes, HTTPPort)
}

func GetDataNodes(servicecfgContent string) (dataNodes int, err error) {
	lines := strings.Split(servicecfgContent, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "DATA_NODES") {
			strs := strings.Split(line, "=")
			return strconv.Atoi(strs[1])
		}
	}
	return -1, fmt.Errorf("DATA_NODES not find in service configs", servicecfgContent)
}

// GetFirstMemberURI returns the first data node uri
// example: http://myes-0.t1-firecamp.com:9200
func GetFirstMemberURI(domain string, service string) string {
	member := utils.GenServiceMemberName(service, 0)
	memberHost := dns.GenDNSName(member, domain)
	return fmt.Sprintf("http://%s:%d", memberHost, HTTPPort)
}

func getDedicatedMasterName(service string, idx int64) string {
	return service + common.NameSeparator + "master" + common.NameSeparator + strconv.FormatInt(idx, 10)
}

const (
	servicefileContent = `
PLATFORM=%s
HEAP_SIZE_MB=%d
DEDICATED_MASTERS=%d
DISABLE_DEDICATD_MASTER=%s
DISABLE_FORCE_AWARENESS=%s
DATA_NODES=%d
`

	memberfileContent = `
AVAILABILITY_ZONE=%s
MEMBER_NAME=%s
SERVICE_MEMBER=%s
BIND_IP=%s
MEMBER_ROLE=%s
`

	esSep = ", "

	// https://www.elastic.co/guide/en/elasticsearch/reference/current/modules-network.html
	//
	// https://www.elastic.co/guide/en/elasticsearch/reference/current/important-settings.html
	// discovery.zen.minimum_master_nodes = (master_eligible_nodes / 2) + 1, to avoid split brain.
	//
	// example unicast.hosts: ["host1", "host2:port"]
	esConfigs = `
cluster.name: %s

cluster.routing.allocation.awareness.attributes: zone

discovery.zen.ping.unicast.hosts: [%s]

path.data: /data/elasticsearch

discovery.zen.minimum_master_nodes: %d

xpack.security.enabled: false
`

	forceZone = `
cluster.routing.allocation.awareness.force.zone.values: %s
`
)
