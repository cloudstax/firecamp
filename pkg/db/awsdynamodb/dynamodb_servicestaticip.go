package awsdynamodb

import (
	"encoding/json"
	"strconv"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/pkg/db"
	"github.com/cloudstax/firecamp/pkg/utils"
)

// CreateServiceStaticIP creates one ServiceStaticIP in DB
func (d *DynamoDB) CreateServiceStaticIP(ctx context.Context, serviceip *common.ServiceStaticIP) error {
	requuid := utils.GetReqIDFromContext(ctx)

	specBytes, err := json.Marshal(serviceip.Spec)
	if err != nil {
		glog.Errorln("Marshal StaticIPSpec error", err, "requuid", requuid, serviceip)
		return err
	}

	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.PutItemInput{
		TableName: aws.String(d.tableName),
		Item: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(staticIPPartitionKeyPrefix + serviceip.StaticIP),
			},
			tableSortKey: {
				S: aws.String(staticIPPartitionKeyPrefix),
			},
			db.Revision: {
				N: aws.String(strconv.FormatInt(serviceip.Revision, 10)),
			},
			db.StaticIPSpec: {
				B: specBytes,
			},
		},
		ConditionExpression: aws.String(tablePartitionKeyPutCondition),
	}

	_, err = dbsvc.PutItem(params)
	if err != nil {
		glog.Errorln("failed to create service static ip", serviceip, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("created service static ip", serviceip, "requuid", requuid)
	return nil
}

// UpdateServiceStaticIP updates the ServiceStaticIP in DB
func (d *DynamoDB) UpdateServiceStaticIP(ctx context.Context, oldIP *common.ServiceStaticIP, newIP *common.ServiceStaticIP) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// sanity check. ServiceUUID, AvailableZone, etc, are not allowed to update.
	if oldIP.StaticIP != newIP.StaticIP ||
		oldIP.Revision+1 != newIP.Revision ||
		oldIP.Spec.ServiceUUID != newIP.Spec.ServiceUUID ||
		oldIP.Spec.AvailableZone != newIP.Spec.AvailableZone {
		glog.Errorln("immutable attributes are updated, oldIP", oldIP, "newIP", newIP, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	specBytes, err := json.Marshal(newIP.Spec)
	if err != nil {
		glog.Errorln("Marshal StaticIPSpec error", err, "requuid", requuid, newIP)
		return err
	}

	dbsvc := dynamodb.New(d.sess)

	updateExpr := "SET " + db.Revision + " = :v1, " + db.StaticIPSpec + " = :v2"
	conditionExpr := db.Revision + " = :cv1"
	params := &dynamodb.UpdateItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(staticIPPartitionKeyPrefix + oldIP.StaticIP),
			},
			tableSortKey: {
				S: aws.String(staticIPPartitionKeyPrefix),
			},
		},
		UpdateExpression:    aws.String(updateExpr),
		ConditionExpression: aws.String(conditionExpr),
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":v1": {
				N: aws.String(strconv.FormatInt(newIP.Revision, 10)),
			},
			":v2": {
				B: specBytes,
			},
			":cv1": {
				N: aws.String(strconv.FormatInt(oldIP.Revision, 10)),
			},
		},
	}

	_, err = dbsvc.UpdateItem(params)
	if err != nil {
		glog.Errorln("failed to update service static ip", oldIP, "to", newIP, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("updated service static ip", oldIP, "to", newIP, "requuid", requuid)
	return nil
}

// GetServiceStaticIP gets the ServiceStaticIP from DB
func (d *DynamoDB) GetServiceStaticIP(ctx context.Context, staticIP string) (serviceip *common.ServiceStaticIP, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(staticIPPartitionKeyPrefix + staticIP),
			},
			tableSortKey: {
				S: aws.String(staticIPPartitionKeyPrefix),
			},
		},
		ConsistentRead: aws.Bool(true),
	}

	resp, err := dbsvc.GetItem(params)
	if err != nil {
		glog.Errorln("failed to get service static ip", staticIP, "error", err, "requuid", requuid)
		return nil, d.convertError(err)
	}

	if len(resp.Item) == 0 {
		glog.Infoln("service static ip", staticIP, "not found, requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	revision, err := strconv.ParseInt(*(resp.Item[db.Revision].N), 10, 64)
	if err != nil {
		glog.Errorln("ParseInt Revision error", err, "requuid", requuid, "resp", resp)
		return nil, db.ErrDBInternal
	}
	var spec common.StaticIPSpec
	err = json.Unmarshal(resp.Item[db.StaticIPSpec].B, &spec)
	if err != nil {
		glog.Errorln("Unmarshal StaticIPSpec error", err, "requuid", requuid, resp)
		return nil, db.ErrDBInternal
	}

	serviceip = db.CreateServiceStaticIP(staticIP, revision, &spec)

	glog.Infoln("get service static ip", serviceip, "requuid", requuid)
	return serviceip, nil
}

// DeleteServiceStaticIP deletes the service static ip from DB
func (d *DynamoDB) DeleteServiceStaticIP(ctx context.Context, staticIP string) error {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.DeleteItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(staticIPPartitionKeyPrefix + staticIP),
			},
			tableSortKey: {
				S: aws.String(staticIPPartitionKeyPrefix),
			},
		},
		ConditionExpression: aws.String(tablePartitionKeyDelCondition),
	}

	resp, err := dbsvc.DeleteItem(params)

	if err != nil {
		if err.(awserr.Error).Code() == ConditionalCheckFailedException {
			glog.Infoln("service static ip not found", staticIP, "requuid", requuid, "resp", resp)
			return db.ErrDBRecordNotFound
		}
		glog.Errorln("failed to delete service static ip", staticIP, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("deleted service static ip", staticIP, "requuid", requuid)
	return nil
}
