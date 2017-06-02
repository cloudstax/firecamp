package awsdynamodb

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/db"
)

// DynamoDB implements DB interfae on dynamodb
type DynamoDB struct {
	// see https://docs.aws.amazon.com/sdk-for-go/api/aws/session/
	// session could and should be shared when possible.
	// Creating service clients concurrently from a shared Session is safe.
	sess                 *session.Session
	deviceTableName      string
	serviceTableName     string
	serviceAttrTableName string
	volumeTableName      string
	configTableName      string
}

// DynamoDB related const
const (
	InternalServerError                    = "InternalServerError"
	LimitExceededException                 = "LimitExceededException"
	TableInUseException                    = "TableInUseException"
	ResourceNotFoundException              = "ResourceNotFoundException"
	TableNotFoundException                 = "TableNotFoundException"
	ConditionalCheckFailedException        = "ConditionalCheckFailedException"
	ProvisionedThroughputExceededException = "ProvisionedThroughputExceededException"

	defaultReadCapacity  = 5
	defaultWriteCapacity = 5
)

// NewDynamoDB allocates a new DynamoDB instance
//
// DB requirements: 1) conditional creation/update. 2) strong consistency on get/list.
//
// DynamoDB could easily achieve both.
// Azure table has insert and update APIs.
//   Q: sounds Azure table is strong consistency for the single key? as table builds on top of
//   Azure storage, which is strongly consistent.
//   - insert: assume creating new entry, and will return EntityAlreadyExists if entry exists
//   - update: support etag, assume updating existing entry, and will return error if entry doesn't exist
//   [1] https://azure.microsoft.com/en-us/documentation/articles/storage-table-design-guide/
//   [2] https://msdn.microsoft.com/en-us/library/dd894033.aspx
// GCP datastore also provides the similar NoSQL DB. Looks guarantee strong consistency is more complex?
//   - upsert, overwrite an entity if exists. Q: return error if not exist?
//   - insert, requires the entity key not already exist. see example code, actually use
//     RunInTx: get check and then put. Q: conditional update follows the same way?
//   - lookup, retrieves an entity. "strong consistency (an ancestor query, or a lookup of a single
//     entity)"[1].
//   Q: sounds datastore is strong consistency for the single key operations (upsert, insert, lookup)?
//   - list, "Ancestor queries (those that execute against an entity group) are strongly
//     consistent"[1].
//   [1] https://cloud.google.com/datastore/docs/concepts/structuring_for_strong_consistency
//   [2] https://cloud.google.com/datastore/docs/concepts/entities
//   [3] https://cloud.google.com/datastore/docs/best-practices
//   [4] https://cloud.google.com/datastore/docs/concepts/transaction
func NewDynamoDB(sess *session.Session) *DynamoDB {
	d := new(DynamoDB)
	d.sess = sess
	d.deviceTableName = db.DeviceTableName
	d.serviceTableName = db.ServiceTableName
	d.serviceAttrTableName = db.ServiceAttrTableName
	d.volumeTableName = db.VolumeTableName
	d.configTableName = db.ConfigTableName
	return d
}

// NewTestDynamoDB creates a DynamoDB instance for test
func NewTestDynamoDB(sess *session.Session, tableNameSuffix string) *DynamoDB {
	d := new(DynamoDB)
	d.sess = sess
	d.deviceTableName = "TestDevTable" + tableNameSuffix
	d.serviceTableName = "TestSvcTable" + tableNameSuffix
	d.serviceAttrTableName = "TestSvcAttrTable" + tableNameSuffix
	d.volumeTableName = "TestVolTable" + tableNameSuffix
	d.configTableName = "TestCfgTable" + tableNameSuffix
	return d
}

// convert the error to the common db error code
func (d *DynamoDB) convertError(err error) error {
	switch err.(awserr.Error).Code() {
	case LimitExceededException:
		return db.ErrDBLimitExceeded
	case TableInUseException:
		return db.ErrDBTableInUse
	case TableNotFoundException:
		return db.ErrDBTableNotFound
	case ResourceNotFoundException:
		return db.ErrDBResourceNotFound
	case ConditionalCheckFailedException:
		return db.ErrDBConditionalCheckFailed
	case ProvisionedThroughputExceededException:
		return db.ErrDBLimitExceeded
	default:
		return db.ErrDBInternal
	}
}

// CreateSystemTables creates device/service/volume tables
func (d *DynamoDB) CreateSystemTables(ctx context.Context) error {
	dbsvc := dynamodb.New(d.sess)

	// create device table
	err := d.createDeviceTable(dbsvc)
	if err != nil {
		glog.Errorln("createDeviceTable failed", err)
		return err
	}

	// create service table
	err = d.createServiceTable(dbsvc)
	if err != nil {
		glog.Errorln("createServiceTable failed", err)
		return err
	}

	// create service attr table
	err = d.createServiceAttrTable(dbsvc)
	if err != nil {
		glog.Errorln("createServiceAttrTable failed", err)
		return err
	}

	// create volume table
	err = d.createVolumeTable(dbsvc)
	if err != nil {
		glog.Errorln("createVolumeTable failed", err)
		return err
	}

	// create config table
	err = d.createConfigTable(dbsvc)
	if err != nil {
		glog.Errorln("createConfigTable failed", err)
		return err
	}

	return nil
}

func (d *DynamoDB) createDeviceTable(dbsvc *dynamodb.DynamoDB) error {
	params := &dynamodb.CreateTableInput{
		TableName: aws.String(d.deviceTableName),
		AttributeDefinitions: []*dynamodb.AttributeDefinition{
			{
				AttributeName: aws.String(db.ClusterName),
				AttributeType: aws.String(dynamodb.ScalarAttributeTypeS),
			},
			{
				AttributeName: aws.String(db.DeviceName),
				AttributeType: aws.String(dynamodb.ScalarAttributeTypeS),
			},
		},
		KeySchema: []*dynamodb.KeySchemaElement{
			{
				AttributeName: aws.String(db.ClusterName),
				KeyType:       aws.String(dynamodb.KeyTypeHash),
			},
			{
				AttributeName: aws.String(db.DeviceName),
				KeyType:       aws.String(dynamodb.KeyTypeRange),
			},
		},
		ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
			ReadCapacityUnits:  aws.Int64(defaultReadCapacity),
			WriteCapacityUnits: aws.Int64(defaultWriteCapacity),
		},
	}
	resp, err := dbsvc.CreateTable(params)

	if err != nil && err.(awserr.Error).Code() != TableInUseException {
		glog.Errorln("failed to create table", d.deviceTableName, "error", err)
		return d.convertError(err)
	}

	glog.Infoln("device table", d.deviceTableName, "created or exists, resp", resp)
	return nil
}

func (d *DynamoDB) createServiceTable(dbsvc *dynamodb.DynamoDB) error {
	params := &dynamodb.CreateTableInput{
		TableName: aws.String(d.serviceTableName),
		AttributeDefinitions: []*dynamodb.AttributeDefinition{
			{
				AttributeName: aws.String(db.ClusterName),
				AttributeType: aws.String(dynamodb.ScalarAttributeTypeS),
			},
			{
				AttributeName: aws.String(db.ServiceName),
				AttributeType: aws.String(dynamodb.ScalarAttributeTypeS),
			},
		},
		KeySchema: []*dynamodb.KeySchemaElement{
			{
				AttributeName: aws.String(db.ClusterName),
				KeyType:       aws.String(dynamodb.KeyTypeHash),
			},
			{
				AttributeName: aws.String(db.ServiceName),
				KeyType:       aws.String(dynamodb.KeyTypeRange),
			},
		},
		ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
			ReadCapacityUnits:  aws.Int64(defaultReadCapacity),
			WriteCapacityUnits: aws.Int64(defaultWriteCapacity),
		},
	}
	resp, err := dbsvc.CreateTable(params)

	if err != nil && err.(awserr.Error).Code() != TableInUseException {
		glog.Errorln("failed to create table", d.serviceTableName, "error", err)
		return d.convertError(err)
	}

	glog.Infoln("service table", d.serviceTableName, "created or exists, resp", resp)
	return nil
}

func (d *DynamoDB) createServiceAttrTable(dbsvc *dynamodb.DynamoDB) error {
	params := &dynamodb.CreateTableInput{
		TableName: aws.String(d.serviceAttrTableName),
		AttributeDefinitions: []*dynamodb.AttributeDefinition{
			{
				AttributeName: aws.String(db.ServiceUUID),
				AttributeType: aws.String(dynamodb.ScalarAttributeTypeS),
			},
		},
		KeySchema: []*dynamodb.KeySchemaElement{
			{
				AttributeName: aws.String(db.ServiceUUID),
				KeyType:       aws.String(dynamodb.KeyTypeHash),
			},
		},
		ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
			ReadCapacityUnits:  aws.Int64(defaultReadCapacity),
			WriteCapacityUnits: aws.Int64(defaultWriteCapacity),
		},
	}
	resp, err := dbsvc.CreateTable(params)

	if err != nil && err.(awserr.Error).Code() != TableInUseException {
		glog.Errorln("failed to create table", d.serviceAttrTableName, "error", err)
		return d.convertError(err)
	}

	glog.Infoln("service attr table", d.serviceAttrTableName, "created or exists, resp", resp)
	return nil
}

func (d *DynamoDB) createVolumeTable(dbsvc *dynamodb.DynamoDB) error {
	params := &dynamodb.CreateTableInput{
		TableName: aws.String(d.volumeTableName),
		AttributeDefinitions: []*dynamodb.AttributeDefinition{
			{
				AttributeName: aws.String(db.ServiceUUID),
				AttributeType: aws.String(dynamodb.ScalarAttributeTypeS),
			},
			{
				AttributeName: aws.String(db.VolumeID),
				AttributeType: aws.String(dynamodb.ScalarAttributeTypeS),
			},
		},
		KeySchema: []*dynamodb.KeySchemaElement{
			{
				AttributeName: aws.String(db.ServiceUUID),
				KeyType:       aws.String(dynamodb.KeyTypeHash),
			},
			{
				AttributeName: aws.String(db.VolumeID),
				KeyType:       aws.String(dynamodb.KeyTypeRange),
			},
		},
		ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
			ReadCapacityUnits:  aws.Int64(defaultReadCapacity),
			WriteCapacityUnits: aws.Int64(defaultWriteCapacity),
		},
	}
	resp, err := dbsvc.CreateTable(params)

	if err != nil && err.(awserr.Error).Code() != TableInUseException {
		glog.Errorln("failed to create table", d.volumeTableName, "error", err)
		return d.convertError(err)
	}

	glog.Infoln("volume table", d.volumeTableName, "created or exists, resp", resp)
	return nil
}

func (d *DynamoDB) createConfigTable(dbsvc *dynamodb.DynamoDB) error {
	params := &dynamodb.CreateTableInput{
		TableName: aws.String(d.configTableName),
		AttributeDefinitions: []*dynamodb.AttributeDefinition{
			{
				AttributeName: aws.String(db.ServiceUUID),
				AttributeType: aws.String(dynamodb.ScalarAttributeTypeS),
			},
			{
				AttributeName: aws.String(db.ConfigFileID),
				AttributeType: aws.String(dynamodb.ScalarAttributeTypeS),
			},
		},
		KeySchema: []*dynamodb.KeySchemaElement{
			{
				AttributeName: aws.String(db.ServiceUUID),
				KeyType:       aws.String(dynamodb.KeyTypeHash),
			},
			{
				AttributeName: aws.String(db.ConfigFileID),
				KeyType:       aws.String(dynamodb.KeyTypeRange),
			},
		},
		ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
			ReadCapacityUnits:  aws.Int64(defaultReadCapacity),
			WriteCapacityUnits: aws.Int64(defaultWriteCapacity),
		},
	}
	resp, err := dbsvc.CreateTable(params)

	if err != nil && err.(awserr.Error).Code() != TableInUseException {
		glog.Errorln("failed to create table", d.configTableName, "error", err)
		return d.convertError(err)
	}

	glog.Infoln("config table", d.configTableName, "created or exists, resp", resp)
	return nil
}

// SystemTablesReady checks if all system tables are ready to use
func (d *DynamoDB) SystemTablesReady(ctx context.Context) (tableStatus string, ready bool, err error) {
	dbsvc := dynamodb.New(d.sess)

	TableActive := "ACTIVE"

	// check device table status
	tableStatus, err = d.getTableStatus(dbsvc, d.deviceTableName)
	if err != nil {
		glog.Errorln("get device table status failed", err)
		return tableStatus, false, err
	}
	if tableStatus != TableActive {
		glog.Infoln("device table not ready, status", tableStatus)
		return tableStatus, false, nil
	}

	// check service table status
	tableStatus, err = d.getTableStatus(dbsvc, d.serviceTableName)
	if err != nil {
		glog.Errorln("get service table status failed", err)
		return tableStatus, false, err
	}
	if tableStatus != TableActive {
		glog.Infoln("service table not ready, status", tableStatus)
		return tableStatus, false, nil
	}

	// check service attr table status
	tableStatus, err = d.getTableStatus(dbsvc, d.serviceAttrTableName)
	if err != nil {
		glog.Errorln("get service attr table status failed", err)
		return tableStatus, false, err
	}
	if tableStatus != TableActive {
		glog.Infoln("service attr table not ready, status", tableStatus)
		return tableStatus, false, nil
	}

	// check volume table status
	tableStatus, err = d.getTableStatus(dbsvc, d.volumeTableName)
	if err != nil {
		glog.Errorln("get volume table status failed", err)
		return tableStatus, false, err
	}
	if tableStatus != TableActive {
		glog.Infoln("service table not ready, status", tableStatus)
		return tableStatus, false, nil
	}

	// check config table status
	tableStatus, err = d.getTableStatus(dbsvc, d.configTableName)
	if err != nil {
		glog.Errorln("get config table status failed", err)
		return tableStatus, false, err
	}
	if tableStatus != TableActive {
		glog.Infoln("service table not ready, status", tableStatus)
		return tableStatus, false, nil
	}

	return tableStatus, true, nil
}

func (d *DynamoDB) getTableStatus(dbsvc *dynamodb.DynamoDB, tableName string) (tableStatus string, err error) {
	params := &dynamodb.DescribeTableInput{
		TableName: aws.String(tableName),
	}
	resp, err := dbsvc.DescribeTable(params)

	if err != nil {
		glog.Errorln("failed to DescribeTable", tableName, "error", err)
		return "", d.convertError(err)
	}

	tableStatus = *(resp.Table.TableStatus)
	glog.Infoln("table", tableName, "status", tableStatus)

	return tableStatus, nil
}

// WaitSystemTablesReady waits till the system tables are ready
func (d *DynamoDB) WaitSystemTablesReady(ctx context.Context, maxWaitSeconds int64) error {
	// wait till table becomes active
	waitSleepInterval := int64(2)
	for wait := int64(0); wait < maxWaitSeconds; wait += waitSleepInterval {
		tableStatus, ready, err := d.SystemTablesReady(ctx)
		if err != nil {
			glog.Errorln("SystemTablesReady check failed", err)
			return err
		}

		if ready {
			glog.Infoln("service/device/volume/config tables are ready")
			return nil
		}

		if tableStatus == "CREATING" {
			glog.Infoln("table is under creation, sleep and check again")
			time.Sleep(time.Duration(waitSleepInterval) * time.Second)
		} else {
			glog.Errorln("unexpected table status", tableStatus)
			return db.ErrDBInternal
		}
	}

	glog.Errorln("service/device/volume/config tables are not ready yet")
	return db.ErrDBInternal
}

// DeleteSystemTables deletes volume/service/device tables.
func (d *DynamoDB) DeleteSystemTables(ctx context.Context) error {
	dbsvc := dynamodb.New(d.sess)

	var ferr error
	err := d.deleteVolumeTable(dbsvc)
	if err != nil && err != db.ErrDBTableNotFound {
		glog.Errorln("delete volume table failed", err)
		ferr = err
	}

	err = d.deleteConfigTable(dbsvc)
	if err != nil && err != db.ErrDBTableNotFound {
		glog.Errorln("delete config table failed", err)
		ferr = err
	}

	err = d.deleteServiceAttrTable(dbsvc)
	if err != nil && err != db.ErrDBTableNotFound {
		glog.Errorln("delete service table failed", err)
		ferr = err
	}

	err = d.deleteServiceTable(dbsvc)
	if err != nil && err != db.ErrDBTableNotFound {
		glog.Errorln("delete service table failed", err)
		ferr = err
	}

	err = d.deleteDeviceTable(dbsvc)
	if err != nil && err != db.ErrDBTableNotFound {
		glog.Errorln("delete device table failed", err)
		ferr = err
	}

	return ferr
}

func (d *DynamoDB) deleteServiceAttrTable(dbsvc *dynamodb.DynamoDB) error {
	// TODO reject if any service is still in DB.
	// should we reject if some volume still exists? probably not,
	// aws ecs allows the whole cluster to be destroyed with volumes left.
	// Volume stores customer data. Should be manually deleted by customer.
	return d.deleteTable(dbsvc, d.serviceAttrTableName)
}

func (d *DynamoDB) deleteServiceTable(dbsvc *dynamodb.DynamoDB) error {
	return d.deleteTable(dbsvc, d.serviceTableName)
}

func (d *DynamoDB) deleteDeviceTable(dbsvc *dynamodb.DynamoDB) error {
	// TODO reject if any device is still in DB.
	return d.deleteTable(dbsvc, d.deviceTableName)
}

func (d *DynamoDB) deleteVolumeTable(dbsvc *dynamodb.DynamoDB) error {
	// TODO reject if any volume is still in DB.
	return d.deleteTable(dbsvc, d.volumeTableName)
}

func (d *DynamoDB) deleteConfigTable(dbsvc *dynamodb.DynamoDB) error {
	// TODO reject if any config is still in DB.
	return d.deleteTable(dbsvc, d.configTableName)
}

func (d *DynamoDB) deleteTable(dbsvc *dynamodb.DynamoDB, tableName string) error {
	params := &dynamodb.DeleteTableInput{
		TableName: aws.String(tableName),
	}
	resp, err := dbsvc.DeleteTable(params)

	if err != nil {
		glog.Errorln("failed to delete table", tableName, "error", err)
		return d.convertError(err)
	}

	glog.Infoln("deleted table", tableName, "resp", resp)
	return nil
}
