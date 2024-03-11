package awsdynamodb

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/jazzl0ver/firecamp/pkg/db"
)

// DynamoDB implements DB interfae on dynamodb
type DynamoDB struct {
	// see https://docs.aws.amazon.com/sdk-for-go/api/aws/session/
	// session could and should be shared when possible.
	// Creating service clients concurrently from a shared Session is safe.
	sess      *session.Session
	tableName string
	// The DynamoDB table read/write capacity
	readCapacity  int64
	writeCapacity int64
}

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
func NewDynamoDB(sess *session.Session, cluster string) *DynamoDB {
	d := new(DynamoDB)
	d.sess = sess
	d.tableName = cluster + common.NameSeparator + tableNameSuffix
	d.readCapacity = defaultReadCapacity
	d.writeCapacity = defaultWriteCapacity
	return d
}

// NewTestDynamoDB creates a DynamoDB instance for test
func NewTestDynamoDB(sess *session.Session, suffix string) *DynamoDB {
	d := new(DynamoDB)
	d.sess = sess
	d.tableName = "TestTable" + suffix
	d.readCapacity = 5
	d.writeCapacity = 5
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

// CreateSystemTables creates the system tables.
func (d *DynamoDB) CreateSystemTables(ctx context.Context) error {
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.CreateTableInput{
		TableName: aws.String(d.tableName),
		AttributeDefinitions: []*dynamodb.AttributeDefinition{
			{
				AttributeName: aws.String(tablePartitionKey),
				AttributeType: aws.String(dynamodb.ScalarAttributeTypeS),
			},
			{
				AttributeName: aws.String(tableSortKey),
				AttributeType: aws.String(dynamodb.ScalarAttributeTypeS),
			},
		},
		KeySchema: []*dynamodb.KeySchemaElement{
			{
				AttributeName: aws.String(tablePartitionKey),
				KeyType:       aws.String(dynamodb.KeyTypeHash),
			},
			{
				AttributeName: aws.String(tableSortKey),
				KeyType:       aws.String(dynamodb.KeyTypeRange),
			},
		},
		ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
			ReadCapacityUnits:  aws.Int64(d.readCapacity),
			WriteCapacityUnits: aws.Int64(d.writeCapacity),
		},
	}
	resp, err := dbsvc.CreateTable(params)

	if err != nil && err.(awserr.Error).Code() != TableInUseException {
		glog.Errorln("failed to create table", d.tableName, "error", err)
		return d.convertError(err)
	}

	glog.Infoln("device table", d.tableName, "created or exists, resp", resp)
	return nil
}

// SystemTablesReady checks if all system tables are ready to use
func (d *DynamoDB) SystemTablesReady(ctx context.Context) (tableStatus string, ready bool, err error) {
	dbsvc := dynamodb.New(d.sess)

	// check table status
	tableStatus, err = d.getTableStatus(dbsvc, d.tableName)
	if err != nil {
		glog.Errorln("get table status failed", err)
		return tableStatus, false, err
	}
	if tableStatus != db.TableStatusActive {
		glog.Infoln("device table not ready, status", tableStatus)
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
			glog.Infoln("table is ready", d.tableName)
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

	glog.Errorln("table not ready yet", d.tableName)
	return db.ErrDBInternal
}

// DeleteSystemTables deletes the system tables.
func (d *DynamoDB) DeleteSystemTables(ctx context.Context) error {
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.DeleteTableInput{
		TableName: aws.String(d.tableName),
	}
	resp, err := dbsvc.DeleteTable(params)

	if err != nil {
		glog.Errorln("failed to delete table", d.tableName, "error", err)
		return d.convertError(err)
	}

	glog.Infoln("deleted table", d.tableName, "resp", resp)
	return nil
}

// WaitSystemTablesDeleted waits till all system tables are deleted.
func (d *DynamoDB) WaitSystemTablesDeleted(ctx context.Context, maxWaitSeconds int64) error {
	dbsvc := dynamodb.New(d.sess)

	waitSleepInterval := int64(2)
	sleepSecs := time.Duration(waitSleepInterval) * time.Second
	for wait := int64(0); wait < maxWaitSeconds; wait += waitSleepInterval {
		time.Sleep(sleepSecs)

		tableStatus, err := d.getTableStatus(dbsvc, d.tableName)
		if err != db.ErrDBResourceNotFound && tableStatus != db.TableStatusDeleting {
			glog.Errorln("get table status error", err, d.tableName, tableStatus)
			return db.ErrDBInternal
		}
		if tableStatus == db.TableStatusDeleting {
			continue
		}

		glog.Infoln("The table is deleted", d.tableName)
		return nil
	}

	return common.ErrTimeout
}
