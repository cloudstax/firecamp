package db

import (
	"github.com/cloudstax/firecamp/common"
)

const (
	// Device table
	DeviceTableName = common.SystemName + "-device-table"
	// Service table
	ServiceTableName = common.SystemName + "-service-table"
	// ServiceAttr table
	ServiceAttrTableName = common.SystemName + "-serviceattr-table"
	// ServiceMember table
	ServiceMemberTableName = common.SystemName + "-servicemember-table"
	// ConfigFile table
	ConfigTableName = common.SystemName + "-config-table"

	ClusterName   = "ClusterName"
	ServiceName   = "ServiceName"
	ServiceStatus = "ServiceStatus"
	ServiceUUID   = "ServiceUUID"
	Replicas      = "Replicas"
	RegisterDNS   = "RegisterDNS"
	DomainName    = "DomainName"
	HostedZoneID  = "HostedZoneID"
	LastModified  = "LastModified"

	VolumeID            = "VolumeID"
	VolumeSizeGB        = "VolumeSizeGB"
	DeviceName          = "DeviceName"
	AvailableZone       = "AvailableZone"
	TaskID              = "TaskID"
	ContainerInstanceID = "ContainerInstanceID"
	ServerInstanceID    = "ServerInstanceID"
	MemberName          = "MemberName"
	MemberConfigs       = "MemberConfigs"

	ConfigFileID      = "ConfigFileID"
	ConfigFileMD5     = "ConfigFileMD5"
	ConfigFileName    = "ConfigFileName"
	ConfigFileMode    = "ConfigFileMode"
	ConfigFileContent = "ConfigFileContent"

	// see http://stackoverflow.com/questions/32833351/dynamodb-put-item-if-hash-or-hash-and-range-combination-doesnt-exist
	// Every item has both a hash and a range key. This means that one of two things will happen:
	// 1. The hash+range pair exists in the database.
	//			attribute_not_exists(hash) must be true
	//			attribute_not_exists(range) must be true
	// 2. The hash+range pair does not exist in the database.
	// 			attribute_not_exists(hash) must be false
	//			attribute_not_exists(range) must be false
	ServicePutCondition       = "attribute_not_exists(" + ServiceName + ")"
	ServiceDelCondition       = "attribute_exists(" + ServiceName + ")"
	ServiceAttrPutCondition   = "attribute_not_exists(" + ServiceUUID + ")"
	ServiceAttrDelCondition   = "attribute_exists(" + ServiceUUID + ")"
	DevicePutCondition        = "attribute_not_exists(" + DeviceName + ")"
	DeviceDelCondition        = "attribute_exists(" + DeviceName + ")"
	ServiceMemberPutCondition = "attribute_not_exists(" + VolumeID + ")"
	ServiceMemberDelCondition = "attribute_exists(" + VolumeID + ")"
	ConfigFilePutCondition    = "attribute_not_exists(" + ConfigFileID + ")"
	ConfigFileDelCondition    = "attribute_exists(" + ConfigFileID + ")"

	// The status of one table
	TableStatusCreating = "CREATING"
	TableStatusUpdating = "UPDATING"
	TableStatusDeleting = "DELETING"
	TableStatusActive   = "ACTIVE"
)
