package pgcatalog

import (
	"fmt"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

const (
	// ContainerImage is the main PostgreSQL running container.
	ContainerImage = common.ContainerNamePrefix + "postgres:" + common.Version
	defaultPort    = 5432

	containerRolePrimary = "primary"
	containerRoleStandby = "standby"
	serviceConfFileName  = "service.conf"
	postgresConfFileName = "postgresql.conf"
	pgHbaConfFileName    = "pg_hba.conf"
	recoveryConfFileName = "recovery.conf"
)

// The default PostgreSQL catalog service. By default,
// 1) One PostgreSQL has 1 primary and 2 secondary replicas across 3 availability zones.
// 2) Listen on the standard port, 5432.

// GenDefaultCreateServiceRequest returns the default PostgreSQL creation request.
func GenDefaultCreateServiceRequest(platform string, region string, azs []string,
	cluster string, service string, replicas int64, volSizeGB int64,
	adminPasswd string, replUser string, replPasswd string, res *common.Resources) *manage.CreateServiceRequest {
	// generate service ReplicaConfigs
	replicaCfgs := GenReplicaConfigs(platform, cluster, service, azs, replicas, defaultPort, adminPasswd, replUser, replPasswd)

	portmapping := common.PortMapping{
		ContainerPort: defaultPort,
		HostPort:      defaultPort,
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
		PortMappings:   []common.PortMapping{portmapping},

		RegisterDNS:    true,
		ReplicaConfigs: replicaCfgs,
	}
}

// GenReplicaConfigs generates the replica configs.
// Note: if the number of availability zones is less than replicas, 2 or more replicas will run on the same zone.
func GenReplicaConfigs(platform string, cluster string, service string, azs []string, replicas int64,
	port int64, adminPasswd string, replUser string, replPasswd string) []*manage.ReplicaConfig {
	replicaCfgs := make([]*manage.ReplicaConfig, replicas)

	// generate the primary configs
	domain := dns.GenDefaultDomainName(cluster)
	primaryMember := utils.GenServiceMemberName(service, 0)
	primaryHost := dns.GenDNSName(primaryMember, domain)
	replicaCfgs[0] = genPrimaryConfig(platform, azs[0], primaryHost, port, adminPasswd, replUser, replPasswd)

	// generate the standby configs.
	// TODO support cascading replication, specially for cross-region replication.
	for i := 1; i < int(replicas); i++ {
		index := i % len(azs)
		member := utils.GenServiceMemberName(service, int64(i))
		memberHost := dns.GenDNSName(member, domain)
		replicaCfgs[i] = genStandbyConfig(platform, azs[index], memberHost, primaryHost, port, adminPasswd, replUser, replPasswd)
	}

	return replicaCfgs
}

func genPrimaryConfig(platform string, az string, primaryHost string, port int64,
	adminPasswd string, replUser string, replPasswd string) *manage.ReplicaConfig {
	// create the sys.conf file
	cfg0 := catalog.CreateSysConfigFile(platform, primaryHost)

	// create service.conf file
	content := fmt.Sprintf(serviceConf, containerRolePrimary, primaryHost, port, adminPasswd, replUser, replPasswd)
	cfg1 := &manage.ReplicaConfigFile{
		FileName: serviceConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	// create postgres.conf
	cfg2 := &manage.ReplicaConfigFile{
		FileName: postgresConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  primaryPostgresConf,
	}

	// create pg_hba.conf
	content = fmt.Sprintf(primaryPgHbaConf, replUser)
	cfg3 := &manage.ReplicaConfigFile{
		FileName: pgHbaConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	configs := []*manage.ReplicaConfigFile{cfg0, cfg1, cfg2, cfg3}
	return &manage.ReplicaConfig{Zone: az, Configs: configs}
}

func genStandbyConfig(platform, az string, memberHost string, primaryHost string, port int64,
	adminPasswd string, replUser string, replPasswd string) *manage.ReplicaConfig {
	// create the sys.conf file
	cfg0 := catalog.CreateSysConfigFile(platform, memberHost)

	// create service.conf file
	content := fmt.Sprintf(serviceConf, containerRoleStandby, primaryHost, port, adminPasswd, replUser, replPasswd)
	cfg1 := &manage.ReplicaConfigFile{
		FileName: serviceConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	// create postgres.conf
	cfg2 := &manage.ReplicaConfigFile{
		FileName: postgresConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  standbyPostgresConf,
	}

	// create pg_hba.conf
	cfg3 := &manage.ReplicaConfigFile{
		FileName: pgHbaConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  standbyPgHbaConf,
	}

	// create recovery.conf
	primaryConnInfo := fmt.Sprintf(standbyPrimaryConnInfo, primaryHost, port, replUser, replPasswd)
	content = fmt.Sprintf(standbyRecoveryConf, primaryConnInfo, standbyRestoreCmd)
	cfg4 := &manage.ReplicaConfigFile{
		FileName: recoveryConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	configs := []*manage.ReplicaConfigFile{cfg0, cfg1, cfg2, cfg3, cfg4}
	return &manage.ReplicaConfig{Zone: az, Configs: configs}
}

const (
	serviceConf = `
CONTAINER_ROLE=%s
PRIMARY_HOST=%s
LISTEN_PORT=%d
POSTGRES_PASSWORD=%s
REPLICATION_USER=%s
REPLICATION_PASSWORD=%s
`

	primaryPostgresConf = `
listen_addresses = '*'

# To enable read-only queries on a standby server, wal_level must be set to
# "hot_standby".
wal_level = hot_standby

# allow up to 5 standby servers
max_wal_senders = 5

# To prevent the primary server from removing the WAL segments required for
# the standby server before shipping them, set the minimum number of segments
# retained in the pg_xlog directory. At least wal_keep_segments should be
# larger than the number of segments generated between the beginning of
# online-backup and the startup of streaming replication. If you enable WAL
# archiving to an archive directory accessible from the standby, this may
# not be necessary.
wal_keep_segments = 32

# Enable WAL archiving on the primary to an archive directory accessible from
# the standby. If wal_keep_segments is a high enough number to retain the WAL
# segments required for the standby server, this is not necessary.
# TODO enable it with archiving to S3
#archive_mode    = on
#archive_command = 'cp %p /path_to/archive/%f'

log_line_prefix = '%t %c %u %r '
`

	primaryPgHbaConf = `
# TYPE  DATABASE        USER            ADDRESS                 METHOD

# "local" is for Unix domain socket connections only
local   all             all                                     trust

# the application hosts to access PostgreSQL
host    all             all               all                    md5

# the PostgreSQL standby hosts to access PostgreSQL replication
# need to allow all. The primary only gets the standby's ip address,
# then does reverse lookup, which returns EC2's private DNS name.
# The primary has no way to know the DNS name in Route53.
# There is no security concern for this. The EC2's inbound rule will
# limit the source from the specific security group.
host   replication      %s                all                    md5
`

	standbyPostgresConf = `
listen_addresses = '*'

hot_standby = on

log_line_prefix = '%t %c %u %r '
`
	standbyPgHbaConf = `
# TYPE  DATABASE        USER            ADDRESS                 METHOD

# "local" is for Unix domain socket connections only
local   all             all                                     trust

# the application hosts to access PostgreSQL
host    all             all               all                    md5
`

	standbyPrimaryConnInfo = "host=%s port=%d user=%s password=%s"
	standbyRestoreCmd      = `cp /path_to/archive/%f "%p"`
	standbyRecoveryConf    = `
# Specifies whether to start the server as a standby. In streaming replication,
# this parameter must to be set to on.
standby_mode          = 'on'

# Specifies a connection string which is used for the standby server to connect
# with the primary.
primary_conninfo      = '%s'

# Specifies a trigger file whose presence should cause streaming replication to
# end (i.e., failover).
trigger_file = '/data/recovery-trigger'

# Specifies a command to load archive segments from the WAL archive. If
# wal_keep_segments is a high enough number to retain the WAL segments
# required for the standby server, this may not be necessary. But
# a large workload can cause segments to be recycled before the standby
# is fully synchronized, requiring you to start again from a new base backup.
# TODO enable it with archiving to S3
#restore_command = '%s'
`
)
