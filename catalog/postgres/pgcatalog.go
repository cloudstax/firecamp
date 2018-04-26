package pgcatalog

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
	defaultVersion = "9.6"
	// ContainerImage is the main PostgreSQL running container.
	ContainerImage = common.ContainerNamePrefix + "postgres:" + defaultVersion
	// PostGISContainerImage is the container image for PostgreSQL with PostGIS.
	PostGISContainerImage = common.ContainerNamePrefix + "postgres-postgis:" + defaultVersion

	defaultPort = 5432

	containerRolePrimary     = "primary"
	containerRoleStandby     = "standby"
	primaryPGConfFileName    = "primary_postgresql.conf"
	standbyPGConfFileName    = "standby_postgresql.conf"
	primaryPGHbaConfFileName = "primary_pg_hba.conf"
	standbyPGHbaConfFileName = "standby_pg_hba.conf"
	recoveryConfFileName     = "recovery.conf"
)

// The default PostgreSQL catalog service. By default,
// 1) One PostgreSQL has 1 primary and 2 secondary replicas across 3 availability zones.
// 2) Listen on the standard port, 5432.

// ValidateRequest checks if the request is valid
func ValidateRequest(req *manage.CatalogCreatePostgreSQLRequest) error {
	// for now, limit the container image to postgres and postgres-postgis only.
	// after we get more requirements and finalize the design for the custom image, we may remove this check.
	if len(req.Options.ContainerImage) != 0 && req.Options.ContainerImage != ContainerImage && req.Options.ContainerImage != PostGISContainerImage {
		return errors.New("only postgres and postgres-postgis images are supported")
	}
	// use the dedicated volume for the WAL logs could help on performance.
	// But it introduces the potential data consistency risks.
	// When the server crashes, it might happen that some pages in the data
	// files are written only partially (because most disks have a much smaller
	// blocksize than the PostgreSQL page size (which is 8k by default)).
	// If the WAL volume happens to crash, server cannot reply the WAL log,
	// those half-written pages will not be repaired. Would need to restore database
	// from the backups.
	// While, this is probably ok. If WAL log is on the same volume with data, the volume
	// is possible to crash as well. Then we will have to recover database from the backups.
	// https://www.postgresql.org/docs/9.6/static/wal-internals.html
	// https://wiki.postgresql.org/wiki/Installation_and_Administration_Best_practices#WAL_Directory
	// https://www.postgresql.org/message-id/4573FEE0.2010406@logix-tt.com
	if req.Options.JournalVolume == nil {
		return errors.New("postgres should have separate volume for journal")
	}

	return nil
}

// GenDefaultCreateServiceRequest returns the default PostgreSQL creation request.
func GenDefaultCreateServiceRequest(platform string, region string, azs []string,
	cluster string, service string, res *common.Resources, opts *manage.CatalogPostgreSQLOptions) *manage.CreateServiceRequest {
	// generate service configs
	serviceCfgs := genServiceConfigs(platform, cluster, service, azs, defaultPort, opts)

	// generate member ReplicaConfigs
	replicaCfgs := GenReplicaConfigs(platform, cluster, service, azs, defaultPort, opts)

	portmapping := common.PortMapping{
		ContainerPort: defaultPort,
		HostPort:      defaultPort,
		IsServicePort: true,
	}

	image := ContainerImage
	if len(opts.ContainerImage) != 0 {
		image = opts.ContainerImage
	}

	req := &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     cluster,
			ServiceName: service,
			ServiceType: common.ServiceTypeStateful,
		},

		Resource:           res,
		CatalogServiceType: common.CatalogService_PostgreSQL,

		ContainerImage: image,
		Replicas:       opts.Replicas,
		PortMappings:   []common.PortMapping{portmapping},
		RegisterDNS:    true,

		ServiceConfigs: serviceCfgs,

		Volume:               opts.Volume,
		ContainerPath:        common.DefaultContainerMountPath,
		JournalVolume:        opts.JournalVolume,
		JournalContainerPath: common.DefaultJournalVolumeContainerMountPath,

		ReplicaConfigs: replicaCfgs,
	}
	return req
}

func genServiceConfigs(platform string, cluster string, service string, azs []string,
	port int64, opts *manage.CatalogPostgreSQLOptions) []*manage.ConfigFileContent {
	domain := dns.GenDefaultDomainName(cluster)
	primaryMember := utils.GenServiceMemberName(service, 0)
	primaryHost := dns.GenDNSName(primaryMember, domain)

	// create service.conf file
	content := fmt.Sprintf(servicefileContent, platform, opts.ContainerImage, primaryHost, port, opts.AdminPasswd, opts.ReplUser, opts.ReplUserPasswd)
	serviceCfg := &manage.ConfigFileContent{
		FileName: catalog.SERVICE_FILE_NAME,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	// create primary_postgresql.conf
	primaryCfg := &manage.ConfigFileContent{
		FileName: primaryPGConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  primaryPostgresConf,
	}

	// create standby_postgresql.conf
	standbyCfg := &manage.ConfigFileContent{
		FileName: standbyPGConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  standbyPostgresConf,
	}

	// create primary_pg_hba.conf
	primaryHbaCfg := &manage.ConfigFileContent{
		FileName: primaryPGHbaConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  primaryPgHbaConf,
	}

	// create standby_pg_hba.conf
	standbyHbaCfg := &manage.ConfigFileContent{
		FileName: standbyPGHbaConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  standbyPgHbaConf,
	}

	// create recovery.conf
	primaryConnInfo := fmt.Sprintf(standbyPrimaryConnInfo, primaryHost, port, opts.ReplUser, opts.ReplUserPasswd)
	content = fmt.Sprintf(standbyRecoveryConf, primaryConnInfo, standbyRestoreCmd)
	recoveryCfg := &manage.ConfigFileContent{
		FileName: recoveryConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	return []*manage.ConfigFileContent{serviceCfg, primaryCfg, standbyCfg, primaryHbaCfg, standbyHbaCfg, recoveryCfg}
}

// GenReplicaConfigs generates the replica configs.
// Note: if the number of availability zones is less than replicas, 2 or more replicas will run on the same zone.
func GenReplicaConfigs(platform string, cluster string, service string, azs []string,
	port int64, opts *manage.CatalogPostgreSQLOptions) []*manage.ReplicaConfig {
	replicaCfgs := make([]*manage.ReplicaConfig, opts.Replicas)

	// generate the primary configs
	domain := dns.GenDefaultDomainName(cluster)
	primaryMember := utils.GenServiceMemberName(service, 0)
	primaryHost := dns.GenDNSName(primaryMember, domain)

	bindIP := primaryHost
	if platform == common.ContainerPlatformSwarm {
		bindIP = catalog.BindAllIP
	}

	content := fmt.Sprintf(memberfileContent, containerRolePrimary, azs[0], primaryHost, bindIP)
	primaryMemberCfg := &manage.ConfigFileContent{
		FileName: catalog.MEMBER_FILE_NAME,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	configs := []*manage.ConfigFileContent{primaryMemberCfg}
	replicaCfgs[0] = &manage.ReplicaConfig{Zone: azs[0], MemberName: primaryMember, Configs: configs}

	// generate the standby configs.
	// TODO support cascading replication, especially for cross-region replication.
	for i := int64(1); i < opts.Replicas; i++ {
		index := int(i) % len(azs)
		member := utils.GenServiceMemberName(service, i)
		memberHost := dns.GenDNSName(member, domain)

		bindIP := memberHost
		if platform == common.ContainerPlatformSwarm {
			bindIP = catalog.BindAllIP
		}

		content := fmt.Sprintf(memberfileContent, containerRoleStandby, azs[index], memberHost, bindIP)
		memberCfg := &manage.ConfigFileContent{
			FileName: catalog.MEMBER_FILE_NAME,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		configs := []*manage.ConfigFileContent{memberCfg}
		replicaCfgs[i] = &manage.ReplicaConfig{Zone: azs[index], MemberName: member, Configs: configs}
	}

	return replicaCfgs
}

const (
	servicefileContent = `
PLATFORM=%s
CONTAINER_IMAGE=%s
PRIMARY_HOST=%s
LISTEN_PORT=%d
POSTGRES_PASSWORD=%s
REPLICATION_USER=%s
REPLICATION_PASSWORD=%s
`

	memberfileContent = `
CONTAINER_ROLE=%s
AVAILABILITY_ZONE=%s
SERVICE_MEMBER=%s
BIND_IP=%s
`

	primaryPostgresConf = `
listen_addresses = 'localhost'

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
host   replication      defaultReplUser                all                    md5
`

	standbyPostgresConf = `
listen_addresses = 'localhost'

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
