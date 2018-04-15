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

// CreateConfigFile creates one config file in DB
func (d *DynamoDB) CreateConfigFile(ctx context.Context, cfg *common.ConfigFile) error {
	requuid := utils.GetReqIDFromContext(ctx)

	metaBytes, err := json.Marshal(cfg.Meta)
	if err != nil {
		glog.Errorln("Marshal ConfigFileMeta error", err, "requuid", requuid, cfg)
		return err
	}
	specBytes, err := json.Marshal(cfg.Spec)
	if err != nil {
		glog.Errorln("Marshal ConfigFileSpec error", err, "requuid", requuid, cfg)
		return err
	}

	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.PutItemInput{
		TableName: aws.String(d.tableName),
		Item: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(configPartitionKeyPrefix + cfg.ServiceUUID),
			},
			tableSortKey: {
				S: aws.String(cfg.FileID),
			},
			db.Revision: {
				N: aws.String(strconv.FormatInt(cfg.Revision, 10)),
			},
			db.ConfigFileMeta: {
				B: metaBytes,
			},
			db.ConfigFileSpec: {
				B: specBytes,
			},
		},
		ConditionExpression: aws.String(tableSortKeyPutCondition),
	}

	_, err = dbsvc.PutItem(params)
	if err != nil {
		glog.Errorln("failed to create config file", cfg.Meta.FileName, cfg.FileID,
			"serviceUUID", cfg.ServiceUUID, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("created config file", cfg.Meta.FileName, cfg.FileID, "serviceUUID", cfg.ServiceUUID, "requuid", requuid)
	return nil
}

// GetConfigFile gets the config fileItem from DB
func (d *DynamoDB) GetConfigFile(ctx context.Context, serviceUUID string, fileID string) (cfg *common.ConfigFile, err error) {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(configPartitionKeyPrefix + serviceUUID),
			},
			tableSortKey: {
				S: aws.String(fileID),
			},
		},
		ConsistentRead: aws.Bool(true),
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

	revision, err := strconv.ParseInt(*(resp.Item[db.Revision].N), 10, 64)
	if err != nil {
		glog.Errorln("ParseInt Revision error", err, "requuid", requuid, "resp", resp)
		return nil, db.ErrDBInternal
	}
	var meta common.ConfigFileMeta
	err = json.Unmarshal(resp.Item[db.ConfigFileMeta].B, &meta)
	if err != nil {
		glog.Errorln("Unmarshal ConfigFileMeta error", err, "requuid", requuid, resp)
		return nil, db.ErrDBInternal
	}
	var spec common.ConfigFileSpec
	err = json.Unmarshal(resp.Item[db.ConfigFileSpec].B, &spec)
	if err != nil {
		glog.Errorln("Unmarshal ConfigFileSpec error", err, "requuid", requuid, resp)
		return nil, db.ErrDBInternal
	}

	cfg = db.CreateConfigFile(serviceUUID, fileID, revision, &meta, &spec)

	glog.Infoln("get config file", cfg.Meta.FileName, cfg.FileID, "serviceUUID", cfg.ServiceUUID, "requuid", requuid)
	return cfg, nil
}

// DeleteConfigFile deletes the config file from DB
func (d *DynamoDB) DeleteConfigFile(ctx context.Context, serviceUUID string, fileID string) error {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	// TODO reject if any config file is still attached or service item is not at DELETING status.

	params := &dynamodb.DeleteItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(configPartitionKeyPrefix + serviceUUID),
			},
			tableSortKey: {
				S: aws.String(fileID),
			},
		},
		ConditionExpression: aws.String(tableSortKeyDelCondition),
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

	glog.Infoln("deleted config file", fileID, "serviceUUID", serviceUUID, "requuid", requuid)
	return nil
}
