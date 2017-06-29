package awsdynamodb

import (
	"encoding/json"
	"strconv"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/db"
	"github.com/cloudstax/openmanage/utils"
)

// CreateVolume creates one EBS volume in DB
func (d *DynamoDB) CreateVolume(ctx context.Context, vol *common.Volume) error {
	requuid := utils.GetReqIDFromContext(ctx)
	configBytes, err := json.Marshal(vol.Configs)
	if err != nil {
		glog.Errorln("Marshal MemberConfigs error", err, vol, "requuid", requuid)
		return err
	}

	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.PutItemInput{
		TableName: aws.String(d.volumeTableName),
		Item: map[string]*dynamodb.AttributeValue{
			db.ServiceUUID: {
				S: aws.String(vol.ServiceUUID),
			},
			db.VolumeID: {
				S: aws.String(vol.VolumeID),
			},
			db.LastModified: {
				N: aws.String(strconv.FormatInt(vol.LastModified, 10)),
			},
			db.DeviceName: {
				S: aws.String(vol.DeviceName),
			},
			db.AvailableZone: {
				S: aws.String(vol.AvailableZone),
			},
			db.TaskID: {
				S: aws.String(vol.TaskID),
			},
			db.ContainerInstanceID: {
				S: aws.String(vol.ContainerInstanceID),
			},
			db.ServerInstanceID: {
				S: aws.String(vol.ServerInstanceID),
			},
			db.MemberName: {
				S: aws.String(vol.MemberName),
			},
			db.MemberConfigs: {
				B: configBytes,
			},
		},
		ConditionExpression:    aws.String(db.VolumePutCondition),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}
	resp, err := dbsvc.PutItem(params)

	if err != nil {
		glog.Errorln("failed to create volume", vol, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("created volume", vol, "requuid", requuid, "resp", resp)
	return nil
}

// UpdateVolume updates the db.Volume in DB
func (d *DynamoDB) UpdateVolume(ctx context.Context, oldVol *common.Volume, newVol *common.Volume) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// sanity check. ServiceUUID, VolumeID, etc, are not allowed to update.
	if oldVol.ServiceUUID != newVol.ServiceUUID ||
		oldVol.VolumeID != newVol.VolumeID ||
		oldVol.DeviceName != newVol.DeviceName ||
		oldVol.AvailableZone != newVol.AvailableZone ||
		oldVol.MemberName != newVol.MemberName {
		glog.Errorln("immutable attributes are updated, oldVol", oldVol, "newVol", newVol, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	var err error
	var oldCfgBytes []byte
	if oldVol.Configs != nil {
		oldCfgBytes, err = json.Marshal(oldVol.Configs)
		if err != nil {
			glog.Errorln("Marshal new MemberConfigs error", err, "requuid", requuid, oldVol.Configs)
			return err
		}
	}

	var newCfgBytes []byte
	if newVol.Configs != nil {
		newCfgBytes, err = json.Marshal(newVol.Configs)
		if err != nil {
			glog.Errorln("Marshal new MemberConfigs error", err, "requuid", requuid, newVol.Configs)
			return err
		}
	}

	dbsvc := dynamodb.New(d.sess)

	updateExpr := "SET " + db.TaskID + " = :v1, " + db.ContainerInstanceID + " = :v2, " +
		db.ServerInstanceID + " = :v3, " + db.LastModified + " = :v4, " + db.MemberConfigs + " = :v5"
	conditionExpr := db.TaskID + " = :cv1 AND " + db.ContainerInstanceID + " = :cv2 AND " +
		db.ServerInstanceID + " = :cv3 AND " + db.MemberConfigs + " = :cv4"

	params := &dynamodb.UpdateItemInput{
		TableName: aws.String(d.volumeTableName),
		Key: map[string]*dynamodb.AttributeValue{
			db.ServiceUUID: {
				S: aws.String(oldVol.ServiceUUID),
			},
			db.VolumeID: {
				S: aws.String(oldVol.VolumeID),
			},
		},
		UpdateExpression:    aws.String(updateExpr),
		ConditionExpression: aws.String(conditionExpr),
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":v1": {
				S: aws.String(newVol.TaskID),
			},
			":v2": {
				S: aws.String(newVol.ContainerInstanceID),
			},
			":v3": {
				S: aws.String(newVol.ServerInstanceID),
			},
			":v4": {
				N: aws.String(strconv.FormatInt(newVol.LastModified, 10)),
			},
			":v5": {
				B: newCfgBytes,
			},
			":cv1": {
				S: aws.String(oldVol.TaskID),
			},
			":cv2": {
				S: aws.String(oldVol.ContainerInstanceID),
			},
			":cv3": {
				S: aws.String(oldVol.ServerInstanceID),
			},
			":cv4": {
				B: oldCfgBytes,
			},
		},
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}

	resp, err := dbsvc.UpdateItem(params)

	if err != nil {
		glog.Errorln("failed to update volume", oldVol, "to", newVol, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("updated volume", oldVol, "to", newVol, "requuid", requuid, "resp", resp)
	return nil
}

// GetVolume gets the volumeItem from DB
func (d *DynamoDB) GetVolume(ctx context.Context, serviceUUID string, volumeID string) (vol *common.Volume, err error) {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.GetItemInput{
		TableName: aws.String(d.volumeTableName),
		Key: map[string]*dynamodb.AttributeValue{
			db.ServiceUUID: {
				S: aws.String(serviceUUID),
			},
			db.VolumeID: {
				S: aws.String(volumeID),
			},
		},
		ConsistentRead:         aws.Bool(true),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}
	resp, err := dbsvc.GetItem(params)

	if err != nil {
		glog.Errorln("failed to get volume", volumeID, "serviceUUID", serviceUUID, "error", err, "requuid", requuid)
		return nil, d.convertError(err)
	}

	if len(resp.Item) == 0 {
		glog.Infoln("volume", volumeID, "not found, serviceUUID", serviceUUID, "requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	vol, err = d.attrsToVolume(resp.Item)
	if err != nil {
		glog.Errorln("GetVolume convert dynamodb attributes to volume error", err, "requuid", requuid, "resp", resp)
		return nil, err
	}

	glog.Infoln("get volume", vol, "requuid", requuid)
	return vol, nil
}

// ListVolumes lists all volumes of the service
func (d *DynamoDB) ListVolumes(ctx context.Context, serviceUUID string) (volumes []*common.Volume, err error) {
	return d.listVolumesWithLimit(ctx, serviceUUID, 0)
}

// listVolumesWithLimit limits the returned db.Volumes at one query.
// This is for testing the pagination list.
func (d *DynamoDB) listVolumesWithLimit(ctx context.Context, serviceUUID string, limit int64) (volumes []*common.Volume, err error) {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	var lastEvaluatedKey map[string]*dynamodb.AttributeValue
	lastEvaluatedKey = nil

	for true {
		params := &dynamodb.QueryInput{
			TableName:              aws.String(d.volumeTableName),
			KeyConditionExpression: aws.String(db.ServiceUUID + " = :v1"),
			ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
				":v1": {
					S: aws.String(serviceUUID),
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
			glog.Errorln("failed to list volumes, serviceUUID", serviceUUID,
				"limit", limit, "lastEvaluatedKey", lastEvaluatedKey, "error", err, "requuid", requuid)
			return nil, d.convertError(err)
		}

		glog.Infoln("list volumes succeeded, serviceUUID",
			serviceUUID, "limit", limit, "requuid", requuid, "resp count", resp.Count)

		lastEvaluatedKey = resp.LastEvaluatedKey

		if len(resp.Items) == 0 {
			// is it possible dynamodb returns no items with LastEvaluatedKey?
			// we don't use complex filter, so would be impossible?
			if len(resp.LastEvaluatedKey) != 0 {
				glog.Errorln("no items in resp but LastEvaluatedKey is not empty, resp", resp, "requuid", requuid)
				continue
			}

			glog.Infoln("no more volume item for serviceUUID",
				serviceUUID, "volumes", len(volumes), "requuid", requuid)
			return volumes, nil
		}

		for _, item := range resp.Items {
			vol, err := d.attrsToVolume(item)
			if err != nil {
				glog.Errorln("ListVolume convert dynamodb attributes to volume error", err, "requuid", requuid, "item", item)
				return nil, err
			}
			volumes = append(volumes, vol)
		}

		glog.Infoln("list", len(volumes), "volumes, serviceUUID",
			serviceUUID, "LastEvaluatedKey", lastEvaluatedKey, "requuid", requuid)

		if len(lastEvaluatedKey) == 0 {
			// no more volumes
			break
		}
	}

	return volumes, nil
}

// DeleteVolume deletes the volume from DB
func (d *DynamoDB) DeleteVolume(ctx context.Context, serviceUUID string, volumeID string) error {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	// TODO reject if any volume is still attached or service item is not at DELETING status.

	params := &dynamodb.DeleteItemInput{
		TableName: aws.String(d.volumeTableName),
		Key: map[string]*dynamodb.AttributeValue{
			db.ServiceUUID: {
				S: aws.String(serviceUUID),
			},
			db.VolumeID: {
				S: aws.String(volumeID),
			},
		},
		ConditionExpression:    aws.String(db.VolumeDelCondition),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}

	resp, err := dbsvc.DeleteItem(params)

	if err != nil {
		if err.(awserr.Error).Code() == ConditionalCheckFailedException {
			glog.Infoln("volume not found", volumeID, "serviceUUID", serviceUUID, "requuid", requuid, "resp", resp)
			return db.ErrDBRecordNotFound
		}
		glog.Errorln("failed to delete volume", volumeID,
			"serviceUUID", serviceUUID, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("deleted volume", volumeID, "serviceUUID", serviceUUID, "requuid", requuid, "resp", resp)
	return nil
}

func (d *DynamoDB) attrsToVolume(item map[string]*dynamodb.AttributeValue) (*common.Volume, error) {
	mtime, err := strconv.ParseInt(*(item[db.LastModified].N), 10, 64)
	if err != nil {
		glog.Errorln("ParseInt LastModified error", err, item)
		return nil, db.ErrDBInternal
	}

	var configs []*common.MemberConfig
	err = json.Unmarshal(item[db.MemberConfigs].B, &configs)
	if err != nil {
		glog.Errorln("Unmarshal json MemberConfigs error", err, item)
		return nil, db.ErrDBInternal
	}

	vol := db.CreateVolume(*(item[db.ServiceUUID].S),
		*(item[db.VolumeID].S),
		mtime,
		*(item[db.DeviceName].S),
		*(item[db.AvailableZone].S),
		*(item[db.TaskID].S),
		*(item[db.ContainerInstanceID].S),
		*(item[db.ServerInstanceID].S),
		*(item[db.MemberName].S),
		configs)

	return vol, nil
}
