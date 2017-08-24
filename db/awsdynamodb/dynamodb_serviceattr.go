package awsdynamodb

import (
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

// CreateServiceAttr puts a new db.ServiceAttr record into DB
func (d *DynamoDB) CreateServiceAttr(ctx context.Context, attr *common.ServiceAttr) error {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.PutItemInput{
		TableName: aws.String(d.serviceAttrTableName),
		Item: map[string]*dynamodb.AttributeValue{
			db.ServiceUUID: {
				S: aws.String(attr.ServiceUUID),
			},
			db.ServiceStatus: {
				S: aws.String(attr.ServiceStatus),
			},
			db.LastModified: {
				N: aws.String(strconv.FormatInt(attr.LastModified, 10)),
			},
			db.Replicas: {
				N: aws.String(strconv.FormatInt(attr.Replicas, 10)),
			},
			db.VolumeSizeGB: {
				N: aws.String(strconv.FormatInt(attr.VolumeSizeGB, 10)),
			},
			db.ClusterName: {
				S: aws.String(attr.ClusterName),
			},
			db.ServiceName: {
				S: aws.String(attr.ServiceName),
			},
			db.DeviceName: {
				S: aws.String(attr.DeviceName),
			},
			db.RegisterDNS: {
				BOOL: aws.Bool(attr.RegisterDNS),
			},
			db.DomainName: {
				S: aws.String(attr.DomainName),
			},
			db.HostedZoneID: {
				S: aws.String(attr.HostedZoneID),
			},
		},
		ConditionExpression:    aws.String(db.ServiceAttrPutCondition),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}
	resp, err := dbsvc.PutItem(params)

	if err != nil {
		glog.Errorln("failed to create service attr", attr, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("created service attr", attr, "requuid", requuid, "resp", resp)
	return nil
}

// UpdateServiceAttr updates the db.ServiceAttr in DB.
// Only support updating ServiceStatus at v1, all other attributes are immutable.
// TODO support Replicas and VolumeSizeGB change.
func (d *DynamoDB) UpdateServiceAttr(ctx context.Context, oldAttr *common.ServiceAttr, newAttr *common.ServiceAttr) error {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	updateExpr := "SET " + db.ServiceStatus + " = :v1, " + db.LastModified + " = :v2"
	conditionExpr := db.ServiceStatus + " = :cv1"

	params := &dynamodb.UpdateItemInput{
		TableName: aws.String(d.serviceAttrTableName),
		Key: map[string]*dynamodb.AttributeValue{
			db.ServiceUUID: {
				S: aws.String(oldAttr.ServiceUUID),
			},
		},
		UpdateExpression:    aws.String(updateExpr),
		ConditionExpression: aws.String(conditionExpr),
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":v1": {
				S: aws.String(newAttr.ServiceStatus),
			},
			":v2": {
				N: aws.String(strconv.FormatInt(newAttr.LastModified, 10)),
			},
			":cv1": {
				S: aws.String(oldAttr.ServiceStatus),
			},
		},
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}

	resp, err := dbsvc.UpdateItem(params)

	if err != nil {
		glog.Errorln("failed to update service attr", oldAttr, "to", newAttr, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("updated service attr", oldAttr, "to", newAttr, "requuid", requuid, "resp", resp)
	return nil
}

// GetServiceAttr gets the db.ServiceAttr from DB
func (d *DynamoDB) GetServiceAttr(ctx context.Context, serviceUUID string) (attr *common.ServiceAttr, err error) {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.GetItemInput{
		TableName: aws.String(d.serviceAttrTableName),
		Key: map[string]*dynamodb.AttributeValue{
			db.ServiceUUID: {
				S: aws.String(serviceUUID),
			},
		},
		ConsistentRead:         aws.Bool(true),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
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

	replicas, err := strconv.ParseInt(*(resp.Item[db.Replicas].N), 10, 64)
	if err != nil {
		glog.Errorln("Atoi Replicas error", err, "requuid", requuid, "resp", resp)
		return nil, db.ErrDBInternal
	}
	volSize, err := strconv.ParseInt(*(resp.Item[db.VolumeSizeGB].N), 10, 64)
	if err != nil {
		glog.Errorln("ParseInt VolumeSizeGB error", err, "requuid", requuid, "resp", resp)
		return nil, db.ErrDBInternal
	}
	mtime, err := strconv.ParseInt(*(resp.Item[db.LastModified].N), 10, 64)
	if err != nil {
		glog.Errorln("ParseInt LastModified error", err, "requuid", requuid, "resp", resp)
		return nil, db.ErrDBInternal
	}

	attr = db.CreateServiceAttr(
		*(resp.Item[db.ServiceUUID].S),
		*(resp.Item[db.ServiceStatus].S),
		mtime,
		replicas,
		volSize,
		*(resp.Item[db.ClusterName].S),
		*(resp.Item[db.ServiceName].S),
		*(resp.Item[db.DeviceName].S),
		*(resp.Item[db.RegisterDNS].BOOL),
		*(resp.Item[db.DomainName].S),
		*(resp.Item[db.HostedZoneID].S))

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
		TableName: aws.String(d.serviceAttrTableName),
		Key: map[string]*dynamodb.AttributeValue{
			db.ServiceUUID: {
				S: aws.String(serviceUUID),
			},
		},
		ConditionExpression:    aws.String(db.ServiceAttrDelCondition),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
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

	glog.Infoln("deleted service attr", serviceUUID, "requuid", requuid, "resp", resp)
	return nil
}
