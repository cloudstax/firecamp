package db

import (
	"github.com/cloudstax/firecamp/common"
	"golang.org/x/net/context"
)

const (
	// The status of one table
	TableStatusCreating = "CREATING"
	TableStatusUpdating = "UPDATING"
	TableStatusDeleting = "DELETING"
	TableStatusActive   = "ACTIVE"

	ClusterName  = "ClusterName"
	ServiceName  = "ServiceName"
	ServiceUUID  = "ServiceUUID"
	Revision     = "Revision"
	LastModified = "LastModified"

	ServiceMeta = "ServiceMeta"
	ServiceSpec = "ServiceSpec"

	MemberIndex = "MemberIndex"
	MemberMeta  = "MemberMeta"
	MemberSpec  = "MemberSpec"

	ConfigFileID      = "ConfigFileID"
	ConfigFileMD5     = "ConfigFileMD5"
	ConfigFileName    = "ConfigFileName"
	ConfigFileMode    = "ConfigFileMode"
	ConfigFileContent = "ConfigFileContent"

	StaticIP     = "StaticIP"
	StaticIPSpec = "StaticIPSpec"
)

// DB defines the DB interfaces
//
// The design aims to provide the flexibility to support different type of key-value DBs.
// For example, could use the simple embedded controlDB, DynamoDB, etcd, zk, etc.
// There are 2 requirements:
// 1) conditional creation/update (create-if-not-exist and update-if-match).
// 2) strong consistency on get/list.
//
// The device/service/serviceattr/servicemember/configfile creations are create-if-not-exist.
// If item exists in DB, the ErrDBConditionalCheckFailed error will be returned.
//
// The serviceattr/servicemember updates are also update-if-match the old item.
// Return ErrDBConditionalCheckFailed as well if not match.
type DB interface {
	CreateSystemTables(ctx context.Context) error
	SystemTablesReady(ctx context.Context) (tableStatus string, ready bool, err error)
	DeleteSystemTables(ctx context.Context) error

	CreateDevice(ctx context.Context, dev *common.Device) error
	GetDevice(ctx context.Context, clusterName string, deviceName string) (dev *common.Device, err error)
	DeleteDevice(ctx context.Context, clusterName string, deviceName string) error
	ListDevices(ctx context.Context, clusterName string) (devs []*common.Device, err error)

	CreateService(ctx context.Context, svc *common.Service) error
	GetService(ctx context.Context, clusterName string, serviceName string) (svc *common.Service, err error)
	DeleteService(ctx context.Context, clusterName string, serviceName string) error
	ListServices(ctx context.Context, clusterName string) (svcs []*common.Service, err error)

	CreateServiceAttr(ctx context.Context, attr *common.ServiceAttr) error
	// Only support updating ServiceStatus, Replicas or UserAttr at v1, all other attributes are immutable.
	UpdateServiceAttr(ctx context.Context, oldAttr *common.ServiceAttr, newAttr *common.ServiceAttr) error
	GetServiceAttr(ctx context.Context, serviceUUID string) (attr *common.ServiceAttr, err error)
	DeleteServiceAttr(ctx context.Context, serviceUUID string) error

	CreateServiceMember(ctx context.Context, member *common.ServiceMember) error
	UpdateServiceMember(ctx context.Context, oldMember *common.ServiceMember, newMember *common.ServiceMember) error
	GetServiceMember(ctx context.Context, serviceUUID string, memberIndex int64) (member *common.ServiceMember, err error)
	ListServiceMembers(ctx context.Context, serviceUUID string) (members []*common.ServiceMember, err error)
	DeleteServiceMember(ctx context.Context, serviceUUID string, memberIndex int64) error

	CreateConfigFile(ctx context.Context, cfg *common.ConfigFile) error
	GetConfigFile(ctx context.Context, serviceUUID string, fileID string) (cfg *common.ConfigFile, err error)
	DeleteConfigFile(ctx context.Context, serviceUUID string, fileID string) error

	CreateServiceStaticIP(ctx context.Context, serviceip *common.ServiceStaticIP) error
	UpdateServiceStaticIP(ctx context.Context, oldip *common.ServiceStaticIP, newip *common.ServiceStaticIP) error
	GetServiceStaticIP(ctx context.Context, ip string) (serviceip *common.ServiceStaticIP, err error)
	DeleteServiceStaticIP(ctx context.Context, ip string) error
}
