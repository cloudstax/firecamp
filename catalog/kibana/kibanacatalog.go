package kibanacatalog

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

const (
	// ContainerImage is the main running container.
	ContainerImage = common.ContainerNamePrefix + "kibana:" + common.Version

	// DefaultReserveMemoryMB is the default reserved memory size for Kibana
	DefaultReserveMemoryMB = 2048

	// https://www.elastic.co/guide/en/kibana/5.6/settings.html
	kbHTTPPort = 5601

	kbConfFileName     = "kibana.yml"
	kbsslConfFileName  = "server.key"
	kbcertConfFileName = "server.cert"
)

// ValidateRequest checks if the request is valid
func ValidateRequest(r *manage.CatalogCreateKibanaRequest) error {
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
// kibana simply connects to the first elasticsearch node. TODO create a local coordinating elasticsearch instance.
func GenDefaultCreateServiceRequest(platform string, region string, azs []string, cluster string,
	service string, res *common.Resources, opts *manage.CatalogKibanaOptions, esNode string) *manage.CreateServiceRequest {
	// generate service ReplicaConfigs
	replicaCfgs := GenReplicaConfigs(platform, cluster, service, azs, res, opts, esNode)

	portMappings := []common.PortMapping{
		{ContainerPort: kbHTTPPort, HostPort: kbHTTPPort},
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
func GenReplicaConfigs(platform string, cluster string, service string, azs []string, res *common.Resources, opts *manage.CatalogKibanaOptions, esNode string) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)

	replicaCfgs := make([]*manage.ReplicaConfig, opts.Replicas)
	for i := int64(0); i < opts.Replicas; i++ {
		// create the sys.conf file
		member := utils.GenServiceMemberName(service, i)
		memberHost := dns.GenDNSName(member, domain)
		sysCfg := catalog.CreateSysConfigFile(platform, memberHost)

		// create the kibana.yml file
		content := fmt.Sprintf(kbConfigs, member, catalog.BindAllIP, esNode)
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

		configs := []*manage.ReplicaConfigFile{sysCfg, kbCfg}

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

		azIndex := int(i) % len(azs)
		az := azs[azIndex]
		replicaCfg := &manage.ReplicaConfig{Zone: az, Configs: configs}
		replicaCfgs[i] = replicaCfg
	}
	return replicaCfgs
}

const (
	// https://www.elastic.co/guide/en/kibana/5.6/production.html
	// https://github.com/elastic/kibana-docker/tree/5.6/build/kibana/config
	//
	// set "cpu.cgroup.path.override=/" and "cpuacct.cgroup.path.override=/".
	// Below are explanations copied from Kibana 5.6.3 docker start cmd, /usr/local/bin/kibana-docker.
	// The virtual file /proc/self/cgroup should list the current cgroup
	// membership. For each hierarchy, you can follow the cgroup path from
	// this file to the cgroup filesystem (usually /sys/fs/cgroup/) and
	// introspect the statistics for the cgroup for the given
	// hierarchy. Alas, Docker breaks this by mounting the container
	// statistics at the root while leaving the cgroup paths as the actual
	// paths. Therefore, Kibana provides a mechanism to override
	// reading the cgroup path from /proc/self/cgroup and instead uses the
	// cgroup path defined the configuration properties
	// cpu.cgroup.path.override and cpuacct.cgroup.path.override.
	// Therefore, we set this value here so that cgroup statistics are
	// available for the container this process will run in.
	kbConfigs = `
server.name: %s
server.host: %s
elasticsearch.url: http://%s:9200

path.data: /data/kibana

cpu.cgroup.path.override: /
cpuacct.cgroup.path.override: /

xpack.security.enabled: false
xpack.monitoring.ui.container.elasticsearch.enabled: false
`

	kbSSLConfigs = `
server.ssl.enabled: true
server.ssl.key: /data/conf/server.key
server.ssl.certificate: /data/conf/server.cert
`

	kbServerBasePath = `server.basePath: %s`
)
