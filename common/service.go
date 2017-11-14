package common

const (
	// The status of one service
	// ServiceStatusCreating: creating the required resources of the service, such as volumes.
	ServiceStatusCreating = "CREATING"
	// ServiceStatusInitializing: service resources are created, wait for the initialization.
	// at this state, the service containers are running. Some services require the special
	// initialization configurations, such as configuring the DB replicaset. Some services
	// does not require this step, such as Cassandra. The service creation script could simply
	// change the service status to ACTIVE.
	ServiceStatusInitializing = "INITIALIZING"
	// ServiceStatusActive: service initialized and ready to use.
	ServiceStatusActive   = "ACTIVE"
	ServiceStatusDeleting = "DELETING"
	ServiceStatusDeleted  = "DELETED"

	// The status of one task
	TaskStatusRunning = "RUNNING"
	TaskStatusStopped = "STOPPED"

	// Task types
	TaskTypeInit = "init"

	// The log volume name prefix, the log volume name will be log-serviceuuid,
	// the mount path will be /mnt/log-serviceuuid
	LogVolumeNamePrefix = "log"

	// General Purpose SSD
	VolumeTypeGPSSD = "gp2"
	// Provisioned IOPS SSD
	VolumeTypeIOPSSSD = "io1"
	// Throughput Optimized HDD
	VolumeTypeTPHDD = "st1"
)

// Resources represents the service/task resources, cpu and memory.
type Resources struct {
	// The number of cpu units for the container.
	// A container instance has 1,024 cpu units for every CPU core.
	// MaxCPUUnits specifies how much of the available CPU resources a container can use.
	// This will set docker's --cpu-period and --cpu-quota.
	// ReserveCPUUnits is docker's --cpu-share, same with the ECS cpu units.
	// The value could be -1, which means no limit on the resource.
	MaxCPUUnits     int64
	ReserveCPUUnits int64
	MaxMemMB        int64
	ReserveMemMB    int64
}

// PortMapping defines the container port to host port mapping.
type PortMapping struct {
	ContainerPort int64
	HostPort      int64
}

// ServiceStatus represents the service's running status.
// TODO add more status. For example, the members are running on which nodes.
type ServiceStatus struct {
	RunningCount int64
	DesiredCount int64
}

// TaskStatus represents the task's status
type TaskStatus struct {
	// valid status: pending, running, stopped
	Status        string
	StoppedReason string
	StartedAt     string // time format: 2017-05-20T19:50:56.834924582Z
	FinishedAt    string
}

// Device records the assigned device for the service.
// The DeviceName has to be part of the key, to ensure one device is only assigned to one service.
type Device struct {
	ClusterName string // partition key
	DeviceName  string // sort key
	// service that the device is assigned to
	ServiceName string
}

// Service records the assigned uuid for the service.
type Service struct {
	ClusterName string // partition key
	ServiceName string // sort key
	ServiceUUID string
}

// ServiceAttr represents the detail attributes of the service.
// The ServiceUUID is passed as volume name to the volume driver. The volume driver
// could get the detail service attributes for other operations.
type ServiceAttr struct {
	ServiceUUID   string // partition key
	ServiceStatus string
	LastModified  int64
	Replicas      int64
	ClusterName   string
	ServiceName   string
	Volumes       ServiceVolumes

	// whether the service members need to know each other, such as database replicas.
	// if yes, the member will be registered to DNS. in aws, DNS will be Route53.
	RegisterDNS bool
	// for v1, DomainName would be the default domain, such as cluster-firecamp.com
	DomainName string
	// The AWS Route53 HostedZone for the current firecamp cluster.
	HostedZoneID string

	// Whether the service member needs the static ip. This is only required by Redis.
	RequireStaticIP bool

	// The service admin and password. The password will be deleted once the service is initialized.
	//Admin       string
	//AdminPasswd string
}

// ServiceVolumes represent the volumes of one service.
// For now, allow maximum 2 devices. Could further expand if necessary in the future.
type ServiceVolumes struct {
	PrimaryDeviceName string
	PrimaryVolume     ServiceVolume

	// The LogDeviceName could be empty if service only needs one volume.
	LogDeviceName string
	LogVolume     ServiceVolume
}

// ServiceVolume contains the volume parameters.
type ServiceVolume struct {
	VolumeType   string
	VolumeSizeGB int64
	Iops         int64
}

// ServiceMember represents the attributes of one service member.
type ServiceMember struct {
	ServiceUUID         string // partition key
	MemberName          string // sort key
	AvailableZone       string
	TaskID              string
	ContainerInstanceID string
	ServerInstanceID    string
	LastModified        int64

	// The volumes of one member. One member could have multiple volumes.
	// For example, one for DB data, the other for log.
	Volumes MemberVolumes

	// The static IP assigned to this member
	StaticIP string

	// One member could have multiple config files.
	// For example, cassandra.yaml and rackdc properties files.
	Configs []*MemberConfig
}

// MemberVolumes represent the volumes of one member.
type MemberVolumes struct {
	// The config files will be created on the primary volume.
	PrimaryVolumeID string
	// The primary device will be mounted to /mnt/serviceuuid on the host and /data in the container
	PrimaryDeviceName string

	// The possible log volume to store the service's data log
	LogVolumeID string
	// The log device will be mounted to /mnt/serviceuuid-log on the host and /log in the container.
	// We don't want to mount the log device under the primary device mount path, such as /mnt/serviceuuid/log.
	// If there is some bug that the log device is not mounted, the service log will be directly written to
	// the primary device. Later when the log device is mounted again, some logs will be temporary lost.
	LogDeviceName string
}

// MemberConfig represents the configs of one member
type MemberConfig struct {
	FileName string
	// The config file uuid
	FileID string
	// The MD5 checksum of the config file content.
	// The config file content would usually not be updated. The checksum could help
	// the volume driver easily know whether it needs to load the config file content.
	// The config file content may not be small, such as a few hundreds KB.
	FileMD5 string
}

// ConfigFile represents the detail config content of service member.
// To update the ConfigFile content of one replica, 2 steps are required:
// 1) create a new ConfigFile with a new FileID, 2) update ServiceMember MemberConfig
// to point to the new ConfigFile.
// We could not directly update the old ConfigFile. If node crashes before step2,
// ServiceMember MemberConfig will not be consistent with ConfigFile.
type ConfigFile struct {
	ServiceUUID  string // partition key
	FileID       string // sort key
	FileMD5      string
	FileName     string
	FileMode     uint32
	LastModified int64
	Content      string // The content of the config file.
}

// ServiceStaticIP represents the owner service of one static IP.
type ServiceStaticIP struct {
	StaticIP string // partition key

	// The owner service of this static IP.
	ServiceUUID string
	// TODO adding MemberName would be a good optimization.
	//      so volume plugin could directly get the local static IP and get the member.
	// MemberName  string
	// The AvailableZone this static IP belongs to.
	AvailableZone string
	// The server instance this IP is assigned to.
	ServerInstanceID string
	// The network interface this IP is assigned to.
	NetworkInterfaceID string
}
