package cascatalog

import (
	"fmt"

	"github.com/cloudstax/openmanage/catalog"
	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/manage"
	"github.com/cloudstax/openmanage/utils"
)

const (
	// ContainerImage is the main PostgreSQL running container.
	ContainerImage = common.ContainerNamePrefix + "cassandra:" + common.Version

	intraNodePort    = 7000
	tlsIntraNodePort = 7001
	jmxPort          = 7199
	cqlPort          = 9042
	thriftPort       = 9160

	yamlConfFileName   = "cassandra.yaml"
	rackdcConfFileName = "cassandra-rackdc.properties"
)

// The default Cassandra catalog service. By default,
// 1) Have equal number of nodes on 3 availability zones.
// 2) Listen on the standard ports, 7000 7001 7199 9042 9160.

// GenDefaultCreateServiceRequest returns the default service creation request.
func GenDefaultCreateServiceRequest(region string, azs []string,
	cluster string, service string, replicas int64, volSizeGB int64, res *common.Resources) *manage.CreateServiceRequest {
	// generate service ReplicaConfigs
	replicaCfgs := GenReplicaConfigs(region, cluster, service, azs, replicas)

	portMappings := []common.PortMapping{
		{ContainerPort: intraNodePort, HostPort: intraNodePort},
		{ContainerPort: tlsIntraNodePort, HostPort: tlsIntraNodePort},
		{ContainerPort: jmxPort, HostPort: jmxPort},
		{ContainerPort: cqlPort, HostPort: cqlPort},
		{ContainerPort: thriftPort, HostPort: thriftPort},
	}

	return &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     cluster,
			ServiceName: service,
		},

		Resource: res,

		ContainerImage: ContainerImage,
		Replicas:       replicas,
		VolumeSizeGB:   volSizeGB,
		ContainerPath:  common.DefaultContainerMountPath,
		PortMappings:   portMappings,

		RegisterDNS:    true,
		ReplicaConfigs: replicaCfgs,
	}
}

// GenReplicaConfigs generates the replica configs.
func GenReplicaConfigs(region string, cluster string, service string, azs []string, replicas int64) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)
	seedMember := utils.GenServiceMemberName(service, 0)
	seedHost := dns.GenDNSName(seedMember, domain)

	replicaCfgs := make([]*manage.ReplicaConfig, replicas)
	for i := 0; i < int(replicas); i++ {
		// create the sys.conf file
		member := utils.GenServiceMemberName(service, int64(i))
		sysCfg := catalog.CreateSysConfigFile(member)

		// create the cassandra.yaml file
		memberHost := dns.GenDNSName(member, domain)
		customContent := fmt.Sprintf(yamlConfigs, cluster, seedHost, memberHost, memberHost, memberHost, memberHost)
		yamlCfg := &manage.ReplicaConfigFile{
			FileName: yamlConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  customContent + yamlDefaultConfigs,
		}

		index := i % len(azs)
		content := fmt.Sprintf(rackdcProps, region, azs[index])
		rackdcCfg := &manage.ReplicaConfigFile{
			FileName: rackdcConfFileName,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		configs := []*manage.ReplicaConfigFile{sysCfg, yamlCfg, rackdcCfg}
		replicaCfg := &manage.ReplicaConfig{Zone: azs[index], Configs: configs}
		replicaCfgs[i] = replicaCfg
	}
	return replicaCfgs
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
commitlog_directory: /data/commitlog
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
authenticator: AllowAllAuthenticator
authorizer: AllowAllAuthorizer
role_manager: CassandraRoleManager
roles_validity_in_ms: 2000
permissions_validity_in_ms: 2000
credentials_validity_in_ms: 2000
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
)