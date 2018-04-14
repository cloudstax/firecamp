package awsdynamodb

import (
	"encoding/json"
	"fmt"
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

	metaBytes, err := json.Marshal(member.Meta)
	if err != nil {
		glog.Errorln("Marshal MemberMeta error", err, member, "requuid", requuid)
		return err
	}
	specBytes, err := json.Marshal(member.Spec)
	if err != nil {
		glog.Errorln("Marshal MemberSpec error", err, member, "requuid", requuid)
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
			db.Revision: {
				N: aws.String(strconv.FormatInt(member.Revision, 10)),
			},
			db.MemberMeta: {
				B: metaBytes,
			},
			db.MemberSpec: {
				B: specBytes,
			},
		},
		ConditionExpression: aws.String(tableSortKeyPutCondition),
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

	if oldMember.Revision+1 != newMember.Revision ||
		!db.EqualServiceMemberImmutableFields(oldMember, newMember) {
		glog.Errorln("revision is not increased by 1 or immutable attributes are updated, oldMember",
			oldMember, "newMember", newMember, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	metaBytes, err := json.Marshal(newMember.Meta)
	if err != nil {
		glog.Errorln("Marshal MemberMeta error", err, "requuid", requuid, newMember)
		return err
	}
	specBytes, err := json.Marshal(newMember.Spec)
	if err != nil {
		glog.Errorln("Marshal MemberSpec error", err, "requuid", requuid, newMember)
		return err
	}

	dbsvc := dynamodb.New(d.sess)

	updateExpr := fmt.Sprintf("SET %s = :v1, %s = :v2, %s = :v3", db.Revision, db.MemberMeta, db.MemberSpec)
	conditionExpr := fmt.Sprintf("%s = :cv1", db.Revision)

	params := &dynamodb.UpdateItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(serviceMemberPartitionKeyPrefix + newMember.ServiceUUID),
			},
			tableSortKey: {
				S: aws.String(strconv.FormatInt(newMember.MemberIndex, 10)),
			},
		},
		UpdateExpression:    aws.String(updateExpr),
		ConditionExpression: aws.String(conditionExpr),
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":v1": {
				N: aws.String(strconv.FormatInt(newMember.Revision, 10)),
			},
			":v2": {
				B: metaBytes,
			},
			":v3": {
				B: specBytes,
			},
			":cv1": {
				N: aws.String(strconv.FormatInt(oldMember.Revision, 10)),
			},
		},
	}

	_, err = dbsvc.UpdateItem(params)
	if err != nil {
		glog.Errorln("update serviceMember error", err, "requuid", requuid)
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
		ConsistentRead: aws.Bool(true),
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

	lastEvaluatedKey := map[string]*dynamodb.AttributeValue{}

	for true {
		params := &dynamodb.QueryInput{
			TableName:              aws.String(d.tableName),
			KeyConditionExpression: aws.String(tablePartitionKey + " = :v1"),
			ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
				":v1": {
					S: aws.String(serviceMemberPartitionKeyPrefix + serviceUUID),
				},
			},
			ConsistentRead: aws.Bool(true),
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
			serviceUUID, "limit", limit, "requuid", requuid, "resp count", *resp.Count)

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
		ConditionExpression: aws.String(tableSortKeyDelCondition),
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
	revision, err := strconv.ParseInt(*(item[db.Revision].N), 10, 64)
	if err != nil {
		glog.Errorln("ParseInt Revision error", err, item)
		return nil, db.ErrDBInternal
	}

	var meta common.MemberMeta
	err = json.Unmarshal(item[db.MemberMeta].B, &meta)
	if err != nil {
		glog.Errorln("Unmarshal MemberMeta error", err, item)
		return nil, db.ErrDBInternal
	}

	var spec common.MemberSpec
	err = json.Unmarshal(item[db.MemberSpec].B, &spec)
	if err != nil {
		glog.Errorln("Unmarshal MemberSpec error", err, item)
		return nil, db.ErrDBInternal
	}

	memberIndex, err := strconv.ParseInt(*(item[tableSortKey].S), 10, 64)
	if err != nil {
		glog.Errorln("parse MemberIndex error", err, item)
		return nil, db.ErrDBInternal
	}

	member := db.CreateServiceMember(serviceUUID, memberIndex, revision, &meta, &spec)
	return member, nil
}
