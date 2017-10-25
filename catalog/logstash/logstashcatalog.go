package logstashcatalog

import (
	"errors"
	"fmt"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

const (
	// ContainerImage is the main running container.
	ContainerImage = common.ContainerNamePrefix + "logstash:" + common.Version
	// InputCouchDBContainerImage is the container image for Logstash with the couchdb input plugin
	InputCouchDBContainerImage = common.ContainerNamePrefix + "logstash-input-couchdb:" + common.Version

	// DefaultReserveMemoryMB is the default reserved memory size for Kibana
	DefaultReserveMemoryMB = 2048

	QueueTypeMemory  = "memory"
	QueueTypePersist = "persisted"

	// https://www.elastic.co/guide/en/logstash/5.6/settings.html
	httpPort  = 9600
	beatsPort = 5044

	lsConfFileName         = "logstash.yml"
	jvmConfFileName        = "jvm.options"
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
	// generate service ReplicaConfigs
	replicaCfgs := GenReplicaConfigs(platform, cluster, service, azs, res, opts)

	portMappings := []common.PortMapping{
		{ContainerPort: httpPort, HostPort: httpPort},
		{ContainerPort: beatsPort, HostPort: beatsPort},
	}

	return &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     cluster,
			ServiceName: service,
		},

		Resource: res,

		ContainerImage: opts.ContainerImage,
		Replicas:       opts.Replicas,
		VolumeSizeGB:   opts.VolumeSizeGB,
		ContainerPath:  common.DefaultContainerMountPath,
		PortMappings:   portMappings,

		RegisterDNS:    true,
		ReplicaConfigs: replicaCfgs,
	}
}

// GenReplicaConfigs generates the replica configs.
func GenReplicaConfigs(platform string, cluster string, service string, azs []string, res *common.Resources, opts *manage.CatalogLogstashOptions) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)

	replicaCfgs := make([]*manage.ReplicaConfig, opts.Replicas)
	for i := int64(0); i < opts.Replicas; i++ {
		// create the sys.conf file
		member := utils.GenServiceMemberName(service, i)
		memberHost := dns.GenDNSName(member, domain)
		sysCfg := catalog.CreateSysConfigFile(platform, memberHost)

		// create the logstash.yml file
		bind := memberHost
		if platform == common.ContainerPlatformSwarm {
			bind = catalog.BindAllIP
		}
		enableDLQStr := "false"
		if opts.EnableDeadLetterQueue {
			enableDLQStr = "true"
		}
		content := fmt.Sprintf(lsConfigs, member, bind, opts.QueueType, enableDLQStr)

		if opts.PipelineWorkers > 0 {
			content += fmt.Sprintf(plWorkersConfig, opts.PipelineWorkers)
		}
		if opts.PipelineOutputWorkers > 0 {
			content += fmt.Sprintf(plOutputWorkerConfig, opts.PipelineOutputWorkers)
		}
		if opts.PipelineBatchSize > 0 {
			content += fmt.Sprintf(plBatchSizeConfig, opts.PipelineBatchSize)
		}
		if opts.PipelineBatchDelay > 0 {
			content += fmt.Sprintf(plBatchDelayConfig, opts.PipelineBatchDelay)
		}

		lsCfg := &manage.ReplicaConfigFile{
			FileName: lsConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		// create the jvm.options file
		content = fmt.Sprintf(jvmHeapConfigs, res.ReserveMemMB, res.ReserveMemMB)
		content += jvmConfigs
		jvmCfg := &manage.ReplicaConfigFile{
			FileName: jvmConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		// create the pipeline config file
		plCfg := &manage.ReplicaConfigFile{
			FileName: pipelineConfigFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  opts.PipelineConfigs,
		}

		configs := []*manage.ReplicaConfigFile{sysCfg, lsCfg, jvmCfg, plCfg}

		azIndex := int(i) % len(azs)
		az := azs[azIndex]
		replicaCfg := &manage.ReplicaConfig{Zone: az, Configs: configs}
		replicaCfgs[i] = replicaCfg
	}
	return replicaCfgs
}

const (
	// https://www.elastic.co/guide/en/logstash/5.6/production.html
	// https://github.com/elastic/logstash-docker/tree/5.6/build/kibana/config
	lsConfigs = `
node.name: %s
http.host: %s
http.port: 9600

xpack.monitoring.enabled: false

path.data: /data/logstash
path.config: /usr/share/logstash/pipeline

queue.type: %s
dead_letter_queue.enable: %s
`
	plWorkersConfig = `
pipeline.workers: %d
`
	plOutputWorkerConfig = `
pipeline.output.workers: %d
`
	plBatchSizeConfig = `
pipeline.batch.size: %d
`
	plBatchDelayConfig = `
pipeline.batch.delay: %d
`

	jvmHeapConfigs = `
-Xms%dm
-Xmx%dm
`

	jvmConfigs = `
## GC configuration
-XX:+UseParNewGC
-XX:+UseConcMarkSweepGC
-XX:CMSInitiatingOccupancyFraction=75
-XX:+UseCMSInitiatingOccupancyOnly

## optimizations

# disable calls to System#gc
-XX:+DisableExplicitGC

## basic

# set the I/O temp directory
#-Djava.io.tmpdir=$HOME

# set to headless, just in case
-Djava.awt.headless=true

# ensure UTF-8 encoding by default (e.g. filenames)
-Dfile.encoding=UTF-8

# use our provided JNA always versus the system one
#-Djna.nosys=true

# Turn on JRuby invokedynamic
-Djruby.compile.invokedynamic=true
# Force Compilation
-Djruby.jit.threshold=0

## heap dumps

# generate a heap dump when an allocation from the Java heap fails
# heap dumps are created in the working directory of the JVM
-XX:+HeapDumpOnOutOfMemoryError

# specify an alternative path for heap dumps
# ensure the directory exists and has sufficient space
-XX:HeapDumpPath=/data/heapdump.hprof

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

# Entropy source for randomness
-Djava.security.egd=file:/dev/urandom
`
)
