package common

const (
	ContainerPlatformECS   = "ecs"
	ContainerPlatformSwarm = "swarm"

	OrgName             = "cloudstax"
	SystemName          = "openmanage"
	ContainerNamePrefix = OrgName + "/" + SystemName + "-"

	// VolumeDriverName is the name for docker volume driver
	// If this name is changed, please change dockervolume/aws-ecs-agent-patch/openmanage_task_engine.go as well.
	// Has the separate definition in openmanage_task_engine.go aims to avoid the dependency of
	// ecs-agent on openmanage code.
	VolumeDriverName = "OpenManageVolumeDriver"

	DefaultLogDir = "/var/log/" + SystemName

	NameSeparator = "-"

	ServiceMemberDomainNameTTLSeconds = 10

	// A ECS container instance has 1,024 cpu units for every CPU core
	DefaultMaxCPUUnits     = -1
	DefaultReserveCPUUnits = 256
	DefaultMaxMemoryMB     = -1
	DefaultReserveMemoryMB = 256

	DefaultServiceWaitSeconds = 120
	DefaultTaskWaitSeconds    = 120
	DefaultTaskRetryCounts    = 5
	DefaultRetryWaitSeconds   = 3

	DomainSeparator  = "."
	DomainNameSuffix = SystemName
	DomainCom        = "com"

	ContainerNameSuffix = "container"

	DefaultContainerMountPath = "/data"
	DefaultConfigDir          = "/conf"
	DefaultConfigPath         = DefaultContainerMountPath + DefaultConfigDir
	DefaultConfigFileMode     = 0600

	DBTypeControlDB = "controldb" // the controldb service
	DBTypeCloudDB   = "clouddb"   // such as AWS DynamoDB

	ControlDBServerPort  = 27030
	ControlDBName        = "controldb"
	ControlDBServiceName = SystemName + NameSeparator + ControlDBName
	ControlDBDefaultDir  = DefaultContainerMountPath + "/" + ControlDBName
	// ControlDBUUIDPrefix defines the prefix of the controldb server id.
	// The service uuid of the controldb service would be ControlDBUUIDPrefix + volumeID.
	// The volumeID is the ID of the volume created for the controldb service.
	ControlDBUUIDPrefix      = ControlDBName + NameSeparator
	ControlDBContainerImage  = OrgName + "/" + ControlDBServiceName
	ControlDBReserveCPUUnits = 256
	ControlDBMaxMemMB        = 4096
	ControlDBReserveMemMB    = 256
	ControlDBVolumeSizeGB    = int64(4)

	ManageHTTPServerPort  = 27040
	ManageName            = "manageserver"
	ManageServiceName     = SystemName + NameSeparator + ManageName
	ManageContainerImage  = OrgName + "/" + ManageServiceName
	ManageReserveCPUUnits = 256
	ManageMaxMemMB        = 4096
	ManageReserveMemMB    = 256
)

type EnvKeyValuePair struct {
	Name  string
	Value string
}

// define the common environment keys
const (
	ENV_VALUE_SEPARATOR = ","

	ENV_REGION            = "REGION"
	ENV_CLUSTER           = "CLUSTER"
	ENV_MANAGE_SERVER_URL = "MANAGE_SERVER_URL"
	ENV_OP                = "OP"

	ENV_SERVICE_NAME    = "SERVICE_NAME"
	ENV_SERVICE_MASTER  = "SERVICE_MASTER"
	ENV_SERVICE_MEMBERS = "SERVICE_MEMBERS"
	ENV_SERVICE_TYPE    = "SERVICE_TYPE"

	ENV_CONTAINER_PLATFORM = "CONTAINER_PLATFORM"
	ENV_DB_TYPE            = "DB_TYPE"
)
