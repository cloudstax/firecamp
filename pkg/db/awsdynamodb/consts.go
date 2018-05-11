package awsdynamodb

import (
	"github.com/cloudstax/firecamp/api/common"
)

// DynamoDB related const
const (
	InternalServerError                    = "InternalServerError"
	LimitExceededException                 = "LimitExceededException"
	TableInUseException                    = "TableInUseException"
	ResourceNotFoundException              = "ResourceNotFoundException"
	TableNotFoundException                 = "TableNotFoundException"
	ConditionalCheckFailedException        = "ConditionalCheckFailedException"
	ProvisionedThroughputExceededException = "ProvisionedThroughputExceededException"

	tableNameSuffix   = common.SystemName + "-table"
	tablePartitionKey = "PartitionKey"
	tableSortKey      = "SortKey"

	devicePartitionKeyPrefix        = "DeviceKey-"
	servicePartitionKeyPrefix       = "ServiceKey-"
	serviceAttrPartitionKeyPrefix   = "ServiceAttrKey-"
	serviceMemberPartitionKeyPrefix = "ServiceMemberKey-"
	configPartitionKeyPrefix        = "ConfigKey-"
	staticIPPartitionKeyPrefix      = "StaticIPKey-"

	defaultReadCapacity  = 25
	defaultWriteCapacity = 25

	// see http://stackoverflow.com/questions/32833351/dynamodb-put-item-if-hash-or-hash-and-range-combination-doesnt-exist
	// Every item has both a hash and a range key. This means that one of two things will happen:
	// 1. The hash+range pair exists in the database.
	//			attribute_not_exists(hash) must be true
	//			attribute_not_exists(range) must be true
	// 2. The hash+range pair does not exist in the database.
	// 			attribute_not_exists(hash) must be false
	//			attribute_not_exists(range) must be false
	// We define 2 conditions on SortKey and PartitionKey to make it clear which key is unique.
	tableSortKeyPutCondition      = "attribute_not_exists(" + tableSortKey + ")"
	tableSortKeyDelCondition      = "attribute_exists(" + tableSortKey + ")"
	tablePartitionKeyPutCondition = "attribute_not_exists(" + tablePartitionKey + ")"
	tablePartitionKeyDelCondition = "attribute_exists(" + tablePartitionKey + ")"
)
