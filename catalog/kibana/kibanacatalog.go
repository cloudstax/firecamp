package kibanacatalog

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudstax/firecamp/api/catalog"
	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/pkg/dns"
	"github.com/cloudstax/firecamp/api/manage"
	"github.com/cloudstax/firecamp/pkg/utils"
)

const (
	defaultVersion = "5.6"
	// ContainerImage is the main running container.
	ContainerImage = common.ContainerNamePrefix + "kibana:" + defaultVersion

	// DefaultReserveMemoryMB is the default reserved memory size for Kibana
	DefaultReserveMemoryMB = 2048

	// https://www.elastic.co/guide/en/kibana/5.6/settings.html
	kbHTTPPort = 5601

	kbsslConfFileName  = "server.key"
	kbcertConfFileName = "server.cert"
)

// ValidateRequest checks if the request is valid
func ValidateRequest(r *catalog.CatalogCreateKibanaRequest) error {
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
	service string, res *common.Resources, opts *catalog.CatalogKibanaOptions, esNodeURL string) *manage.CreateServiceRequest {
	// generate service configs
	serviceCfgs := genServiceConfigs(platform, opts, esNodeURL)

	// generate member ReplicaConfigs
	replicaCfgs := genReplicaConfigs(platform, cluster, service, azs, opts)

	portMappings := []common.PortMapping{
		{ContainerPort: kbHTTPPort, HostPort: kbHTTPPort, IsServicePort: true},
	}

	req := &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     cluster,
			ServiceName: service,
		},

		Resource: res,

		ServiceType:        common.ServiceTypeStateful,
		CatalogServiceType: common.CatalogService_Kibana,

		ContainerImage: ContainerImage,
		Replicas:       opts.Replicas,
		PortMappings:   portMappings,
		RegisterDNS:    true,

		ServiceConfigs: serviceCfgs,

		Volume:        opts.Volume,
		ContainerPath: common.DefaultContainerMountPath,

		ReplicaConfigs: replicaCfgs,
	}
	return req
}

// genServiceConfigs generates the service configs.
func genServiceConfigs(platform string, opts *catalog.CatalogKibanaOptions, esNodeURL string) []*manage.ConfigFileContent {
	// create the service.conf file
	content := fmt.Sprintf(servicefileContent, platform, opts.ESServiceName, esNodeURL, opts.ProxyBasePath, strconv.FormatBool(opts.EnableSSL))
	serviceCfg := &manage.ConfigFileContent{
		FileName: catalog.SERVICE_FILE_NAME,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	configs := []*manage.ConfigFileContent{serviceCfg}

	// create ssl key and cert files if ssl is enabled
	if opts.EnableSSL {
		sslKeyCfg := &manage.ConfigFileContent{
			FileName: kbsslConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  opts.SSLKey,
		}
		sslCertCfg := &manage.ConfigFileContent{
			FileName: kbcertConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  opts.SSLCert,
		}
		configs = append(configs, sslKeyCfg, sslCertCfg)
	}
	return configs
}

// genReplicaConfigs generates the replica configs.
func genReplicaConfigs(platform string, cluster string, service string, azs []string, opts *catalog.CatalogKibanaOptions) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)

	replicaCfgs := make([]*manage.ReplicaConfig, opts.Replicas)
	for i := int64(0); i < opts.Replicas; i++ {
		azIndex := int(i) % len(azs)
		az := azs[azIndex]

		member := utils.GenServiceMemberName(service, i)
		memberHost := dns.GenDNSName(member, domain)

		bind := memberHost
		if platform == common.ContainerPlatformSwarm {
			bind = catalog.BindAllIP
		}

		// create the member.conf file
		content := fmt.Sprintf(memberfileContent, az, member, memberHost, bind)
		memberCfg := &manage.ConfigFileContent{
			FileName: catalog.MEMBER_FILE_NAME,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		configs := []*manage.ConfigFileContent{memberCfg}
		replicaCfg := &manage.ReplicaConfig{Zone: az, MemberName: member, Configs: configs}
		replicaCfgs[i] = replicaCfg
	}
	return replicaCfgs
}

const (
	servicefileContent = `
PLATFORM=%s
ES_SERVICE_NAME=%s
ES_URL=%s
PROXY_BASE_PATH=%s
ENABLE_SSL=%s
`

	memberfileContent = `
AVAILABILITY_ZONE=%s
MEMBER_NAME=%s
SERVICE_MEMBER=%s
BIND_IP=%s
`
)
