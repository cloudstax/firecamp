package common

const (
	Version = "latest"

	CloudPlatformAWS = "aws"

	ContainerPlatformECS   = "ecs"
	ContainerPlatformSwarm = "swarm"
	ContainerPlatformK8s   = "k8s"

	DefaultK8sNamespace = "default"

	ContainerPlatformRoleManager = "manager"
	ContainerPlatformRoleWorker  = "worker"

	// OrgName and SystemName could not include "-"
	OrgName             = "cloudstax"
	SystemName          = "firecamp"
	ContainerNamePrefix = OrgName + "/" + SystemName + "-"

	DefaultFSType = "xfs"

	// VolumeDriverName is the name for docker volume driver.
	// Do NOT change the volume driver name. If this name is changed,
	// please update the volume driver plugin in scripts/builddocker.sh.
	// please also update docker/volume/aws-ecs-agent-patch/firecamp_task_engine.go,
	// Has the separate definition in firecamp_task_engine.go aims to avoid the dependency of
	// ecs-agent on firecamp code.
	VolumeDriverName = OrgName + "/" + SystemName + "-" + "volume:" + Version

	// LogDriverName is the name for docker log driver.
	// Do NOT change the log driver name. If this name is changed,
	// please update the log driver plugin in scripts/builddocker.sh.
	LogDriverName = OrgName + "/" + SystemName + "-" + "log:" + Version
	// The LogServiceUUIDKey is set by firecamp_task_engine.go in cloudstax/amazon-ecs-agent.
	// if you want to change the value here, also need to change in cloudstax/amazon-ecs-agent.
	LogServiceUUIDKey = "ServiceUUID"
	// The LogServiceMemberKey is for the single member service, such as the management service.
	LogServiceMemberKey = "ServiceMember"

	LOGDRIVER_DEFAULT = "json-file"
	LOGDRIVER_AWSLOGS = "awslogs"

	DefaultLogDir = "/var/log/" + SystemName

	NameSeparator = "-"

	ServiceMemberDomainNameTTLSeconds = 5

	// A ECS container instance has 1,024 cpu units for every CPU core
	DefaultMaxCPUUnits     = 0
	DefaultReserveCPUUnits = 256
	DefaultMaxMemoryMB     = 0
	DefaultReserveMemoryMB = 256

	DefaultServiceWaitSeconds = 120
	DefaultTaskWaitSeconds    = 120
	DefaultTaskRetryCounts    = 5
	DefaultRetryWaitSeconds   = 3

	DomainSeparator  = "."
	DomainNameSuffix = SystemName
	DomainCom        = "com"

	DefaultHostIP = "127.0.0.1"

	ContainerNameSuffix = "container"

	DefaultContainerMountPath              = "/data"
	DefaultJournalVolumeContainerMountPath = "/journal"
	DefaultConfigDir                       = "/conf"
	DefaultConfigPath                      = DefaultContainerMountPath + DefaultConfigDir
	DefaultConfigFileMode                  = 0600

	DBTypeControlDB = "controldb" // the controldb service
	DBTypeCloudDB   = "clouddb"   // such as AWS DynamoDB
	DBTypeK8sDB     = "k8sdb"     // db on top of k8s ConfigMap
	DBTypeMemDB     = "memdb"     // in-memory db, for test only

	ControlDBServerPort  = 27030
	ControlDBName        = "controldb"
	ControlDBServiceName = SystemName + NameSeparator + ControlDBName
	ControlDBDefaultDir  = DefaultContainerMountPath + "/" + ControlDBName
	// ControlDBUUIDPrefix defines the prefix of the controldb server id.
	// The service uuid of the controldb service would be ControlDBUUIDPrefix + volumeID.
	// The volumeID is the ID of the volume created for the controldb service.
	ControlDBUUIDPrefix      = ControlDBName + NameSeparator
	ControlDBContainerImage  = OrgName + "/" + ControlDBServiceName + ":" + Version
	ControlDBReserveCPUUnits = 256
	ControlDBMaxMemMB        = 4096
	ControlDBReserveMemMB    = 256
	ControlDBVolumeSizeGB    = int64(4)

	ManageHTTPServerPort  = 27040
	ManageName            = "manageserver"
	ManageServiceName     = SystemName + NameSeparator + ManageName
	ManageContainerImage  = OrgName + "/" + ManageServiceName + ":" + Version
	ManageReserveCPUUnits = 256
	ManageMaxMemMB        = 4096
	ManageReserveMemMB    = 256
)

type EnvKeyValuePair struct {
	Name  string
	Value string
}

type KeyValuePair struct {
	Key   string
	Value string
}

// define the common environment keys
const (
	// this is for passing the firecamp version to AWS ECS task definition.
	// the ECS agent patch will construct the correct volume plugin name with it.
	ENV_VERSION = "VERSION"

	ENV_VALUE_SEPARATOR = ","
	ENV_SHARD_SEPARATOR = ";"

	ENV_REGION            = "REGION"
	ENV_CLUSTER           = "CLUSTER"
	ENV_MANAGE_SERVER_URL = "MANAGE_SERVER_URL"
	ENV_OP                = "OP"

	ENV_ADMIN          = "ADMIN"
	ENV_ADMIN_PASSWORD = "ADMIN_PASSWORD"

	ENV_SERVICE_NAME    = "SERVICE_NAME"
	ENV_SERVICE_NODE    = "SERVICE_NODE"
	ENV_SERVICE_PORT    = "SERVICE_PORT"
	ENV_SERVICE_MASTER  = "SERVICE_MASTER"
	ENV_SERVICE_MEMBERS = "SERVICE_MEMBERS"
	ENV_SERVICE_TYPE    = "SERVICE_TYPE"

	ENV_CONTAINER_PLATFORM = "CONTAINER_PLATFORM"
	ENV_DB_TYPE            = "DB_TYPE"
	ENV_AVAILABILITY_ZONES = "AVAILABILITY_ZONES"
	ENV_K8S_NAMESPACE      = "K8S_NAMESPACE"

	ENV_SHARDS            = "SHARDS"
	ENV_REPLICAS_PERSHARD = "REPLICAS_PERSHARD"
)
