package awsdynamodb

import (
	"encoding/json"
	"strconv"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/jazzl0ver/firecamp/pkg/db"
	"github.com/jazzl0ver/firecamp/pkg/utils"
)

// The ServiceAttr does not need sort key. Every service will have only one ServiceAttr.
// The ServiceAttr is uniquely represented by the ServiceUUID.

// CreateServiceAttr puts a new ServiceAttr record into DB
func (d *DynamoDB) CreateServiceAttr(ctx context.Context, attr *common.ServiceAttr) error {
	requuid := utils.GetReqIDFromContext(ctx)

	metaBytes, err := json.Marshal(attr.Meta)
	if err != nil {
		glog.Errorln("Marshal ServiceMeta error", err, "requuid", requuid, attr)
		return err
	}
	specBytes, err := json.Marshal(attr.Spec)
	if err != nil {
		glog.Errorln("Marshal ServiceSpec error", err, "requuid", requuid, attr)
		return err
	}

	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.PutItemInput{
		TableName: aws.String(d.tableName),
		Item: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(serviceAttrPartitionKeyPrefix + attr.ServiceUUID),
			},
			tableSortKey: {
				S: aws.String(serviceAttrPartitionKeyPrefix),
			},
			db.Revision: {
				N: aws.String(strconv.FormatInt(attr.Revision, 10)),
			},
			db.ServiceMeta: {
				B: metaBytes,
			},
			db.ServiceSpec: {
				B: specBytes,
			},
		},
		ConditionExpression: aws.String(tablePartitionKeyPutCondition),
	}

	_, err = dbsvc.PutItem(params)

	if err != nil {
		glog.Errorln("failed to create service attr", attr, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("created service attr", attr, "requuid", requuid)
	return nil
}

// UpdateServiceAttr updates the ServiceAttr in DB.
// Only support updating ServiceStatus, Replicas, ServiceConfigs or UserAttr at v1, all other attributes are immutable.
func (d *DynamoDB) UpdateServiceAttr(ctx context.Context, oldAttr *common.ServiceAttr, newAttr *common.ServiceAttr) error {
	requuid := utils.GetReqIDFromContext(ctx)

	if (oldAttr.Revision+1) != newAttr.Revision ||
		!db.EqualServiceAttrImmutableFields(oldAttr, newAttr) {
		glog.Errorln("revision not increased by 1 or immutable fields are updated, oldAttr", oldAttr, "newAttr", newAttr, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	dbsvc := dynamodb.New(d.sess)

	metaBytes, err := json.Marshal(newAttr.Meta)
	if err != nil {
		glog.Errorln("Marshal ServiceMeta error", err, "requuid", requuid, newAttr)
		return err
	}
	specBytes, err := json.Marshal(newAttr.Spec)
	if err != nil {
		glog.Errorln("Marshal ServiceSpec error", err, "requuid", requuid, newAttr)
		return err
	}

	updateExpr := "SET " + db.Revision + " = :v1, " + db.ServiceMeta + " = :v2, " + db.ServiceSpec + " = :v3"
	conditionExpr := db.Revision + " = :cv1"
	params := &dynamodb.UpdateItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(serviceAttrPartitionKeyPrefix + oldAttr.ServiceUUID),
			},
			tableSortKey: {
				S: aws.String(serviceAttrPartitionKeyPrefix),
			},
		},
		UpdateExpression:    aws.String(updateExpr),
		ConditionExpression: aws.String(conditionExpr),
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":v1": {
				N: aws.String(strconv.FormatInt(newAttr.Revision, 10)),
			},
			":v2": {
				B: metaBytes,
			},
			":v3": {
				B: specBytes,
			},
			":cv1": {
				N: aws.String(strconv.FormatInt(oldAttr.Revision, 10)),
			},
		},
	}

	_, err = dbsvc.UpdateItem(params)
	if err != nil {
		glog.Errorln("failed to update service attr", oldAttr, "to", newAttr, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("updated service attr", oldAttr, "to", newAttr, "requuid", requuid)
	return nil
}

// GetServiceAttr gets the ServiceAttr from DB
func (d *DynamoDB) GetServiceAttr(ctx context.Context, serviceUUID string) (attr *common.ServiceAttr, err error) {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(serviceAttrPartitionKeyPrefix + serviceUUID),
			},
			tableSortKey: {
				S: aws.String(serviceAttrPartitionKeyPrefix),
			},
		},
		ConsistentRead: aws.Bool(true),
	}

	resp, err := dbsvc.GetItem(params)
	if err != nil {
		glog.Errorln("failed to get service attr", serviceUUID, "error", err, "requuid", requuid)
		return nil, d.convertError(err)
	}

	if len(resp.Item) == 0 {
		glog.Infoln("service attr not found, serviceUUID", serviceUUID, "requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	revision, err := strconv.ParseInt(*(resp.Item[db.Revision].N), 10, 64)
	if err != nil {
		glog.Errorln("ParseInt Revision error", err, "requuid", requuid, "resp", resp)
		return nil, db.ErrDBInternal
	}
	var meta common.ServiceMeta
	err = json.Unmarshal(resp.Item[db.ServiceMeta].B, &meta)
	if err != nil {
		glog.Errorln("Unmarshal ServiceMeta error", err, "requuid", requuid, resp)
		return nil, db.ErrDBInternal
	}
	var spec common.ServiceSpec
	err = json.Unmarshal(resp.Item[db.ServiceSpec].B, &spec)
	if err != nil {
		glog.Errorln("Unmarshal ServiceSpec error", err, "requuid", requuid, resp)
		return nil, db.ErrDBInternal
	}

	attr = db.CreateServiceAttr(serviceUUID, revision, &meta, &spec)

	glog.Infoln("get service attr", attr, "requuid", requuid)
	return attr, nil
}

// DeleteServiceAttr deletes the service attr from DB
func (d *DynamoDB) DeleteServiceAttr(ctx context.Context, serviceUUID string) error {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	// TODO reject if any serviceMember is still mounted, e.g. task still running.
	// should we reject if some serviceMember still exists? probably not, as aws ecs allows service to be deleted with serviceMembers left.

	params := &dynamodb.DeleteItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(serviceAttrPartitionKeyPrefix + serviceUUID),
			},
			tableSortKey: {
				S: aws.String(serviceAttrPartitionKeyPrefix),
			},
		},
		ConditionExpression: aws.String(tablePartitionKeyDelCondition),
	}

	resp, err := dbsvc.DeleteItem(params)

	if err != nil {
		if err.(awserr.Error).Code() == ConditionalCheckFailedException {
			glog.Infoln("service attr not found", serviceUUID, "requuid", requuid, "resp", resp)
			return db.ErrDBRecordNotFound
		}
		glog.Errorln("failed to delete service attr", serviceUUID, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("deleted service attr", serviceUUID, "requuid", requuid)
	return nil
}
