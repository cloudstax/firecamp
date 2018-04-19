package logstashcatalog

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

const (
	defaultVersion = "5.6"
	// ContainerImage is the main running container.
	ContainerImage = common.ContainerNamePrefix + "logstash:" + defaultVersion
	// InputCouchDBContainerImage is the container image for Logstash with the couchdb input plugin
	InputCouchDBContainerImage = common.ContainerNamePrefix + "logstash-input-couchdb:" + defaultVersion

	// DefaultHeapMB is the default reserved memory size for Logstash
	DefaultHeapMB = 2048

	QueueTypeMemory  = "memory"
	QueueTypePersist = "persisted"

	// https://www.elastic.co/guide/en/logstash/5.6/settings.html
	httpPort  = 9600
	beatsPort = 5044

	pipelineConfigFileName = "logstash.conf"
)

// ValidateRequest checks if the request is valid
func ValidateRequest(r *manage.CatalogCreateLogstashRequest) error {
	if r.Options.ContainerImage != ContainerImage && r.Options.ContainerImage != InputCouchDBContainerImage {
		return errors.New("Not supported container image")
	}
	if r.Options.QueueType != QueueTypeMemory && r.Options.QueueType != QueueTypePersist {
		return errors.New("Please specify memory or persisted queue type")
	}
	if len(r.Options.PipelineConfigs) == 0 {
		return errors.New("Please specify the pipeline configs")
	}
	if r.Options.PipelineOutputWorkers > r.Options.PipelineWorkers {
		return errors.New("The pipeline output workers should not be larger than the pipeline workers")
	}
	return nil
}

// GenDefaultCreateServiceRequest returns the default service creation request.
func GenDefaultCreateServiceRequest(platform string, region string, azs []string, cluster string,
	service string, res *common.Resources, opts *manage.CatalogLogstashOptions) *manage.CreateServiceRequest {
	// generate service configs
	serviceCfgs := genServiceConfigs(platform, opts)

	// generate member ReplicaConfigs
	replicaCfgs := genReplicaConfigs(platform, cluster, service, azs, opts)

	portMappings := []common.PortMapping{
		{ContainerPort: httpPort, HostPort: httpPort, IsServicePort: true},
		{ContainerPort: beatsPort, HostPort: beatsPort},
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
			ServiceType: common.ServiceTypeStateful,
		},

		Resource: &common.Resources{
			MaxCPUUnits:     res.MaxCPUUnits,
			ReserveCPUUnits: res.ReserveCPUUnits,
			MaxMemMB:        res.MaxMemMB,
			ReserveMemMB:    reserveMemMB,
		},

		CatalogServiceType: common.CatalogService_Logstash,

		ContainerImage: opts.ContainerImage,
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
func genServiceConfigs(platform string, opts *manage.CatalogLogstashOptions) []*manage.ConfigFileContent {
	// create the service.conf file
	content := fmt.Sprintf(servicefileContent, platform, opts.HeapSizeMB, opts.ContainerImage,
		opts.QueueType, strconv.FormatBool(opts.EnableDeadLetterQueue), opts.PipelineWorkers,
		opts.PipelineOutputWorkers, opts.PipelineBatchSize, opts.PipelineBatchDelay)
	serviceCfg := &manage.ConfigFileContent{
		FileName: catalog.SERVICE_FILE_NAME,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	// create the pipeline config file
	plCfg := &manage.ConfigFileContent{
		FileName: pipelineConfigFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  opts.PipelineConfigs,
	}

	return []*manage.ConfigFileContent{serviceCfg, plCfg}
}

// genReplicaConfigs generates the replica configs.
func genReplicaConfigs(platform string, cluster string, service string, azs []string, opts *manage.CatalogLogstashOptions) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)

	replicaCfgs := make([]*manage.ReplicaConfig, opts.Replicas)
	for i := int64(0); i < opts.Replicas; i++ {
		azIndex := int(i) % len(azs)
		az := azs[azIndex]

		// create the member.conf file
		member := utils.GenServiceMemberName(service, i)
		memberHost := dns.GenDNSName(member, domain)

		bind := memberHost
		if platform == common.ContainerPlatformSwarm {
			bind = catalog.BindAllIP
		}

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
HEAP_SIZE_MB=%d
CONTAINER_IMAGE=%s
QUEUE_TYPE=%s
ENABLE_DEAD_LETTER_QUEUE=%s
PIPELINE_WORKERS=%d
PIPELINE_OUTPUT_WORKERS=%d
PIPELINE_BATCH_SIZE=%d
PIPELINE_BATCH_DELAY=%d
`

	memberfileContent = `
AVAILABILITY_ZONE=%s
MEMBER_NAME=%s
SERVICE_MEMBER=%s
BIND_IP=%s
`
)
