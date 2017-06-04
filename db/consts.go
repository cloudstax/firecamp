package db

import (
	"github.com/cloudstax/openmanage/common"
)

const (
	// Device table
	DeviceTableName = common.SystemName + "-DeviceTable"
	// Service table
	ServiceTableName = common.SystemName + "-ServiceTable"
	// ServiceAttr table
	ServiceAttrTableName = common.SystemName + "-ServiceAttrTable"
	// Volume table
	VolumeTableName = common.SystemName + "-VolumeTable"
	// ConfigFile table
	ConfigTableName = common.SystemName + "-ConfigTable"

	ClusterName         = "ClusterName"
	ServiceName         = "ServiceName"
	ServiceStatus       = "ServiceStatus"
	ServiceUUID         = "ServiceUUID"
	Replicas            = "Replicas"
	HasStrictMembership = "HasStrictMembership"
	DomainName          = "DomainName"
	HostedZoneID        = "HostedZoneID"
	LastModified        = "LastModified"

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
	ConfigFileContent = "ConfigFileContent"

	// see http://stackoverflow.com/questions/32833351/dynamodb-put-item-if-hash-or-hash-and-range-combination-doesnt-exist
	// Every item has both a hash and a range key. This means that one of two things will happen:
	// 1. The hash+range pair exists in the database.
	//			attribute_not_exists(hash) must be true
	//			attribute_not_exists(range) must be true
	// 2. The hash+range pair does not exist in the database.
	// 			attribute_not_exists(hash) must be false
	//			attribute_not_exists(range) must be false
	ServicePutCondition     = "attribute_not_exists(" + ServiceName + ")"
	ServiceDelCondition     = "attribute_exists(" + ServiceName + ")"
	ServiceAttrPutCondition = "attribute_not_exists(" + ServiceUUID + ")"
	ServiceAttrDelCondition = "attribute_exists(" + ServiceUUID + ")"
	DevicePutCondition      = "attribute_not_exists(" + DeviceName + ")"
	DeviceDelCondition      = "attribute_exists(" + DeviceName + ")"
	VolumePutCondition      = "attribute_not_exists(" + VolumeID + ")"
	VolumeDelCondition      = "attribute_exists(" + VolumeID + ")"
	ConfigFilePutCondition  = "attribute_not_exists(" + ConfigFileID + ")"
	ConfigFileDelCondition  = "attribute_exists(" + ConfigFileID + ")"

	// The status of one table
	TableStatusCreating = "CREATING"
	TableStatusUpdating = "UPDATING"
	TableStatusDeleting = "DELETING"
	TableStatusActive   = "ACTIVE"
)
