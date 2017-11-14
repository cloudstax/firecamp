package awsdynamodb

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/utils"
)

// CreateServiceStaticIP creates one ServiceStaticIP in DB
func (d *DynamoDB) CreateServiceStaticIP(ctx context.Context, serviceip *common.ServiceStaticIP) error {
	requuid := utils.GetReqIDFromContext(ctx)

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
			ServiceUUID: {
				S: aws.String(serviceip.ServiceUUID),
			},
			AvailableZone: {
				S: aws.String(serviceip.AvailableZone),
			},
			ServerInstanceID: {
				S: aws.String(serviceip.ServerInstanceID),
			},
			NetworkInterfaceID: {
				S: aws.String(serviceip.NetworkInterfaceID),
			},
		},
		ConditionExpression:    aws.String(tablePartitionKeyPutCondition),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}
	_, err := dbsvc.PutItem(params)

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
		oldIP.ServiceUUID != newIP.ServiceUUID ||
		oldIP.AvailableZone != newIP.AvailableZone {
		glog.Errorln("immutable attributes are updated, oldIP", oldIP, "newIP", newIP, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	dbsvc := dynamodb.New(d.sess)

	updateExpr := "SET " + ServerInstanceID + " = :v1, " + NetworkInterfaceID + " = :v2"
	conditionExpr := ServerInstanceID + " = :cv1 AND " + NetworkInterfaceID + " = :cv2"

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
				S: aws.String(newIP.ServerInstanceID),
			},
			":v2": {
				S: aws.String(newIP.NetworkInterfaceID),
			},
			":cv1": {
				S: aws.String(oldIP.ServerInstanceID),
			},
			":cv2": {
				S: aws.String(oldIP.NetworkInterfaceID),
			},
		},
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}

	_, err := dbsvc.UpdateItem(params)

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
		ConsistentRead:         aws.Bool(true),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
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

	serviceip = db.CreateServiceStaticIP(
		staticIP,
		*(resp.Item[ServiceUUID].S),
		*(resp.Item[AvailableZone].S),
		*(resp.Item[ServerInstanceID].S),
		*(resp.Item[NetworkInterfaceID].S))

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
		ConditionExpression:    aws.String(tablePartitionKeyDelCondition),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
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
