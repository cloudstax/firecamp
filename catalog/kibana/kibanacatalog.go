package kibanacatalog

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/catalog/elasticsearch"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

const (
	// ContainerImage is the main running container.
	ContainerImage = common.ContainerNamePrefix + "kibana:" + common.Version

	// https://www.elastic.co/guide/en/kibana/5.6/settings.html
	kbHTTPPort = 5601

	// DefaultHeapMB is the default elasticsearch java heap size
	DefaultHeapMB = 2048
	// https://www.elastic.co/guide/en/elasticsearch/reference/current/heap-size.html
	// "26 GB is safe on most systems".
	maxHeapMB = 26 * 1024

	kbConfFileName     = "kibana.yml"
	kbsslConfFileName  = "server.key"
	kbcertConfFileName = "server.cert"
	esConfFileName     = "elasticsearch.yml"
	jvmConfFileName    = "jvm.options"
)

// ValidateRequest checks if the request is valid
func ValidateRequest(r *manage.CatalogCreateKibanaRequest) error {
	if r.Resource.ReserveMemMB > maxHeapMB {
		return errors.New("max heap size is 26GB")
	}
	if len(r.Options.ESServiceName) == 0 {
		return errors.New("please specify the elasticsearch service name")
	}
	if len(r.Options.ProxyBasePath) != 0 && strings.HasSuffix(r.Options.ProxyBasePath, "/") {
		return errors.New("the ProxyBasePath cannot end in a slash (/)")
	}
	if r.Options.EnableSSL && (len(r.Options.SSLKey) == 0 || len(r.Options.SSLCert) == 0) {
		return errors.New("SSL enabled, but ssl key or certificate is empty")
	}
	return nil
}

// GenDefaultCreateServiceRequest returns the default service creation request.
func GenDefaultCreateServiceRequest(platform string, region string, azs []string, cluster string,
	service string, res *common.Resources, opts *manage.CatalogKibanaOptions, unicastHosts string, minMasterNodes int64) *manage.CreateServiceRequest {
	// generate service ReplicaConfigs
	replicaCfgs := GenReplicaConfigs(platform, cluster, service, azs, res, opts, unicastHosts, minMasterNodes)

	portMappings := []common.PortMapping{
		{ContainerPort: kbHTTPPort, HostPort: kbHTTPPort},
		{ContainerPort: escatalog.HTTPPort, HostPort: escatalog.HTTPPort},
		{ContainerPort: escatalog.TransportTCPPort, HostPort: escatalog.TransportTCPPort},
	}

	return &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     cluster,
			ServiceName: service,
		},

		Resource: res,

		ContainerImage: ContainerImage,
		Replicas:       opts.Replicas,
		VolumeSizeGB:   opts.VolumeSizeGB,
		ContainerPath:  common.DefaultContainerMountPath,
		PortMappings:   portMappings,

		RegisterDNS:    true,
		ReplicaConfigs: replicaCfgs,
	}
}

// GenReplicaConfigs generates the replica configs.
func GenReplicaConfigs(platform string, cluster string, service string, azs []string,
	res *common.Resources, opts *manage.CatalogKibanaOptions, unicastHosts string, minMasterNodes int64) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)

	replicaCfgs := make([]*manage.ReplicaConfig, opts.Replicas)
	for i := int64(0); i < opts.Replicas; i++ {
		// create the sys.conf file
		member := utils.GenServiceMemberName(service, i)
		memberHost := dns.GenDNSName(member, domain)
		sysCfg := catalog.CreateSysConfigFile(platform, memberHost)

		azIndex := int(i) % len(azs)
		az := azs[azIndex]

		bind := memberHost
		if platform == common.ContainerPlatformSwarm {
			bind = catalog.BindAllIP
		}

		// create the kibana.yml file
		content := fmt.Sprintf(kbConfigs, member, bind)
		if opts.EnableSSL {
			content += kbSSLConfigs
		}
		if len(opts.ProxyBasePath) != 0 {
			content += fmt.Sprintf(kbServerBasePath, opts.ProxyBasePath)
		}
		kbCfg := &manage.ReplicaConfigFile{
			FileName: kbConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		// create the elasticsearch.yml file
		transportBind := memberHost
		if platform == common.ContainerPlatformSwarm {
			transportBind = catalog.BindAllIP
		}
		content = fmt.Sprintf(esConfigs, opts.ESServiceName, member, az, memberHost, transportBind, memberHost, unicastHosts, minMasterNodes)
		esCfg := &manage.ReplicaConfigFile{
			FileName: esConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		// create the jvm.options file
		content = fmt.Sprintf(escatalog.ESJvmHeapConfigs, res.ReserveMemMB, res.ReserveMemMB)
		content += escatalog.ESJvmConfigs
		jvmCfg := &manage.ReplicaConfigFile{
			FileName: jvmConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		configs := []*manage.ReplicaConfigFile{sysCfg, kbCfg, esCfg, jvmCfg}

		// create ssl key & cert files if enabled
		if opts.EnableSSL {
			sslKeyCfg := &manage.ReplicaConfigFile{
				FileName: kbsslConfFileName,
				FileMode: common.DefaultConfigFileMode,
				Content:  opts.SSLKey,
			}
			sslCertCfg := &manage.ReplicaConfigFile{
				FileName: kbcertConfFileName,
				FileMode: common.DefaultConfigFileMode,
				Content:  opts.SSLCert,
			}
			configs = append(configs, sslKeyCfg, sslCertCfg)
		}

		replicaCfg := &manage.ReplicaConfig{Zone: az, Configs: configs}
		replicaCfgs[i] = replicaCfg
	}
	return replicaCfgs
}

const (
	// https://www.elastic.co/guide/en/kibana/5.6/production.html
	// https://github.com/elastic/kibana-docker/tree/5.6/build/kibana/config
	kbConfigs = `
server.name: %s
server.host: %s
elasticsearch.url: http://localhost:9200

path.data: /data/kibana

xpack.security.enabled: false
xpack.monitoring.ui.container.elasticsearch.enabled: false
`

	kbSSLConfigs = `
server.ssl.enabled: true
server.ssl.key: /data/conf/server.key
server.ssl.certificate: /data/conf/server.cert
`

	kbServerBasePath = `server.basePath: %s`

	// https://www.elastic.co/guide/en/elasticsearch/reference/current/modules-network.html
	// https://www.elastic.co/guide/en/elasticsearch/reference/current/modules-node.html
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

node.master: false
node.data: false
node.ingest: false

network.bind_host: localhost
network.publish_host: %s
transport.bind_host: %s
transport.publish_host: %s
discovery.zen.ping.unicast.hosts: [%s]

path.data: /data/elasticsearch

discovery.zen.minimum_master_nodes: %d

xpack.security.enabled: false
`

	esSep = ", "
)
