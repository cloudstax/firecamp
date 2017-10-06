package consulcatalog

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
	ContainerImage = common.ContainerNamePrefix + "consul:" + common.Version

	rpcPort      = 8300
	serfLanPort  = 8301
	serfWanPort  = 8302
	httpAPIPort  = 8500
	httpsAPIPort = 8080
	dnsPort      = 8600

	defaultDomain = "consul."

	basicConfFileName = "basic_config.json"
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
	// generate service ReplicaConfigs
	replicaCfgs := GenReplicaConfigs(platform, region, cluster, service, azs, opts)

	portMappings := []common.PortMapping{
		{ContainerPort: rpcPort, HostPort: rpcPort},
		{ContainerPort: serfLanPort, HostPort: serfLanPort},
		{ContainerPort: serfWanPort, HostPort: serfWanPort},
		{ContainerPort: httpAPIPort, HostPort: httpAPIPort},
		{ContainerPort: dnsPort, HostPort: dnsPort},
	}
	if opts.EnableTLS {
		httpsPort := common.PortMapping{ContainerPort: httpsAPIPort, HostPort: opts.HTTPSPort}
		portMappings = append(portMappings, httpsPort)
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

		RegisterDNS:     true,
		RequireStaticIP: true,
		ReplicaConfigs:  replicaCfgs,
	}
}

// GenReplicaConfigs generates the replica configs.
func GenReplicaConfigs(platform string, region string, cluster string, service string, azs []string, opts *manage.CatalogConsulOptions) []*manage.ReplicaConfig {
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

	replicaCfgs := make([]*manage.ReplicaConfig, opts.Replicas)
	for i := int64(0); i < opts.Replicas; i++ {
		member := utils.GenServiceMemberName(service, i)
		memberHost := dns.GenDNSName(member, domain)
		// create the sys.conf file
		sysCfg := catalog.CreateSysConfigFile(platform, memberHost)

		// create basic_config.json file
		consulDomain := opts.Domain
		if len(consulDomain) == 0 {
			consulDomain = defaultDomain
		}
		bind := memberHost
		if platform == common.ContainerPlatformSwarm {
			bind = "0.0.0.0"
		}

		dc := region
		if len(opts.Datacenter) != 0 {
			dc = opts.Datacenter
		}

		content := configHeader
		content += fmt.Sprintf(basicConfigs, dc, member, opts.Replicas, memberHost, bind, consulDomain, retryJoinNodes)
		if len(opts.Encrypt) != 0 {
			content += fmt.Sprintf(encryptConfigs, opts.Encrypt)
		}
		if opts.EnableTLS {
			content += fmt.Sprintf(tlsConfigs, opts.KeyFileContent, opts.CertFileContent, opts.CACertFileContent, opts.HTTPSPort)
		}
		content += configFooter

		basicCfg := &manage.ReplicaConfigFile{
			FileName: basicConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		configs := []*manage.ReplicaConfigFile{sysCfg, basicCfg}

		azIndex := int(i) % len(azs)
		az := azs[azIndex]
		replicaCfg := &manage.ReplicaConfig{Zone: az, Configs: configs}
		replicaCfgs[i] = replicaCfg
	}

	return replicaCfgs
}

// IsBasicConfigFile checks if the file is the basic_config.json
func IsBasicConfigFile(filename string) bool {
	return filename == basicConfFileName
}

// ReplaceMemberName replaces the member's dns name with ip.
// memberips, key: member dns name, value: ip.
func ReplaceMemberName(content string, memberips map[string]string) string {
	for dnsname, ip := range memberips {
		content = strings.Replace(content, dnsname, ip, -1)
	}
	return content
}

const (
	// https://www.consul.io/docs/agent/options.html for details.
	configHeader = `
{
`

	configFooter = `
  "log_level": "INFO",
  "ui": true
}
`

	// set raft_multiplier to 1 for production. https://www.consul.io/docs/agent/options.html#performance
	basicConfigs = `
  "datacenter": "%s",
  "data_dir": "/data/consul/",
  "node_name": "%s",
  "server": true,
  "bootstrap_expect": %d,
  "advertise_addr": "%s",
  "bind_addr": "%s",
  "domain": "%s",
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

	// The secret key to use for encryption of Consul network traffic. This key must be 16-bytes that are Base64-encoded.
	// All nodes within a cluster must share the same encryption key to communicate.
	encryptConfigs = `
  "encrypt": "%s",
`

	tlsConfigs = `
  "key_file": "%s",
  "cert_file": "%s",
  "ca_file": "%s",
  "ports": {
    "https": %d
  },
  "verify_incoming": true,
  "verify_incoming_rpc": true,
  "verify_incoming_https": true,
  "verify_outgoing": true,
`
)
