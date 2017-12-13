package awsdynamodb

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/utils"
)

// CreateServiceMember creates one EBS serviceMember in DB
func (d *DynamoDB) CreateServiceMember(ctx context.Context, member *common.ServiceMember) error {
	requuid := utils.GetReqIDFromContext(ctx)

	volBytes, err := json.Marshal(member.Volumes)
	if err != nil {
		glog.Errorln("Marshal MemberVolumes error", err, member, "requuid", requuid)
		return err
	}

	configBytes, err := json.Marshal(member.Configs)
	if err != nil {
		glog.Errorln("Marshal MemberConfigs error", err, member, "requuid", requuid)
		return err
	}

	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.PutItemInput{
		TableName: aws.String(d.tableName),
		Item: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(serviceMemberPartitionKeyPrefix + member.ServiceUUID),
			},
			tableSortKey: {
				S: aws.String(strconv.FormatInt(member.MemberIndex, 10)),
			},
			MemberName: {
				S: aws.String(member.MemberName),
			},
			LastModified: {
				N: aws.String(strconv.FormatInt(member.LastModified, 10)),
			},
			AvailableZone: {
				S: aws.String(member.AvailableZone),
			},
			TaskID: {
				S: aws.String(member.TaskID),
			},
			ContainerInstanceID: {
				S: aws.String(member.ContainerInstanceID),
			},
			ServerInstanceID: {
				S: aws.String(member.ServerInstanceID),
			},
			MemberVolumes: {
				B: volBytes,
			},
			StaticIP: {
				S: aws.String(member.StaticIP),
			},
			MemberConfigs: {
				B: configBytes,
			},
		},
		ConditionExpression:    aws.String(tableSortKeyPutCondition),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}
	_, err = dbsvc.PutItem(params)

	if err != nil {
		glog.Errorln("failed to create serviceMember", member, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("created serviceMember", member, "requuid", requuid)
	return nil
}

// UpdateServiceMember updates the ServiceMember in DB
func (d *DynamoDB) UpdateServiceMember(ctx context.Context, oldMember *common.ServiceMember, newMember *common.ServiceMember) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// sanity check. ServiceUUID, VolumeID, etc, are not allowed to update.
	if oldMember.ServiceUUID != newMember.ServiceUUID ||
		!db.EqualMemberVolumes(&(oldMember.Volumes), &(newMember.Volumes)) ||
		oldMember.AvailableZone != newMember.AvailableZone ||
		oldMember.MemberIndex != newMember.MemberIndex ||
		oldMember.MemberName != newMember.MemberName ||
		oldMember.StaticIP != newMember.StaticIP {
		glog.Errorln("immutable attributes are updated, oldMember", oldMember, "newMember", newMember, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	var err error
	var oldCfgBytes []byte
	if oldMember.Configs != nil {
		oldCfgBytes, err = json.Marshal(oldMember.Configs)
		if err != nil {
			glog.Errorln("Marshal new MemberConfigs error", err, "requuid", requuid, oldMember.Configs)
			return err
		}
	}

	var newCfgBytes []byte
	if newMember.Configs != nil {
		newCfgBytes, err = json.Marshal(newMember.Configs)
		if err != nil {
			glog.Errorln("Marshal new MemberConfigs error", err, "requuid", requuid, newMember.Configs)
			return err
		}
	}

	dbsvc := dynamodb.New(d.sess)

	updateExpr := "SET " + TaskID + " = :v1, " + ContainerInstanceID + " = :v2, " +
		ServerInstanceID + " = :v3, " + LastModified + " = :v4, " + MemberConfigs + " = :v5"
	conditionExpr := TaskID + " = :cv1 AND " + ContainerInstanceID + " = :cv2 AND " +
		ServerInstanceID + " = :cv3 AND " + MemberConfigs + " = :cv4"

	params := &dynamodb.UpdateItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(serviceMemberPartitionKeyPrefix + oldMember.ServiceUUID),
			},
			tableSortKey: {
				S: aws.String(strconv.FormatInt(oldMember.MemberIndex, 10)),
			},
		},
		UpdateExpression:    aws.String(updateExpr),
		ConditionExpression: aws.String(conditionExpr),
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":v1": {
				S: aws.String(newMember.TaskID),
			},
			":v2": {
				S: aws.String(newMember.ContainerInstanceID),
			},
			":v3": {
				S: aws.String(newMember.ServerInstanceID),
			},
			":v4": {
				N: aws.String(strconv.FormatInt(newMember.LastModified, 10)),
			},
			":v5": {
				B: newCfgBytes,
			},
			":cv1": {
				S: aws.String(oldMember.TaskID),
			},
			":cv2": {
				S: aws.String(oldMember.ContainerInstanceID),
			},
			":cv3": {
				S: aws.String(oldMember.ServerInstanceID),
			},
			":cv4": {
				B: oldCfgBytes,
			},
		},
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}

	_, err = dbsvc.UpdateItem(params)

	if err != nil {
		glog.Errorln("failed to update serviceMember", oldMember, "to", newMember, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("updated serviceMember", oldMember, "to", newMember, "requuid", requuid)
	return nil
}

// GetServiceMember gets the serviceMemberItem from DB
func (d *DynamoDB) GetServiceMember(ctx context.Context, serviceUUID string, memberIndex int64) (member *common.ServiceMember, err error) {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(serviceMemberPartitionKeyPrefix + serviceUUID),
			},
			tableSortKey: {
				S: aws.String(strconv.FormatInt(memberIndex, 10)),
			},
		},
		ConsistentRead:         aws.Bool(true),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}
	resp, err := dbsvc.GetItem(params)

	if err != nil {
		glog.Errorln("failed to get serviceMember", memberIndex, "serviceUUID", serviceUUID, "error", err, "requuid", requuid)
		return nil, d.convertError(err)
	}

	if len(resp.Item) == 0 {
		glog.Infoln("serviceMember", memberIndex, "not found, serviceUUID", serviceUUID, "requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	member, err = d.attrsToServiceMember(serviceUUID, resp.Item)
	if err != nil {
		glog.Errorln("GetServiceMember convert dynamodb attributes to serviceMember error", err, "requuid", requuid, "resp", resp)
		return nil, err
	}

	glog.Infoln("get serviceMember", member, "requuid", requuid)
	return member, nil
}

// ListServiceMembers lists all serviceMembers of the service
func (d *DynamoDB) ListServiceMembers(ctx context.Context, serviceUUID string) (serviceMembers []*common.ServiceMember, err error) {
	return d.listServiceMembersWithLimit(ctx, serviceUUID, 0)
}

// listServiceMembersWithLimit limits the returned db.ServiceMembers at one query.
// This is for testing the pagination list.
func (d *DynamoDB) listServiceMembersWithLimit(ctx context.Context, serviceUUID string, limit int64) (serviceMembers []*common.ServiceMember, err error) {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	var lastEvaluatedKey map[string]*dynamodb.AttributeValue
	lastEvaluatedKey = nil

	for true {
		params := &dynamodb.QueryInput{
			TableName:              aws.String(d.tableName),
			KeyConditionExpression: aws.String(tablePartitionKey + " = :v1"),
			ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
				":v1": {
					S: aws.String(serviceMemberPartitionKeyPrefix + serviceUUID),
				},
			},
			ConsistentRead:         aws.Bool(true),
			ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
		}
		if limit > 0 {
			// set the query limit
			params.Limit = aws.Int64(limit)
		}
		if len(lastEvaluatedKey) != 0 {
			params.ExclusiveStartKey = lastEvaluatedKey
		}

		resp, err := dbsvc.Query(params)

		if err != nil {
			glog.Errorln("failed to list serviceMembers, serviceUUID", serviceUUID,
				"limit", limit, "lastEvaluatedKey", lastEvaluatedKey, "error", err, "requuid", requuid)
			return nil, d.convertError(err)
		}

		glog.Infoln("list serviceMembers succeeded, serviceUUID",
			serviceUUID, "limit", limit, "requuid", requuid, "resp count", resp.Count)

		lastEvaluatedKey = resp.LastEvaluatedKey

		if len(resp.Items) == 0 {
			// is it possible dynamodb returns no items with LastEvaluatedKey?
			// we don't use complex filter, so would be impossible?
			if len(resp.LastEvaluatedKey) != 0 {
				glog.Errorln("no items in resp but LastEvaluatedKey is not empty, resp", resp, "requuid", requuid)
				continue
			}

			glog.Infoln("no more serviceMember item for serviceUUID",
				serviceUUID, "serviceMembers", len(serviceMembers), "requuid", requuid)
			return serviceMembers, nil
		}

		for _, item := range resp.Items {
			member, err := d.attrsToServiceMember(serviceUUID, item)
			if err != nil {
				glog.Errorln("ListServiceMember convert dynamodb attributes to serviceMember error", err, "requuid", requuid, "item", item)
				return nil, err
			}
			serviceMembers = append(serviceMembers, member)
		}

		glog.Infoln("list", len(serviceMembers), "serviceMembers, serviceUUID",
			serviceUUID, "LastEvaluatedKey", lastEvaluatedKey, "requuid", requuid)

		if len(lastEvaluatedKey) == 0 {
			// no more serviceMembers
			break
		}
	}

	return serviceMembers, nil
}

// DeleteServiceMember deletes the serviceMember from DB
func (d *DynamoDB) DeleteServiceMember(ctx context.Context, serviceUUID string, memberIndex int64) error {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	// TODO reject if any serviceMember is still attached or service item is not at DELETING status.

	params := &dynamodb.DeleteItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(serviceMemberPartitionKeyPrefix + serviceUUID),
			},
			tableSortKey: {
				S: aws.String(strconv.FormatInt(memberIndex, 10)),
			},
		},
		ConditionExpression:    aws.String(tableSortKeyDelCondition),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}

	resp, err := dbsvc.DeleteItem(params)

	if err != nil {
		if err.(awserr.Error).Code() == ConditionalCheckFailedException {
			glog.Infoln("serviceMember not found", memberIndex, "serviceUUID", serviceUUID, "requuid", requuid, "resp", resp)
			return db.ErrDBRecordNotFound
		}
		glog.Errorln("failed to delete serviceMember", memberIndex,
			"serviceUUID", serviceUUID, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("deleted serviceMember", memberIndex, "serviceUUID", serviceUUID, "requuid", requuid)
	return nil
}

func (d *DynamoDB) attrsToServiceMember(serviceUUID string, item map[string]*dynamodb.AttributeValue) (*common.ServiceMember, error) {
	mtime, err := strconv.ParseInt(*(item[LastModified].N), 10, 64)
	if err != nil {
		glog.Errorln("ParseInt LastModified error", err, item)
		return nil, db.ErrDBInternal
	}

	var configs []*common.MemberConfig
	err = json.Unmarshal(item[MemberConfigs].B, &configs)
	if err != nil {
		glog.Errorln("Unmarshal json MemberConfigs error", err, item)
		return nil, db.ErrDBInternal
	}

	var volumes common.MemberVolumes
	err = json.Unmarshal(item[MemberVolumes].B, &volumes)
	if err != nil {
		glog.Errorln("Unmarshal json MemberVolumes error", err, item)
		return nil, db.ErrDBInternal
	}

	memberIndex, err := strconv.ParseInt(*(item[tableSortKey].S), 10, 64)
	if err != nil {
		glog.Errorln("parse MemberIndex error", err, item)
		return nil, db.ErrDBInternal
	}

	member := db.CreateServiceMember(serviceUUID,
		memberIndex,
		*(item[MemberName].S),
		*(item[AvailableZone].S),
		*(item[TaskID].S),
		*(item[ContainerInstanceID].S),
		*(item[ServerInstanceID].S),
		mtime,
		volumes,
		*(item[StaticIP].S),
		configs)

	return member, nil
}

// UpdateServiceMemberVolume updates the ServiceMember's volume in DB
func (d *DynamoDB) UpdateServiceMemberVolume(ctx context.Context, member *common.ServiceMember, newVolID string, badVolID string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	if member.Volumes.JournalVolumeID != badVolID && member.Volumes.PrimaryVolumeID != badVolID {
		glog.Errorln("the bad volume", badVolID, "does not belong to member", member, member.Volumes)
		return errors.New("the bad volume does not belong to member")
	}

	if member.Volumes.JournalVolumeID == badVolID {
		member.Volumes.JournalVolumeID = newVolID
		glog.Infoln("replace the journal volume", badVolID, "with new volume", newVolID, "requuid", requuid, member)
	} else {
		member.Volumes.PrimaryVolumeID = newVolID
		glog.Infoln("replace the data volume", badVolID, "with new volume", newVolID, "requuid", requuid, member)
	}

	volBytes, err := json.Marshal(member.Volumes)
	if err != nil {
		glog.Errorln("Marshal MemberVolumes error", err, member, "requuid", requuid)
		return err
	}

	dbsvc := dynamodb.New(d.sess)

	updateExpr := "SET " + MemberVolumes + " = :v1"

	params := &dynamodb.UpdateItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(serviceMemberPartitionKeyPrefix + member.ServiceUUID),
			},
			tableSortKey: {
				S: aws.String(strconv.FormatInt(member.MemberIndex, 10)),
			},
		},
		UpdateExpression: aws.String(updateExpr),
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":v1": {
				B: volBytes,
			},
		},
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}

	_, err = dbsvc.UpdateItem(params)

	if err != nil {
		glog.Errorln("failed to update serviceMember", member, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("updated serviceMember to use new volume", newVolID, "bad volume", badVolID, "requuid", requuid, member)
	return nil
}
