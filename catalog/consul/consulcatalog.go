package consulcatalog

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
	defaultVersion = "1.0"
	// ContainerImage is the main running container.
	ContainerImage = common.ContainerNamePrefix + "consul:" + defaultVersion

	rpcPort      = 8300
	serfLanPort  = 8301
	serfWanPort  = 8302
	httpAPIPort  = 8500
	httpsAPIPort = 8080
	dnsPort      = 8600

	defaultDomain = "consul."

	basicConfFileName = "basic_config.json"
	tlsKeyFileName    = "key.pem"
	tlsCertFileName   = "cert.pem"
	tlsCAFileName     = "ca.crt"
)

// ValidateRequest checks if the request is valid
func ValidateRequest(r *manage.CatalogCreateConsulRequest) error {
	if (r.Options.Replicas % 2) == 0 {
		return errors.New("Invalid replicas, please create the odd number replicas")
	}
	// https://www.consul.io/docs/agent/options.html#_encrypt
	// This key must be 16-bytes that are Base64-encoded.
	if len(r.Options.Encrypt) != 0 && len(r.Options.Encrypt) != 16 {
		return errors.New("The encrypt key must be 16-bytes that are Base64-encoded")
	}
	if r.Options.EnableTLS {
		if len(r.Options.CertFileContent) == 0 || len(r.Options.KeyFileContent) == 0 ||
			len(r.Options.CACertFileContent) == 0 || r.Options.HTTPSPort <= 0 {
			return errors.New("tls is enabled, please set the valid cert, key, ca cert files and https port")
		}
	}
	return nil
}

// GenDefaultCreateServiceRequest returns the default service creation request.
func GenDefaultCreateServiceRequest(platform string, region string, azs []string, cluster string,
	service string, res *common.Resources, opts *manage.CatalogConsulOptions) *manage.CreateServiceRequest {
	// generate service configs
	serviceCfgs := genServiceConfigs(platform, region, cluster, service, opts)

	// generate service ReplicaConfigs
	replicaCfgs := genReplicaConfigs(platform, cluster, service, azs, opts)

	portMappings := []common.PortMapping{
		{ContainerPort: rpcPort, HostPort: rpcPort},
		{ContainerPort: serfLanPort, HostPort: serfLanPort},
		{ContainerPort: serfWanPort, HostPort: serfWanPort},
		{ContainerPort: httpAPIPort, HostPort: httpAPIPort, IsServicePort: true},
		{ContainerPort: dnsPort, HostPort: dnsPort},
	}
	if opts.EnableTLS {
		httpsPort := common.PortMapping{ContainerPort: httpsAPIPort, HostPort: opts.HTTPSPort}
		portMappings = append(portMappings, httpsPort)
	}

	req := &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     cluster,
			ServiceName: service,
			ServiceType: common.ServiceTypeStateful,
		},

		Resource:           res,
		CatalogServiceType: common.CatalogService_Consul,

		ContainerImage:  ContainerImage,
		Replicas:        opts.Replicas,
		PortMappings:    portMappings,
		RegisterDNS:     true,
		RequireStaticIP: true,

		ServiceConfigs: serviceCfgs,

		Volume:        opts.Volume,
		ContainerPath: common.DefaultContainerMountPath,

		ReplicaConfigs: replicaCfgs,
	}
	return req
}

// genServiceConfigs generates the service configs.
func genServiceConfigs(platform string, region string, cluster string, service string, opts *manage.CatalogConsulOptions) []*manage.ConfigFileContent {

	// create the service.conf file
	dc := region
	if len(opts.Datacenter) != 0 {
		dc = opts.Datacenter
	}
	consulDomain := opts.Domain
	if len(consulDomain) == 0 {
		consulDomain = defaultDomain
	}

	content := fmt.Sprintf(servicefileContent, platform, dc, consulDomain, opts.Encrypt, strconv.FormatBool(opts.EnableTLS), opts.HTTPSPort)
	serviceCfg := &manage.ConfigFileContent{
		FileName: catalog.SERVICE_FILE_NAME,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	// create the default basic_config.json file
	domain := dns.GenDefaultDomainName(cluster)

	retryJoinNodes := ""
	for i := int64(0); i < opts.Replicas; i++ {
		member := utils.GenServiceMemberName(service, i)
		memberHost := dns.GenDNSName(member, domain)
		if i == int64(0) {
			retryJoinNodes = fmt.Sprintf("    \"%s\"", memberHost)
		} else {
			retryJoinNodes += fmt.Sprintf(",\n    \"%s\"", memberHost)
		}
	}

	content = fmt.Sprintf(basicConfigs, opts.Replicas, retryJoinNodes)
	basicCfg := &manage.ConfigFileContent{
		FileName: basicConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	configs := []*manage.ConfigFileContent{serviceCfg, basicCfg}

	if opts.EnableTLS {
		// create the key, cert and ca files
		certCfg := &manage.ConfigFileContent{
			FileName: tlsCertFileName,
			FileMode: common.ReadOnlyFileMode,
			Content:  opts.CertFileContent,
		}
		keyCfg := &manage.ConfigFileContent{
			FileName: tlsKeyFileName,
			FileMode: common.ReadOnlyFileMode,
			Content:  opts.KeyFileContent,
		}
		caCfg := &manage.ConfigFileContent{
			FileName: tlsCAFileName,
			FileMode: common.ReadOnlyFileMode,
			Content:  opts.CACertFileContent,
		}
		configs = append(configs, certCfg, keyCfg, caCfg)
	}

	return configs
}

// genReplicaConfigs generates the replica configs.
func genReplicaConfigs(platform string, cluster string, service string, azs []string, opts *manage.CatalogConsulOptions) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)

	replicaCfgs := make([]*manage.ReplicaConfig, opts.Replicas)
	for i := int64(0); i < opts.Replicas; i++ {
		member := utils.GenServiceMemberName(service, i)
		memberHost := dns.GenDNSName(member, domain)

		azIndex := int(i) % len(azs)
		az := azs[azIndex]

		bind := memberHost
		if platform == common.ContainerPlatformSwarm {
			bind = catalog.BindAllIP
		}

		// create member.conf file
		staticip := ""
		content := fmt.Sprintf(memberfileContent, az, member, memberHost, staticip, bind)
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

// SetMemberStaticIP sets the static ip in member.conf
func SetMemberStaticIP(oldContent string, memberHost string, staticip string) string {
	// update member staticip.
	newstr := fmt.Sprintf("MEMBER_STATIC_IP=%s", staticip)
	content := strings.Replace(oldContent, "MEMBER_STATIC_IP=", newstr, 1)

	// if BIND_IP is memberHost, set it with staticip as well.
	substr := fmt.Sprintf("BIND_IP=%s", memberHost)
	newstr = fmt.Sprintf("BIND_IP=%s", staticip)
	return strings.Replace(content, substr, newstr, 1)
}

// IsBasicConfigFile checks if the file is the basic_config.json
func IsBasicConfigFile(filename string) bool {
	return filename == basicConfFileName
}

// UpdateBasicConfigsWithIPs replace the retry_join with members' ips.
// memberips, key: member dns name, value: ip.
func UpdateBasicConfigsWithIPs(content string, memberips map[string]string) string {
	for dnsname, ip := range memberips {
		content = strings.Replace(content, dnsname, ip, -1)
	}
	return content
}

const (
	servicefileContent = `
PLATFORM=%s
DC=%s
DOMAIN=%s
ENCRYPT=%s
ENABLE_TLS=%s
HTTPS_PORT=%d
`

	memberfileContent = `
AVAILABILITY_ZONE=%s
MEMBER_NAME=%s
SERVICE_MEMBER=%s
MEMBER_STATIC_IP=%s
BIND_IP=%s
`

	// set raft_multiplier to 1 for production. https://www.consul.io/docs/agent/options.html#performance
	basicConfigs = `
{
  "datacenter": "myDC",
  "data_dir": "/data/consul/",
  "node_name": "memberNodeName",
  "server": true,
  "bootstrap_expect": %d,
  "advertise_addr": "advertiseAddr",
  "bind_addr": "bindAddr",
  "domain": "myDomain",
  "leave_on_terminate": false,
  "skip_leave_on_interrupt": true,
  "rejoin_after_leave": true,
  "retry_interval": "30s",
  "retry_join": [
    %s
  ],
  "performance": {
    "raft_multiplier": 1
  },
`
)
