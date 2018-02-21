package awsdynamodb

import (
	"context"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/golang/glog"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/utils"
)

// Swarm DynamoDB helps to coordinate the Swarm cluster initialization.
// One Swarm manager has to initialize the cluster first, and expose the join token
// for other managers and workers. Other managers and workers need to use the
// corresponding token to join the cluster.

// DynamoDB related const
const (
	RoleWorker  = "worker"
	RoleManager = "manager"

	swarmPartitionKeyPrefix = "SwarmKey-"
	// The swarm init manager. The init manager is used for the swarm cluster init only.
	// The init manager will not be changed.
	initManager     = "InitManager"
	initManagerAddr = "InitManagerAddr"
	// The swarm managers. It includes the current managers. If one manager is dead, it will be replaced.
	// The manager addresses are separated by ",".
	managers     = "SwarmManagers"
	managerAddrs = "SwarmManagerAddrs"
	joinToken    = "JoinToken"
)

// TakeInitManager tries to become the first manager that initializes the swarm cluster and persists the join tokens.
func (d *DynamoDB) TakeInitManager(ctx context.Context, clusterName string, addr string) error {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.PutItemInput{
		TableName: aws.String(d.tableName),
		Item: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(swarmPartitionKeyPrefix + clusterName),
			},
			tableSortKey: {
				S: aws.String(initManager),
			},
			initManagerAddr: {
				S: aws.String(addr),
			},
		},
		ConditionExpression:    aws.String(tablePartitionKeyPutCondition),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}

	_, err := dbsvc.PutItem(params)
	if err != nil {
		glog.Errorln("take init manager error", err, "cluster", clusterName, "addr", addr, "requuid", requuid)
		return d.convertError(err)
	}

	// createManagerAddrs failure will be very rare. It is ok to directly return failure.
	// The whole swarm cluster creation will fail. The user just needs to recreate the stack.
	err = d.createManagerAddrs(ctx, clusterName, addr, requuid)
	if err != nil {
		glog.Errorln("create initial manager addrs error", err, "cluster", clusterName, "addr", addr, "requuid", requuid)
		return err
	}

	glog.Infoln("take the init manager, cluster", clusterName, "addr", addr, "requuid", requuid)
	return nil
}

// GetInitManager gets the init manager address from DB.
func (d *DynamoDB) GetInitManager(ctx context.Context, clusterName string) (addr string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(swarmPartitionKeyPrefix + clusterName),
			},
			tableSortKey: {
				S: aws.String(initManager),
			},
		},
		ConsistentRead:         aws.Bool(true),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}
	resp, err := dbsvc.GetItem(params)

	if err != nil {
		glog.Errorln("get swarm init manager error", err, "cluster", clusterName, "requuid", requuid)
		return "", d.convertError(err)
	}

	if len(resp.Item) == 0 {
		glog.Infoln("swarm init manager not found, cluster", clusterName, "requuid", requuid)
		return "", db.ErrDBRecordNotFound
	}

	addr = *(resp.Item[initManagerAddr].S)

	glog.Infoln("get swarm init manager addr", addr, "requuid", requuid)
	return addr, nil
}

// CreateJoinToken puts the worker/manager join token in DB.
func (d *DynamoDB) CreateJoinToken(ctx context.Context, clusterName string, token string, role string) error {
	if role != RoleWorker && role != RoleManager {
		glog.Errorln("invalid swarm role, please specify worker or manager")
		return common.ErrInvalidArgs
	}

	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.PutItemInput{
		TableName: aws.String(d.tableName),
		Item: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(swarmPartitionKeyPrefix + clusterName),
			},
			tableSortKey: {
				S: aws.String(role),
			},
			joinToken: {
				S: aws.String(token),
			},
		},
		ConditionExpression:    aws.String(tablePartitionKeyPutCondition),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}
	_, err := dbsvc.PutItem(params)

	if err != nil {
		glog.Errorln("create swarm token error", err, "role", role, "cluster", clusterName, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("created swarm token, role", role, "requuid", requuid)
	return nil
}

// GetJoinToken gets the worker/manager join token from DB.
func (d *DynamoDB) GetJoinToken(ctx context.Context, clusterName string, role string) (token string, err error) {
	if role != RoleWorker && role != RoleManager {
		glog.Errorln("invalid swarm role, please specify worker or manager")
		return "", common.ErrInvalidArgs
	}

	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(swarmPartitionKeyPrefix + clusterName),
			},
			tableSortKey: {
				S: aws.String(role),
			},
		},
		ConsistentRead:         aws.Bool(true),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}
	resp, err := dbsvc.GetItem(params)

	if err != nil {
		glog.Errorln("get swarm token error", err, "role", role, "cluster", clusterName, "requuid", requuid)
		return "", d.convertError(err)
	}

	if len(resp.Item) == 0 {
		glog.Infoln("swarm token not found, role", role, "cluster", clusterName, "requuid", requuid)
		return "", db.ErrDBRecordNotFound
	}

	token = *(resp.Item[joinToken].S)

	glog.Infoln("get swarm token", role, "requuid", requuid)
	return token, nil
}

// create the initial managerAddrs record in db
func (d *DynamoDB) createManagerAddrs(ctx context.Context, clusterName string, addr string, requuid string) error {
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.PutItemInput{
		TableName: aws.String(d.tableName),
		Item: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(swarmPartitionKeyPrefix + clusterName),
			},
			tableSortKey: {
				S: aws.String(managers),
			},
			managerAddrs: {
				S: aws.String(addr),
			},
		},
		ConditionExpression:    aws.String(tableSortKeyPutCondition),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}

	_, err := dbsvc.PutItem(params)
	if err != nil {
		glog.Errorln("create the initial manager addrs error", err, "cluster", clusterName, "addr", addr, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("create the initial manager addrs, cluster", clusterName, "addr", addr, "requuid", requuid)
	return nil
}

// GetManagerAddrs gets the manager addresses from DB.
func (d *DynamoDB) GetManagerAddrs(ctx context.Context, clusterName string) (addrs string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(swarmPartitionKeyPrefix + clusterName),
			},
			tableSortKey: {
				S: aws.String(managers),
			},
		},
		ConsistentRead:         aws.Bool(true),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}
	resp, err := dbsvc.GetItem(params)

	if err != nil {
		glog.Errorln("get swarm manager addrs error", err, "cluster", clusterName, "requuid", requuid)
		return "", d.convertError(err)
	}

	if len(resp.Item) == 0 {
		glog.Infoln("swarm manager addrs not found, cluster", clusterName, "requuid", requuid)
		return "", db.ErrDBRecordNotFound
	}

	addrs = *(resp.Item[managerAddrs].S)

	glog.Infoln("get swarm manager addrs", addrs, "requuid", requuid)

	return addrs, nil
}

// AddManagerAddrs updates the managerAddrs record in db
func (d *DynamoDB) AddManagerAddrs(ctx context.Context, clusterName string, oldAddrs string, newAddrs string) error {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	updateExpr := "SET " + managerAddrs + " = :v1"
	conditionExpr := managerAddrs + " = :cv1"

	params := &dynamodb.UpdateItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(swarmPartitionKeyPrefix + clusterName),
			},
			tableSortKey: {
				S: aws.String(managers),
			},
		},
		UpdateExpression:    aws.String(updateExpr),
		ConditionExpression: aws.String(conditionExpr),
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":v1": {
				S: aws.String(newAddrs),
			},
			":cv1": {
				S: aws.String(oldAddrs),
			},
		},
	}

	_, err := dbsvc.UpdateItem(params)
	if err != nil {
		glog.Errorln("update manager addrs error", err, "cluster", clusterName,
			"requuid", requuid, "oldAddrs", oldAddrs, "newAddrs", newAddrs)
		return d.convertError(err)
	}

	glog.Infoln("update manager addrs, cluster", clusterName, "requuid", requuid, "oldAddrs", oldAddrs, "newAddrs", newAddrs)
	return nil
}
