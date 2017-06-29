package awsdynamodb

import (
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

// CreateConfigFile creates one config file in DB
func (d *DynamoDB) CreateConfigFile(ctx context.Context, cfg *common.ConfigFile) error {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.PutItemInput{
		TableName: aws.String(d.configTableName),
		Item: map[string]*dynamodb.AttributeValue{
			db.ServiceUUID: {
				S: aws.String(cfg.ServiceUUID),
			},
			db.ConfigFileID: {
				S: aws.String(cfg.FileID),
			},
			db.ConfigFileMD5: {
				S: aws.String(cfg.FileMD5),
			},
			db.ConfigFileName: {
				S: aws.String(cfg.FileName),
			},
			db.ConfigFileMode: {
				N: aws.String(strconv.FormatUint(uint64(cfg.FileMode), 10)),
			},
			db.LastModified: {
				N: aws.String(strconv.FormatInt(cfg.LastModified, 10)),
			},
			db.ConfigFileContent: {
				S: aws.String(cfg.Content),
			},
		},
		ConditionExpression:    aws.String(db.ConfigFilePutCondition),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}
	resp, err := dbsvc.PutItem(params)

	if err != nil {
		glog.Errorln("failed to create config file", cfg.FileName, cfg.FileID,
			"serviceUUID", cfg.ServiceUUID, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("created config file", cfg.FileName, cfg.FileID,
		"serviceUUID", cfg.ServiceUUID, "requuid", requuid, "resp", resp)
	return nil
}

// UpdateConfigFile updates the config file
func (d *DynamoDB) UpdateConfigFile(ctx context.Context, oldCfg *common.ConfigFile, newCfg *common.ConfigFile) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// sanity check. Only Content, MD5, and LastModified are allowed to update.
	if oldCfg.ServiceUUID != newCfg.ServiceUUID ||
		oldCfg.FileID != newCfg.FileID ||
		oldCfg.FileName != newCfg.FileName ||
		oldCfg.FileMode != newCfg.FileMode {
		glog.Errorln("immutable attributes are updated, oldCfg", db.PrintConfigFile(oldCfg),
			"newCfg", db.PrintConfigFile(newCfg), "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	dbsvc := dynamodb.New(d.sess)

	updateExpr := "SET " + db.ConfigFileMD5 + " = :v1, " + db.ConfigFileContent + " = :v2, " + db.LastModified + " = :v3"
	conditionExpr := db.ConfigFileMD5 + " = :cv1 AND " + db.LastModified + " = :cv2"

	params := &dynamodb.UpdateItemInput{
		TableName: aws.String(d.configTableName),
		Key: map[string]*dynamodb.AttributeValue{
			db.ServiceUUID: {
				S: aws.String(oldCfg.ServiceUUID),
			},
			db.ConfigFileID: {
				S: aws.String(oldCfg.FileID),
			},
		},
		UpdateExpression:    aws.String(updateExpr),
		ConditionExpression: aws.String(conditionExpr),
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":v1": {
				S: aws.String(newCfg.FileMD5),
			},
			":v2": {
				S: aws.String(newCfg.Content),
			},
			":v3": {
				N: aws.String(strconv.FormatInt(newCfg.LastModified, 10)),
			},
			":cv1": {
				S: aws.String(oldCfg.FileMD5),
			},
			":cv2": {
				N: aws.String(strconv.FormatInt(oldCfg.LastModified, 10)),
			},
		},
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}

	resp, err := dbsvc.UpdateItem(params)

	if err != nil {
		glog.Errorln("failed to update config file", db.PrintConfigFile(oldCfg),
			"to", db.PrintConfigFile(newCfg), "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("updated config file", db.PrintConfigFile(oldCfg),
		"to", db.PrintConfigFile(newCfg), "requuid", requuid, "resp", resp)
	return nil
}

// GetConfigFile gets the config fileItem from DB
func (d *DynamoDB) GetConfigFile(ctx context.Context, serviceUUID string, fileID string) (cfg *common.ConfigFile, err error) {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.GetItemInput{
		TableName: aws.String(d.configTableName),
		Key: map[string]*dynamodb.AttributeValue{
			db.ServiceUUID: {
				S: aws.String(serviceUUID),
			},
			db.ConfigFileID: {
				S: aws.String(fileID),
			},
		},
		ConsistentRead:         aws.Bool(true),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}
	resp, err := dbsvc.GetItem(params)

	if err != nil {
		glog.Errorln("failed to get config file", fileID, "serviceUUID", serviceUUID, "error", err, "requuid", requuid)
		return nil, d.convertError(err)
	}

	if len(resp.Item) == 0 {
		glog.Infoln("config file", fileID, "not found, serviceUUID", serviceUUID, "requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	mtime, err := strconv.ParseInt(*(resp.Item[db.LastModified].N), 10, 64)
	if err != nil {
		glog.Errorln("ParseInt LastModified error", err, "requuid", requuid, "resp", resp)
		return nil, db.ErrDBInternal
	}

	mode, err := strconv.ParseUint(*(resp.Item[db.ConfigFileMode].N), 10, 64)
	if err != nil {
		glog.Errorln("ParseUint FileMode error", err, "requuid", requuid, "resp", resp)
		return nil, db.ErrDBInternal
	}

	cfg, err = db.CreateConfigFile(*(resp.Item[db.ServiceUUID].S),
		*(resp.Item[db.ConfigFileID].S),
		*(resp.Item[db.ConfigFileMD5].S),
		*(resp.Item[db.ConfigFileName].S),
		uint32(mode),
		mtime,
		*(resp.Item[db.ConfigFileContent].S))
	if err != nil {
		glog.Errorln("CreateConfigFile error", err, "fileID", fileID, "serviceUUID", serviceUUID, "requuid", requuid)
		return nil, err
	}

	glog.Infoln("get config file", cfg.FileName, cfg.FileID, "serviceUUID", cfg.ServiceUUID, "requuid", requuid)
	return cfg, nil
}

// DeleteConfigFile deletes the config file from DB
func (d *DynamoDB) DeleteConfigFile(ctx context.Context, serviceUUID string, fileID string) error {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	// TODO reject if any config file is still attached or service item is not at DELETING status.

	params := &dynamodb.DeleteItemInput{
		TableName: aws.String(d.configTableName),
		Key: map[string]*dynamodb.AttributeValue{
			db.ServiceUUID: {
				S: aws.String(serviceUUID),
			},
			db.ConfigFileID: {
				S: aws.String(fileID),
			},
		},
		ConditionExpression:    aws.String(db.ConfigFileDelCondition),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}

	resp, err := dbsvc.DeleteItem(params)

	if err != nil {
		if err.(awserr.Error).Code() == ConditionalCheckFailedException {
			glog.Infoln("config file not found", fileID, "serviceUUID", serviceUUID, "requuid", requuid, "resp", resp)
			return db.ErrDBRecordNotFound
		}
		glog.Errorln("failed to delete config file", fileID,
			"serviceUUID", serviceUUID, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("deleted config file", fileID, "serviceUUID", serviceUUID, "requuid", requuid, "resp", resp)
	return nil
}
