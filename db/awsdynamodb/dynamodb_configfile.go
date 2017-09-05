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

// CreateConfigFile creates one config file in DB
func (d *DynamoDB) CreateConfigFile(ctx context.Context, cfg *common.ConfigFile) error {
	requuid := utils.GetReqIDFromContext(ctx)
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
			ConfigFileMD5: {
				S: aws.String(cfg.FileMD5),
			},
			ConfigFileName: {
				S: aws.String(cfg.FileName),
			},
			ConfigFileMode: {
				N: aws.String(strconv.FormatUint(uint64(cfg.FileMode), 10)),
			},
			LastModified: {
				N: aws.String(strconv.FormatInt(cfg.LastModified, 10)),
			},
			ConfigFileContent: {
				S: aws.String(cfg.Content),
			},
		},
		ConditionExpression:    aws.String(tableSortKeyPutCondition),
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

	mtime, err := strconv.ParseInt(*(resp.Item[LastModified].N), 10, 64)
	if err != nil {
		glog.Errorln("ParseInt LastModified error", err, "requuid", requuid, "resp", resp)
		return nil, db.ErrDBInternal
	}

	mode, err := strconv.ParseUint(*(resp.Item[ConfigFileMode].N), 10, 64)
	if err != nil {
		glog.Errorln("ParseUint FileMode error", err, "requuid", requuid, "resp", resp)
		return nil, db.ErrDBInternal
	}

	cfg, err = db.CreateConfigFile(serviceUUID,
		fileID,
		*(resp.Item[ConfigFileMD5].S),
		*(resp.Item[ConfigFileName].S),
		uint32(mode),
		mtime,
		*(resp.Item[ConfigFileContent].S))
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
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(configPartitionKeyPrefix + serviceUUID),
			},
			tableSortKey: {
				S: aws.String(fileID),
			},
		},
		ConditionExpression:    aws.String(tableSortKeyDelCondition),
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
