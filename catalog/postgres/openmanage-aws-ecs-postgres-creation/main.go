package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/catalog"
	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/manage"
	"github.com/cloudstax/openmanage/server/awsec2"
	"github.com/cloudstax/openmanage/utils"
)

// The script is a simple demo for how to create the postgres service, the ECS task definition and service.
var (
	service      = flag.String("service", "", "The target service name in ECS")
	replicas     = flag.Int64("replicas", 1, "The number of replicas for the service")
	volSizeGB    = flag.Int64("volume-size", 0, "The size of each EBS volume, unit: GB")
	region       = flag.String("region", "", "The target AWS region")
	cluster      = flag.String("cluster", "default", "The ECS cluster")
	serverURL    = flag.String("server-url", "", "the management service url, default: "+dns.GetDefaultManageServiceURL("cluster", false))
	tlsEnabled   = flag.Bool("tlsEnabled", false, "whether tls is enabled")
	caFile       = flag.String("ca-file", "", "the ca file")
	certFile     = flag.String("cert-file", "", "the cert file")
	keyFile      = flag.String("key-file", "", "the key file")
	cpuUnits     = flag.Int64("cpu-units", 128, "The number of cpu units to reserve for the container")
	maxMemMB     = flag.Int64("max-memory", 128, "The hard limit for memory, unit: MB")
	reserveMemMB = flag.Int64("soft-memory", 128, "The memory reserved for the container, unit: MB")
	port         = flag.Int64("port", 5432, "The PostgreSQL listening port, default: 5432")
	pgpasswd     = flag.String("passwd", "password", "The PostgreSQL DB password")
	replUser     = flag.String("replication-user", "repluser", "The replication user that the standby DB replicates from the primary")
	replPasswd   = flag.String("replication-passwd", "replpassword", "The password for the standby DB to access the primary")
)

const (
	ContainerImage = common.ContainerNamePrefix + "postgres"
	ContainerPath  = common.DefaultContainerMountPath

	ContainerRolePrimary = "primary"
	ContainerRoleStandby = "standby"
	SysConfFileName      = "sys.conf"
	PostgresConfFileName = "postgresql.conf"
	PGHbaConfFileName    = "pg_hba.conf"
	RecoveryConfFileName = "recovery.conf"
)

func usage() {
	fmt.Println("usage: openmanage-aws-postgres-creation -op=create-service -region=us-west-1")
}

func main() {
	flag.Parse()
	defer glog.Flush()

	if *service == "" || *replicas == 0 || *volSizeGB == 0 {
		fmt.Println("please specify the valid service name, replicas, volume size")
		os.Exit(-1)
	}

	mgtserverurl := *serverURL
	if mgtserverurl == "" {
		// use default server url
		mgtserverurl = dns.GetDefaultManageServiceURL(*cluster, *tlsEnabled)
	} else {
		// format url
		mgtserverurl = dns.FormatManageServiceURL(mgtserverurl, *tlsEnabled)
	}

	var err error
	awsRegion := *region
	if awsRegion == "" {
		awsRegion, err = awsec2.GetLocalEc2Region()
		if err != nil {
			fmt.Println("please input the correct AWS region")
			os.Exit(-1)
		}
	}

	config := aws.NewConfig().WithRegion(awsRegion)
	sess, err := session.NewSession(config)
	if err != nil {
		glog.Fatalln("failed to create aws session, error", err)
	}

	// make sure one replica per az
	azs, err := awsec2.GetRegionAZs(awsRegion, sess)
	if err != nil {
		glog.Fatalln(err)
	}
	if len(azs) < int(*replicas) {
		fmt.Println("region ", awsRegion, " only has ", len(azs), " availability zones")
		os.Exit(-1)
	}

	sop, err := serviceop.NewServiceOp(awsRegion, mgtserverurl, *tlsEnabled, *caFile, *certFile, *keyFile)
	if err != nil {
		glog.Fatalln(err)
	}

	ctx := context.Background()

	// generate service ReplicaConfigs
	replicaCfgs := genReplicaConfigs(*cluster, *service, azs, *replicas, *port, *pgpasswd, *replUser, *replPasswd)

	req := &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      awsRegion,
			Cluster:     *cluster,
			ServiceName: *service,
		},

		Resource: &common.Resources{
			MaxCPUUnits:     *cpuUnits,
			ReserveCPUUnits: *cpuUnits,
			MaxMemMB:        *maxMemMB,
			ReserveMemMB:    *reserveMemMB,
		},

		ContainerImage: ContainerImage,
		Replicas:       *replicas,
		VolumeSizeGB:   *volSizeGB,
		ContainerPath:  ContainerPath,
		Port:           *port,

		HasMembership:  true,
		ReplicaConfigs: replicaCfgs,
	}

	err = sop.CreateService(ctx, req, common.DefaultTaskWaitSeconds)
	if err != nil {
		glog.Fatalln("create service error", err, "cluster", *cluster, "service", *service)
	}

	// postgres does not require the special cluster membership setup. set the service initialized
	err = sop.SetServiceInitialized(ctx, req.Service)
	if err != nil {
		glog.Fatalln("SetServiceInitialized error", err, req.Service)
	}

	fmt.Println("The PostgreSQL cluster are successfully initialized")
}

func genReplicaConfigs(cluster string, service string, azs []string, replicas int64, port int64, pgpasswd string, replUser string, replPasswd string) []*manage.ReplicaConfig {
	replicaCfgs := make([]*manage.ReplicaConfig, replicas)

	// generate the primary configs
	domain := dns.GenDefaultDomainName(cluster)
	primaryMember := utils.GenServiceMemberName(service, 0)
	primaryHost := dns.GenDNSName(primaryMember, domain)
	replicaCfgs[0] = genPrimaryConfig(azs[0], primaryHost, port, pgpasswd, replUser, replPasswd)

	// generate the standby configs.
	// TODO support cascading replication, specially for cross-region replication.
	for i := int64(1); i < replicas; i++ {
		replicaCfgs[i] = genStandbyConfig(azs[i], primaryHost, port, pgpasswd, replUser, replPasswd)
	}

	return replicaCfgs
}

func genPrimaryConfig(az string, primaryHost string, port int64, pgpasswd string,
	replUser string, replPasswd string) *manage.ReplicaConfig {
	// create sys.conf file
	content := fmt.Sprintf(sysConf, ContainerRolePrimary, primaryHost, port, pgpasswd, replUser, replPasswd)
	cfg1 := &manage.ReplicaConfigFile{FileName: SysConfFileName, Content: content}

	// create postgres.conf
	cfg2 := &manage.ReplicaConfigFile{FileName: PostgresConfFileName, Content: primaryPostgresConf}

	// create pg_hba.conf
	content = fmt.Sprintf(primaryPgHbaConf, replUser)
	cfg3 := &manage.ReplicaConfigFile{FileName: PGHbaConfFileName, Content: content}

	configs := []*manage.ReplicaConfigFile{cfg1, cfg2, cfg3}
	return &manage.ReplicaConfig{Zone: az, Configs: configs}
}

func genStandbyConfig(az string, primaryHost string, port int64, pgpasswd string, replUser string, replPasswd string) *manage.ReplicaConfig {
	// create sys.conf file
	content := fmt.Sprintf(sysConf, ContainerRoleStandby, primaryHost, port, pgpasswd, replUser, replPasswd)
	cfg1 := &manage.ReplicaConfigFile{FileName: SysConfFileName, Content: content}

	// create postgres.conf
	cfg2 := &manage.ReplicaConfigFile{FileName: PostgresConfFileName, Content: standbyPostgresConf}

	// create pg_hba.conf
	cfg3 := &manage.ReplicaConfigFile{FileName: PGHbaConfFileName, Content: standbyPgHbaConf}

	// create recovery.conf
	primaryConnInfo := fmt.Sprintf(standbyPrimaryConnInfo, primaryHost, port, replUser, replPasswd)
	content = fmt.Sprintf(standbyRecoveryConf, primaryConnInfo, standbyRestoreCmd)
	cfg4 := &manage.ReplicaConfigFile{FileName: RecoveryConfFileName, Content: content}

	configs := []*manage.ReplicaConfigFile{cfg1, cfg2, cfg3, cfg4}
	return &manage.ReplicaConfig{Zone: az, Configs: configs}
}

const (
	sysConf = `
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
