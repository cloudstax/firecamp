package awsdynamodb

import (
	"encoding/json"
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

// The ServiceAttr does not need sort key. Every service will have only one ServiceAttr.
// The ServiceAttr is uniquely represented by the ServiceUUID.

// CreateServiceAttr puts a new ServiceAttr record into DB
func (d *DynamoDB) CreateServiceAttr(ctx context.Context, attr *common.ServiceAttr) error {
	requuid := utils.GetReqIDFromContext(ctx)

	volBytes, err := json.Marshal(attr.Volumes)
	if err != nil {
		glog.Errorln("Marshal ServiceVolumes error", err, "requuid", requuid, attr)
		return err
	}
	resBytes, err := json.Marshal(attr.Resource)
	if err != nil {
		glog.Errorln("Marshal Resources error", err, "requuid", requuid, attr.Resource, attr)
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
			ServiceStatus: {
				S: aws.String(attr.ServiceStatus),
			},
			LastModified: {
				N: aws.String(strconv.FormatInt(attr.LastModified, 10)),
			},
			Replicas: {
				N: aws.String(strconv.FormatInt(attr.Replicas, 10)),
			},
			ClusterName: {
				S: aws.String(attr.ClusterName),
			},
			ServiceName: {
				S: aws.String(attr.ServiceName),
			},
			ServiceVolumes: {
				B: volBytes,
			},
			RegisterDNS: {
				BOOL: aws.Bool(attr.RegisterDNS),
			},
			DomainName: {
				S: aws.String(attr.DomainName),
			},
			HostedZoneID: {
				S: aws.String(attr.HostedZoneID),
			},
			RequireStaticIP: {
				BOOL: aws.Bool(attr.RequireStaticIP),
			},
			Resource: {
				B: resBytes,
			},
		},
		ConditionExpression:    aws.String(tablePartitionKeyPutCondition),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}
	if attr.UserAttr != nil {
		userAttrBytes, err := json.Marshal(attr.UserAttr)
		if err != nil {
			glog.Errorln("Marshal ServiceUserAttr error", err, "requuid", requuid, attr)
			return err
		}
		params.Item[UserAttr] = &dynamodb.AttributeValue{
			B: userAttrBytes,
		}
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
// Only support updating ServiceStatus or Replicas at v1, all other attributes are immutable.
func (d *DynamoDB) UpdateServiceAttr(ctx context.Context, oldAttr *common.ServiceAttr, newAttr *common.ServiceAttr) error {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

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
	}

	if oldAttr.ServiceStatus != newAttr.ServiceStatus {
		glog.Infoln("update service status from", oldAttr.ServiceStatus, "to", newAttr.ServiceStatus, "requuid", requuid, newAttr)

		updateExpr := "SET " + ServiceStatus + " = :v1, " + LastModified + " = :v2"
		conditionExpr := ServiceStatus + " = :cv1"
		params.UpdateExpression = aws.String(updateExpr)
		params.ConditionExpression = aws.String(conditionExpr)
		params.ExpressionAttributeValues = map[string]*dynamodb.AttributeValue{
			":v1": {
				S: aws.String(newAttr.ServiceStatus),
			},
			":v2": {
				N: aws.String(strconv.FormatInt(newAttr.LastModified, 10)),
			},
			":cv1": {
				S: aws.String(oldAttr.ServiceStatus),
			},
		}
	} else if oldAttr.Replicas != newAttr.Replicas {
		glog.Infoln("update service replicas from", oldAttr.Replicas, "to", newAttr.Replicas, "requuid", requuid, newAttr)

		updateExpr := "SET " + Replicas + " = :v1, " + LastModified + " = :v2"
		conditionExpr := Replicas + " = :cv1"
		params.UpdateExpression = aws.String(updateExpr)
		params.ConditionExpression = aws.String(conditionExpr)
		params.ExpressionAttributeValues = map[string]*dynamodb.AttributeValue{
			":v1": {
				N: aws.String(strconv.FormatInt(newAttr.Replicas, 10)),
			},
			":v2": {
				N: aws.String(strconv.FormatInt(newAttr.LastModified, 10)),
			},
			":cv1": {
				N: aws.String(strconv.FormatInt(oldAttr.Replicas, 10)),
			},
		}
	} else if newAttr.UserAttr != nil && !db.EqualServiceUserAttr(oldAttr.UserAttr, newAttr.UserAttr) {
		// update service user attr
		glog.Infoln("update user attr, oldAttr", oldAttr, "newAttr", newAttr, "requuid", requuid)

		userAttrBytes, err := json.Marshal(newAttr.UserAttr)
		if err != nil {
			glog.Errorln("Marshal ServiceUserAttr error", err, "requuid", requuid, newAttr)
			return err
		}

		updateExpr := "SET " + UserAttr + " = :v1, " + LastModified + " = :v2"
		conditionExpr := LastModified + " = :cv1"
		params.UpdateExpression = aws.String(updateExpr)
		params.ConditionExpression = aws.String(conditionExpr)
		params.ExpressionAttributeValues = map[string]*dynamodb.AttributeValue{
			":v1": {
				B: userAttrBytes,
			},
			":v2": {
				N: aws.String(strconv.FormatInt(newAttr.LastModified, 10)),
			},
			":cv1": {
				N: aws.String(strconv.FormatInt(oldAttr.LastModified, 10)),
			},
		}
	} else {
		glog.Errorln("not supported attr update, oldAttr", oldAttr, "newAttr", newAttr, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	_, err := dbsvc.UpdateItem(params)

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

	replicas, err := strconv.ParseInt(*(resp.Item[Replicas].N), 10, 64)
	if err != nil {
		glog.Errorln("Atoi Replicas error", err, "requuid", requuid, "resp", resp)
		return nil, db.ErrDBInternal
	}
	mtime, err := strconv.ParseInt(*(resp.Item[LastModified].N), 10, 64)
	if err != nil {
		glog.Errorln("ParseInt LastModified error", err, "requuid", requuid, "resp", resp)
		return nil, db.ErrDBInternal
	}
	var vols common.ServiceVolumes
	err = json.Unmarshal(resp.Item[ServiceVolumes].B, &vols)
	if err != nil {
		glog.Errorln("Unmarshal ServiceVolumes error", err, "requuid", requuid, resp)
		return nil, db.ErrDBInternal
	}
	var userAttr *common.ServiceUserAttr
	if _, ok := resp.Item[UserAttr]; ok {
		tmpAttr := &common.ServiceUserAttr{}
		err = json.Unmarshal(resp.Item[UserAttr].B, tmpAttr)
		if err != nil {
			glog.Errorln("Unmarshal ServiceUserAttr error", err, "requuid", requuid, resp)
			return nil, db.ErrDBInternal
		}
		userAttr = tmpAttr
	}
	res := common.Resources{
		MaxCPUUnits:     common.DefaultMaxCPUUnits,
		ReserveCPUUnits: common.DefaultReserveCPUUnits,
		MaxMemMB:        common.DefaultMaxMemoryMB,
		ReserveMemMB:    common.DefaultReserveMemoryMB,
	}
	if _, ok := resp.Item[Resource]; ok {
		err = json.Unmarshal(resp.Item[Resource].B, &res)
		if err != nil {
			glog.Errorln("Unmarshal Resource error", err, "requuid", requuid, resp)
			return nil, db.ErrDBInternal
		}
	}

	attr = db.CreateServiceAttr(
		serviceUUID,
		*(resp.Item[ServiceStatus].S),
		mtime,
		replicas,
		*(resp.Item[ClusterName].S),
		*(resp.Item[ServiceName].S),
		vols,
		*(resp.Item[RegisterDNS].BOOL),
		*(resp.Item[DomainName].S),
		*(resp.Item[HostedZoneID].S),
		*(resp.Item[RequireStaticIP].BOOL),
		userAttr,
		res)

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
		ConditionExpression:    aws.String(tablePartitionKeyDelCondition),
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

	glog.Infoln("deleted service attr", serviceUUID, "requuid", requuid)
	return nil
}
