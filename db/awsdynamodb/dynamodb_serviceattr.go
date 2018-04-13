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
			db.ServiceStatus: {
				S: aws.String(attr.ServiceStatus),
			},
			db.LastModified: {
				N: aws.String(strconv.FormatInt(attr.LastModified, 10)),
			},
			db.Replicas: {
				N: aws.String(strconv.FormatInt(attr.Replicas, 10)),
			},
			db.ClusterName: {
				S: aws.String(attr.ClusterName),
			},
			db.ServiceName: {
				S: aws.String(attr.ServiceName),
			},
			db.ServiceVolumes: {
				B: volBytes,
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
			db.RequireStaticIP: {
				BOOL: aws.Bool(attr.RequireStaticIP),
			},
			db.Resource: {
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
		params.Item[db.UserAttr] = &dynamodb.AttributeValue{
			B: userAttrBytes,
		}
	}
	if len(attr.ServiceConfigs) != 0 {
		cfgBytes, err := json.Marshal(attr.ServiceConfigs)
		if err != nil {
			glog.Errorln("Marshal ServiceConfigs error", err, "requuid", requuid, attr)
			return err
		}
		params.Item[db.ServiceConfigs] = &dynamodb.AttributeValue{
			B: cfgBytes,
		}
	}
	if len(attr.ServiceType) != 0 {
		params.Item[db.ServiceType] = &dynamodb.AttributeValue{
			S: aws.String(attr.ServiceType),
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
// Only support updating ServiceStatus, Replicas, ServiceConfigs or UserAttr at v1, all other attributes are immutable.
func (d *DynamoDB) UpdateServiceAttr(ctx context.Context, oldAttr *common.ServiceAttr, newAttr *common.ServiceAttr) error {
	requuid := utils.GetReqIDFromContext(ctx)

	if oldAttr.ClusterName != newAttr.ClusterName ||
		oldAttr.DomainName != newAttr.DomainName ||
		oldAttr.HostedZoneID != newAttr.HostedZoneID ||
		oldAttr.RegisterDNS != newAttr.RegisterDNS ||
		oldAttr.RequireStaticIP != newAttr.RequireStaticIP ||
		oldAttr.ServiceName != newAttr.ServiceName ||
		!db.EqualResources(&oldAttr.Resource, &newAttr.Resource) ||
		!db.EqualServiceVolumes(&oldAttr.Volumes, &newAttr.Volumes) ||
		oldAttr.ServiceType != newAttr.ServiceType {
		glog.Errorln("immutable fields could not be updated, oldAttr", oldAttr, "newAttr", newAttr, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

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

	if oldAttr.ServiceStatus != newAttr.ServiceStatus ||
		oldAttr.Replicas != newAttr.Replicas ||
		!db.EqualConfigs(oldAttr.ServiceConfigs, newAttr.ServiceConfigs) {

		glog.Infoln("update service status, replicas or configs from", oldAttr, "to", newAttr, "requuid", requuid)

		oldCfgBytes, err := json.Marshal(oldAttr.ServiceConfigs)
		if err != nil {
			glog.Errorln("Marshal old ServiceConfigs error", err, "requuid", requuid, oldAttr)
			return err
		}
		newCfgBytes, err := json.Marshal(newAttr.ServiceConfigs)
		if err != nil {
			glog.Errorln("Marshal new ServiceConfigs error", err, "requuid", requuid, newAttr)
			return err
		}

		updateExpr := "SET " + db.ServiceStatus + " = :v1, " + db.Replicas + " = :v2, " + db.ServiceConfigs + " = :v3, " + db.LastModified + " = :v4"
		conditionExpr := db.ServiceStatus + " = :cv1 AND " + db.Replicas + " = :cv2 AND " + db.ServiceConfigs + " = :cv3"
		params.UpdateExpression = aws.String(updateExpr)
		params.ConditionExpression = aws.String(conditionExpr)
		params.ExpressionAttributeValues = map[string]*dynamodb.AttributeValue{
			":v1": {
				S: aws.String(newAttr.ServiceStatus),
			},
			":v2": {
				N: aws.String(strconv.FormatInt(newAttr.Replicas, 10)),
			},
			":v3": {
				B: newCfgBytes,
			},
			":v4": {
				N: aws.String(strconv.FormatInt(newAttr.LastModified, 10)),
			},
			":cv1": {
				S: aws.String(oldAttr.ServiceStatus),
			},
			":cv2": {
				N: aws.String(strconv.FormatInt(oldAttr.Replicas, 10)),
			},
			":cv3": {
				B: oldCfgBytes,
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

		updateExpr := "SET " + db.UserAttr + " = :v1, " + db.LastModified + " = :v2"
		conditionExpr := db.LastModified + " = :cv1"
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

	replicas, err := strconv.ParseInt(*(resp.Item[db.Replicas].N), 10, 64)
	if err != nil {
		glog.Errorln("Atoi Replicas error", err, "requuid", requuid, "resp", resp)
		return nil, db.ErrDBInternal
	}
	mtime, err := strconv.ParseInt(*(resp.Item[db.LastModified].N), 10, 64)
	if err != nil {
		glog.Errorln("ParseInt LastModified error", err, "requuid", requuid, "resp", resp)
		return nil, db.ErrDBInternal
	}
	var vols common.ServiceVolumes
	err = json.Unmarshal(resp.Item[db.ServiceVolumes].B, &vols)
	if err != nil {
		glog.Errorln("Unmarshal ServiceVolumes error", err, "requuid", requuid, resp)
		return nil, db.ErrDBInternal
	}
	var userAttr *common.ServiceUserAttr
	if _, ok := resp.Item[db.UserAttr]; ok {
		tmpAttr := &common.ServiceUserAttr{}
		err = json.Unmarshal(resp.Item[db.UserAttr].B, tmpAttr)
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
	if _, ok := resp.Item[db.Resource]; ok {
		err = json.Unmarshal(resp.Item[db.Resource].B, &res)
		if err != nil {
			glog.Errorln("Unmarshal Resource error", err, "requuid", requuid, resp)
			return nil, db.ErrDBInternal
		}
	}
	serviceType := ""
	if _, ok := resp.Item[db.ServiceType]; ok {
		serviceType = *(resp.Item[db.ServiceType].S)
	}
	var cfgs []*common.ConfigID
	if _, ok := resp.Item[db.ServiceConfigs]; ok {
		err = json.Unmarshal(resp.Item[db.ServiceConfigs].B, &cfgs)
		if err != nil {
			glog.Errorln("Unmarshal ServiceConfigs error", err, "requuid", requuid, resp)
			return nil, db.ErrDBInternal
		}
	}

	attr = db.CreateServiceAttr(
		serviceUUID,
		*(resp.Item[db.ServiceStatus].S),
		mtime,
		replicas,
		*(resp.Item[db.ClusterName].S),
		*(resp.Item[db.ServiceName].S),
		vols,
		*(resp.Item[db.RegisterDNS].BOOL),
		*(resp.Item[db.DomainName].S),
		*(resp.Item[db.HostedZoneID].S),
		*(resp.Item[db.RequireStaticIP].BOOL),
		userAttr,
		cfgs,
		res,
		serviceType)

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
