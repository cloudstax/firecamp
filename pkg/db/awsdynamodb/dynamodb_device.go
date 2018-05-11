package awsdynamodb

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/pkg/db"
	"github.com/cloudstax/firecamp/pkg/utils"
)

// CreateDevice puts a new Device into DB
func (d *DynamoDB) CreateDevice(ctx context.Context, dev *common.Device) error {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.PutItemInput{
		TableName: aws.String(d.tableName),
		Item: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(devicePartitionKeyPrefix + dev.ClusterName),
			},
			tableSortKey: {
				S: aws.String(dev.DeviceName),
			},
			db.ServiceName: {
				S: aws.String(dev.ServiceName),
			},
		},
		ConditionExpression: aws.String(tableSortKeyPutCondition),
	}
	_, err := dbsvc.PutItem(params)

	if err != nil {
		glog.Errorln("failed to create device", dev, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("created device", dev, "requuid", requuid)
	return nil
}

// GetDevice gets the device from DB
func (d *DynamoDB) GetDevice(ctx context.Context, clusterName string, deviceName string) (dev *common.Device, err error) {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(devicePartitionKeyPrefix + clusterName),
			},
			tableSortKey: {
				S: aws.String(deviceName),
			},
		},
		ConsistentRead: aws.Bool(true),
	}
	resp, err := dbsvc.GetItem(params)

	if err != nil {
		glog.Errorln("failed to get device", deviceName, "cluster", clusterName, "error", err, "requuid", requuid)
		return nil, d.convertError(err)
	}

	if len(resp.Item) == 0 {
		glog.Infoln("device", deviceName, "not found, cluster", clusterName, "requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	dev = db.CreateDevice(clusterName, deviceName, *(resp.Item[db.ServiceName].S))

	glog.Infoln("get device", dev, "requuid", requuid)
	return dev, nil
}

// DeleteDevice deletes the Device from DB.
// The caller should make sure the service is deleted.
func (d *DynamoDB) DeleteDevice(ctx context.Context, clusterName string, deviceName string) error {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.DeleteItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(devicePartitionKeyPrefix + clusterName),
			},
			tableSortKey: {
				S: aws.String(deviceName),
			},
		},
		ConditionExpression: aws.String(tableSortKeyDelCondition),
	}

	resp, err := dbsvc.DeleteItem(params)

	if err != nil {
		if err.(awserr.Error).Code() == ConditionalCheckFailedException {
			glog.Infoln("device not found", deviceName, "cluster", clusterName, "requuid", requuid, "resp", resp)
			return db.ErrDBRecordNotFound
		}
		glog.Errorln("failed to delete device", deviceName,
			"cluster", clusterName, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("deleted device", deviceName, "cluster", clusterName, "requuid", requuid)
	return nil
}

// ListDevices lists all Devices
func (d *DynamoDB) ListDevices(ctx context.Context, clusterName string) (devs []*common.Device, err error) {
	return d.listDevicesWithLimit(ctx, clusterName, 0)
}

func (d *DynamoDB) listDevicesWithLimit(ctx context.Context, clusterName string, limit int64) (devs []*common.Device, err error) {
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
					S: aws.String(devicePartitionKeyPrefix + clusterName),
				},
			},
			ConsistentRead: aws.Bool(true),
		}
		if limit > 0 {
			params.Limit = aws.Int64(limit)
		}
		if len(lastEvaluatedKey) != 0 {
			params.ExclusiveStartKey = lastEvaluatedKey
		}

		resp, err := dbsvc.Query(params)

		if err != nil {
			glog.Errorln("failed to list devices, cluster", clusterName, "error", err, "requuid", requuid)
			return nil, d.convertError(err)
		}

		lastEvaluatedKey = resp.LastEvaluatedKey

		if len(resp.Items) == 0 {
			// is it possible dynamodb returns no items with LastEvaluatedKey?
			// we don't use complex filter, so would be impossible?
			if len(resp.LastEvaluatedKey) != 0 {
				glog.Errorln("no items in resp but LastEvaluatedKey is not empty, resp", resp, "requuid", requuid)
				continue
			}

			glog.Infoln("no more device in cluster", clusterName, "Devices", len(devs), "requuid", requuid)
			return devs, nil
		}

		for _, item := range resp.Items {
			dev := db.CreateDevice(clusterName, *(item[tableSortKey].S), *(item[db.ServiceName].S))
			devs = append(devs, dev)
		}

		glog.Infoln("list", len(devs), "devices, cluster", clusterName, "LastEvaluatedKey", lastEvaluatedKey, "requuid", requuid)

		if len(lastEvaluatedKey) == 0 {
			// no more Devices
			break
		}
	}

	return devs, nil
}
