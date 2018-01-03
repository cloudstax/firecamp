package cascatalog

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/log"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
	"github.com/golang/glog"
)

const (
	defaultVersion = "3.11"
	// ContainerImage is the main Cassandra running container.
	ContainerImage = common.ContainerNamePrefix + "cassandra:" + defaultVersion
	// InitContainerImage initializes the Cassandra cluster.
	InitContainerImage = common.ContainerNamePrefix + "cassandra-init:" + defaultVersion

	// DefaultHeapMB is the default Cassandra JVM heap size.
	// https://docs.datastax.com/en/cassandra/2.1/cassandra/operations/ops_tune_jvm_c.html
	DefaultHeapMB = 8192
	// MaxHeapMB is the max Cassandra JVM heap size. Cassandra recommends no more than 14GB
	MaxHeapMB = 14 * 1024
	// MinHeapMB is the minimal JVM heap size. If heap < 1024, jvm may stall long time at gc.
	MinHeapMB = 1024
	// DefaultJmxRemoteUser is the default jmx remote user.
	DefaultJmxRemoteUser = "cassandrajmx"

	intraNodePort    = 7000
	tlsIntraNodePort = 7001
	jmxPort          = 7199
	cqlPort          = 9042
	thriftPort       = 9160

	yamlConfFileName            = "cassandra.yaml"
	rackdcConfFileName          = "cassandra-rackdc.properties"
	jvmConfFileName             = "jvm.options"
	logConfFileName             = "logback.xml"
	jmxRemotePasswdConfFileName = "jmxremote.password"

	jmxRemotePasswdConfFileMode = 0400
)

// The default Cassandra catalog service. By default,
// 1) Have equal number of nodes on 3 availability zones.
// 2) Listen on the standard ports, 7000 7001 7199 9042 9160.

// ValidateRequest checks if the create request is valid
func ValidateRequest(req *manage.CatalogCreateCassandraRequest) error {
	if req.Options.HeapSizeMB <= 0 {
		return errors.New("heap size should be larger than 0")
	}
	if req.Options.HeapSizeMB > MaxHeapMB {
		return errors.New("max heap size is 14GB")
	}
	if req.Options.JournalVolume == nil {
		return errors.New("cassandra should have separate volume for journal")
	}
	if req.Options.Replicas == 2 {
		return errors.New("2 nodes are not allowed")
	}

	return nil
}

// ValidateUpdateRequest checks if the update request is valid
func ValidateUpdateRequest(req *manage.CatalogUpdateCassandraRequest) error {
	if req.HeapSizeMB <= 0 {
		return errors.New("heap size should be larger than 0")
	}
	if req.HeapSizeMB > MaxHeapMB {
		return errors.New("max heap size is 14GB")
	}
	return nil
}

// GenDefaultCreateServiceRequest returns the default service creation request.
func GenDefaultCreateServiceRequest(platform string, region string, azs []string,
	cluster string, service string, opts *manage.CatalogCassandraOptions, res *common.Resources) (*manage.CreateServiceRequest, error) {
	// generate service ReplicaConfigs
	if len(opts.JmxRemoteUser) == 0 {
		opts.JmxRemoteUser = DefaultJmxRemoteUser
	}
	if len(opts.JmxRemotePasswd) == 0 {
		opts.JmxRemotePasswd = utils.GenUUID()
	}
	replicaCfgs := GenReplicaConfigs(platform, region, cluster, service, azs, opts)

	portMappings := []common.PortMapping{
		{ContainerPort: intraNodePort, HostPort: intraNodePort},
		{ContainerPort: tlsIntraNodePort, HostPort: tlsIntraNodePort},
		{ContainerPort: jmxPort, HostPort: jmxPort},
		{ContainerPort: cqlPort, HostPort: cqlPort},
		{ContainerPort: thriftPort, HostPort: thriftPort},
	}

	reserveMemMB := res.ReserveMemMB
	if res.ReserveMemMB < opts.HeapSizeMB {
		reserveMemMB = opts.HeapSizeMB
	}

	userAttr := &common.CasUserAttr{
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
			ServiceType: common.CatalogService_Cassandra,
			AttrBytes:   b,
		},
	}
	if opts.JournalVolume != nil {
		req.JournalVolume = opts.JournalVolume
		req.JournalContainerPath = common.DefaultJournalVolumeContainerMountPath
	}
	return req, nil
}

// GenReplicaConfigs generates the replica configs.
func GenReplicaConfigs(platform string, region string, cluster string, service string, azs []string, opts *manage.CatalogCassandraOptions) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)
	seeds := genSeedHosts(int64(len(azs)), opts.Replicas, service, domain)

	replicaCfgs := make([]*manage.ReplicaConfig, opts.Replicas)
	for i := int64(0); i < opts.Replicas; i++ {
		member := utils.GenServiceMemberName(service, i)
		memberHost := dns.GenDNSName(member, domain)

		// create the sys.conf file
		sysCfg := catalog.CreateSysConfigFile(platform, memberHost)

		// create the cassandra.yaml file
		customContent := ""
		if platform == common.ContainerPlatformECS {
			// ECS allows to use the host network. So we use the host network for better performance.
			// Cassandra could listen on the member's dnsname.
			customContent = fmt.Sprintf(yamlConfigs, cluster, seeds, memberHost, memberHost, memberHost, memberHost)
		} else {
			// leave listen_address blank and set rpc_address to 0.0.0.0 is for docker swarm.
			// docker swarm does not allow service to use "host" network. So the container
			// could not listen on the member's dnsname.
			// For the blank listen_address, Cassandra will use InetAddress.getLocalHost().
			customContent = fmt.Sprintf(yamlConfigs, cluster, seeds, "", memberHost, catalog.BindAllIP, memberHost)
		}
		yamlCfg := &manage.ReplicaConfigFile{
			FileName: yamlConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  customContent + yamlSecurityConfigs + yamlDefaultConfigs,
		}

		index := int(i) % len(azs)
		content := fmt.Sprintf(rackdcProps, region, azs[index])
		rackdcCfg := &manage.ReplicaConfigFile{
			FileName: rackdcConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		// create the jvm.options file
		content = fmt.Sprintf(jvmHeapConfigs, opts.HeapSizeMB, opts.HeapSizeMB)
		content += jvmConfigs
		jvmCfg := &manage.ReplicaConfigFile{
			FileName: jvmConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		// create the logback.xml file
		logCfg := &manage.ReplicaConfigFile{
			FileName: logConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  logConfContent,
		}

		// create the jmxremote.password file
		jmxCfg := &manage.ReplicaConfigFile{
			FileName: jmxRemotePasswdConfFileName,
			FileMode: jmxRemotePasswdConfFileMode,
			Content:  fmt.Sprintf("%s %s\n", opts.JmxRemoteUser, opts.JmxRemotePasswd),
		}

		configs := []*manage.ReplicaConfigFile{sysCfg, yamlCfg, rackdcCfg, jvmCfg, logCfg, jmxCfg}
		replicaCfg := &manage.ReplicaConfig{Zone: azs[index], MemberName: member, Configs: configs}
		replicaCfgs[i] = replicaCfg
	}
	return replicaCfgs
}

func genSeedHosts(azCount int64, replicas int64, service string, domain string) string {
	seedCount := azCount
	if seedCount > replicas {
		seedCount = replicas
	}
	seedHostSep := ","
	seedHost := ""
	for i := int64(0); i < seedCount; i++ {
		member := utils.GenServiceMemberName(service, i)
		host := dns.GenDNSName(member, domain)
		if i == (seedCount - 1) {
			seedHost += host
		} else {
			seedHost += host + seedHostSep
		}
	}
	return seedHost
}

// GenDefaultInitTaskRequest returns the default Cassandra init task request.
func GenDefaultInitTaskRequest(req *manage.ServiceCommonRequest, logConfig *cloudlog.LogConfig,
	serviceUUID string, manageurl string) *containersvc.RunTaskOptions {
	envkvs := GenInitTaskEnvKVPairs(req.Region, req.Cluster, req.ServiceName, manageurl)

	commonOpts := &containersvc.CommonOptions{
		Cluster:        req.Cluster,
		ServiceName:    req.ServiceName,
		ServiceUUID:    serviceUUID,
		ContainerImage: InitContainerImage,
		Resource: &common.Resources{
			MaxCPUUnits:     common.DefaultMaxCPUUnits,
			ReserveCPUUnits: common.DefaultReserveCPUUnits,
			MaxMemMB:        common.DefaultMaxMemoryMB,
			ReserveMemMB:    common.DefaultReserveMemoryMB,
		},
		LogConfig: logConfig,
	}

	return &containersvc.RunTaskOptions{
		Common:   commonOpts,
		TaskType: common.TaskTypeInit,
		Envkvs:   envkvs,
	}
}

// GenInitTaskEnvKVPairs generates the environment key-values for the init task.
func GenInitTaskEnvKVPairs(region string, cluster string, service string, manageurl string) []*common.EnvKeyValuePair {
	kvregion := &common.EnvKeyValuePair{Name: common.ENV_REGION, Value: region}
	kvcluster := &common.EnvKeyValuePair{Name: common.ENV_CLUSTER, Value: cluster}
	kvmgtserver := &common.EnvKeyValuePair{Name: common.ENV_MANAGE_SERVER_URL, Value: manageurl}
	kvop := &common.EnvKeyValuePair{Name: common.ENV_OP, Value: manage.CatalogSetServiceInitOp}

	kvservice := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_NAME, Value: service}
	kvsvctype := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_TYPE, Value: common.CatalogService_Cassandra}

	domain := dns.GenDefaultDomainName(cluster)
	dnsname := dns.GenDNSName(utils.GenServiceMemberName(service, 0), domain)
	kvnode := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_NODE, Value: dnsname}

	return []*common.EnvKeyValuePair{kvregion, kvcluster, kvmgtserver, kvop, kvservice, kvsvctype, kvnode}
}

// IsJvmConfFile checks if the file is jvm conf file
func IsJvmConfFile(filename string) bool {
	return filename == jvmConfFileName
}

// NewJVMConfContent returns the new jvm.options file content
func NewJVMConfContent(heapSizeMB int64) string {
	return fmt.Sprintf(jvmHeapConfigs, heapSizeMB, heapSizeMB) + jvmConfigs
}

// IsJmxConfFile checks if the file is jmx conf file
func IsJmxConfFile(filename string) bool {
	return filename == jmxRemotePasswdConfFileName
}

// NewJmxConfContent returns the new jmxremote.password file content
func NewJmxConfContent(jmxUser string, jmxPasswd string) string {
	return fmt.Sprintf("%s %s\n", jmxUser, jmxPasswd)
}

const (
	rackdcProps = `
dc=%s
rack=%s
`

	yamlConfigs = `
# Cassandra storage config YAML
cluster_name: '%s'

hints_directory: /data/hints
data_file_directories:
    - /data/data
commitlog_directory: /journal
saved_caches_directory: /data/saved_caches

seed_provider:
    - class_name: org.apache.cassandra.locator.SimpleSeedProvider
      parameters:
          - seeds: "%s"

listen_address: %s
broadcast_address: %s
rpc_address: %s
broadcast_rpc_address: %s

endpoint_snitch: GossipingPropertyFileSnitch

`

	yamlSecurityConfigs = `
authenticator: PasswordAuthenticator
authorizer: CassandraAuthorizer
role_manager: CassandraRoleManager
roles_validity_in_ms: 10000
roles_update_interval_in_ms: 4000
permissions_validity_in_ms: 10000
permissions_update_interval_in_ms: 4000
credentials_validity_in_ms: 10000
credentials_update_interval_in_ms: 4000
`

	yamlDefaultConfigs = `
# The rest configs are the same with default cassandra.yaml
num_tokens: 256
hinted_handoff_enabled: true
max_hint_window_in_ms: 10800000 # 3 hours
hinted_handoff_throttle_in_kb: 1024
max_hints_delivery_threads: 2
hints_flush_period_in_ms: 10000
max_hints_file_size_in_mb: 128
batchlog_replay_throttle_in_kb: 1024
partitioner: org.apache.cassandra.dht.Murmur3Partitioner
cdc_enabled: false
disk_failure_policy: stop
commit_failure_policy: stop
prepared_statements_cache_size_mb:
thrift_prepared_statements_cache_size_mb:
key_cache_size_in_mb:
key_cache_save_period: 14400
row_cache_size_in_mb: 0
row_cache_save_period: 0
counter_cache_size_in_mb:
counter_cache_save_period: 7200
commitlog_sync: periodic
commitlog_sync_period_in_ms: 10000
commitlog_segment_size_in_mb: 32
concurrent_reads: 32
concurrent_writes: 32
concurrent_counter_writes: 32
concurrent_materialized_view_writes: 32
memtable_allocation_type: heap_buffers
index_summary_capacity_in_mb:
index_summary_resize_interval_in_minutes: 60
trickle_fsync: false
trickle_fsync_interval_in_kb: 10240
storage_port: 7000
ssl_storage_port: 7001
start_native_transport: true
native_transport_port: 9042
start_rpc: false
rpc_port: 9160
rpc_keepalive: true
rpc_server_type: sync
thrift_framed_transport_size_in_mb: 15
incremental_backups: false
snapshot_before_compaction: false
auto_snapshot: true
column_index_size_in_kb: 64
column_index_cache_size_in_kb: 2
compaction_throughput_mb_per_sec: 16
sstable_preemptive_open_interval_in_mb: 50
read_request_timeout_in_ms: 5000
range_request_timeout_in_ms: 10000
write_request_timeout_in_ms: 2000
counter_write_request_timeout_in_ms: 5000
cas_contention_timeout_in_ms: 1000
truncate_request_timeout_in_ms: 60000
request_timeout_in_ms: 10000
slow_query_log_timeout_in_ms: 500
cross_node_timeout: false
dynamic_snitch_update_interval_in_ms: 100
dynamic_snitch_reset_interval_in_ms: 600000
dynamic_snitch_badness_threshold: 0.1
request_scheduler: org.apache.cassandra.scheduler.NoScheduler
server_encryption_options:
    internode_encryption: none
    keystore: conf/.keystore
    keystore_password: cassandra
    truststore: conf/.truststore
    truststore_password: cassandra
client_encryption_options:
    enabled: false
    optional: false
    keystore: conf/.keystore
    keystore_password: cassandra
internode_compression: dc
inter_dc_tcp_nodelay: false
tracetype_query_ttl: 86400
tracetype_repair_ttl: 604800
enable_user_defined_functions: false
enable_scripted_user_defined_functions: false
windows_timer_interval: 1
transparent_data_encryption_options:
    enabled: false
    chunk_length_kb: 64
    cipher: AES/CBC/PKCS5Padding
    key_alias: testing:1
    key_provider:
      - class_name: org.apache.cassandra.security.JKSKeyProvider
        parameters:
          - keystore: conf/.keystore
            keystore_password: cassandra
            store_type: JCEKS
            key_password: cassandra
tombstone_warn_threshold: 1000
tombstone_failure_threshold: 100000
batch_size_warn_threshold_in_kb: 5
batch_size_fail_threshold_in_kb: 50
unlogged_batch_across_partitions_warn_threshold: 10
compaction_large_partition_warning_threshold_mb: 100
gc_warn_threshold_in_ms: 1000
back_pressure_enabled: false
back_pressure_strategy:
    - class_name: org.apache.cassandra.net.RateBasedBackPressure
      parameters:
        - high_ratio: 0.90
          factor: 5
          flow: FAST
`

	logConfContent = `
<configuration scan="true">
  <jmxConfigurator />
  <!-- STDOUT console appender to stdout (INFO level) -->

  <appender name="STDOUT" class="ch.qos.logback.core.ConsoleAppender">
    <filter class="ch.qos.logback.classic.filter.ThresholdFilter">
      <level>INFO</level>
    </filter>
    <encoder>
      <pattern>%-5level [%thread] %date{ISO8601} %F:%L - %msg%n</pattern>
    </encoder>
  </appender>

  <root level="INFO">
    <appender-ref ref="STDOUT" />
  </root>

  <logger name="org.apache.cassandra" level="INFO"/>
  <logger name="com.thinkaurelius.thrift" level="ERROR"/>
</configuration>
`

	jvmHeapConfigs = `
# HEAP SETTINGS #

# Heap size is automatically calculated by cassandra-env based on this
# formula: max(min(1/2 ram, 1024MB), min(1/4 ram, 8GB))
# That is:
# - calculate 1/2 ram and cap to 1024MB
# - calculate 1/4 ram and cap to 8192MB
# - pick the max
#
# For production use you may wish to adjust this for your environment.
# If that's the case, uncomment the -Xmx and Xms options below to override the
# automatic calculation of JVM heap memory.
#
# It is recommended to set min (-Xms) and max (-Xmx) heap sizes to
# the same value to avoid stop-the-world GC pauses during resize, and
# so that we can lock the heap in memory on startup to prevent any
# of it from being swapped out.
-Xms%dM
-Xmx%dM

# Young generation size is automatically calculated by cassandra-env
# based on this formula: min(100 * num_cores, 1/4 * heap size)
#
# The main trade-off for the young generation is that the larger it
# is, the longer GC pause times will be. The shorter it is, the more
# expensive GC will be (usually).
#
# It is not recommended to set the young generation size if using the
# G1 GC, since that will override the target pause-time goal.
# More info: http://www.oracle.com/technetwork/articles/java/g1gc-1984535.html
#
# The example below assumes a modern 8-core+ machine for decent
# times. If in doubt, and if you do not particularly want to tweak, go
# 100 MB per physical CPU core.
#-Xmn800M
`

	// jvmConfigs includes other default jvm configs
	jvmConfigs = `
# refer to the default jvm setting for the detail comments

# GENERAL JVM SETTINGS #

# enable assertions. highly suggested for correct application functionality.
-ea

# enable thread priorities, primarily so we can give periodic tasks
# a lower priority to avoid interfering with client workload
-XX:+UseThreadPriorities

# allows lowering thread priority without being root on linux - probably
# not necessary on Windows but doesn't harm anything.
# see http://tech.stolsvik.com/2010/01/linux-java-thread-priorities-workar
-XX:ThreadPriorityPolicy=42

# Enable heap-dump if there's an OOM
-XX:+HeapDumpOnOutOfMemoryError

# Per-thread stack size.
-Xss256k

# Larger interned string table, for gossip's benefit (CASSANDRA-6410)
-XX:StringTableSize=1000003

# Make sure all memory is faulted and zeroed on startup.
# This helps prevent soft faults in containers and makes
# transparent hugepage allocation more effective.
-XX:+AlwaysPreTouch

# Disable biased locking as it does not benefit Cassandra.
-XX:-UseBiasedLocking

# Enable thread-local allocation blocks and allow the JVM to automatically
# resize them at runtime.
-XX:+UseTLAB
-XX:+ResizeTLAB
-XX:+UseNUMA

# http://www.evanjones.ca/jvm-mmap-pause.html
-XX:+PerfDisableSharedMem
-Djava.net.preferIPv4Stack=true

#################
#  GC SETTINGS  #
#################

### CMS Settings

-XX:+UseParNewGC
-XX:+UseConcMarkSweepGC
-XX:+CMSParallelRemarkEnabled
-XX:SurvivorRatio=8
-XX:MaxTenuringThreshold=1
-XX:CMSInitiatingOccupancyFraction=75
-XX:+UseCMSInitiatingOccupancyOnly
-XX:CMSWaitDuration=10000
-XX:+CMSParallelInitialMarkEnabled
-XX:+CMSEdenChunksRecordAlways
# some JVMs will fill up their heap when accessed via JMX, see CASSANDRA-6541
-XX:+CMSClassUnloadingEnabled

### GC logging options -- uncomment to enable

-XX:+PrintGCDetails
-XX:+PrintGCDateStamps
-XX:+PrintHeapAtGC
-XX:+PrintTenuringDistribution
-XX:+PrintGCApplicationStoppedTime
-XX:+PrintPromotionFailure
#-XX:PrintFLSStatistics=1
-Xloggc:/data/gc-%t.log
-XX:+UseGCLogFileRotation
-XX:NumberOfGCLogFiles=10
-XX:GCLogFileSize=10M
`
)
