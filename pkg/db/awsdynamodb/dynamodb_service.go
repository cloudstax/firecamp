package awsdynamodb

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/jazzl0ver/firecamp/pkg/db"
	"github.com/jazzl0ver/firecamp/pkg/utils"
)

// CreateService puts a new Service into DB
func (d *DynamoDB) CreateService(ctx context.Context, svc *common.Service) error {
	requuid := utils.GetReqIDFromContext(ctx)

	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.PutItemInput{
		TableName: aws.String(d.tableName),
		Item: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(servicePartitionKeyPrefix + svc.ClusterName),
			},
			tableSortKey: {
				S: aws.String(svc.ServiceName),
			},
			db.ServiceUUID: {
				S: aws.String(svc.ServiceUUID),
			},
		},
		ConditionExpression: aws.String(tableSortKeyPutCondition),
	}
	_, err := dbsvc.PutItem(params)

	if err != nil {
		glog.Errorln("failed to create service", svc, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("created service", svc, "requuid", requuid)
	return nil
}

// GetService gets the Service from DB
func (d *DynamoDB) GetService(ctx context.Context, clusterName string, serviceName string) (svc *common.Service, err error) {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	params := &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(servicePartitionKeyPrefix + clusterName),
			},
			tableSortKey: {
				S: aws.String(serviceName),
			},
		},
		ConsistentRead: aws.Bool(true),
	}
	resp, err := dbsvc.GetItem(params)

	if err != nil {
		glog.Errorln("failed to get service", serviceName,
			"cluster", clusterName, "error", err, "requuid", requuid)
		return nil, d.convertError(err)
	}

	if len(resp.Item) == 0 {
		glog.Infoln("service", serviceName, "not found, cluster", clusterName, "requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	svc = db.CreateService(clusterName, serviceName, *(resp.Item[db.ServiceUUID].S))

	glog.Infoln("get service", svc, "requuid", requuid)
	return svc, nil
}

// DeleteService deletes the service from DB
func (d *DynamoDB) DeleteService(ctx context.Context, clusterName string, serviceName string) error {
	requuid := utils.GetReqIDFromContext(ctx)
	dbsvc := dynamodb.New(d.sess)

	// TODO reject if any serviceMember is still mounted, e.g. task still running.
	// should we reject if some serviceMember still exists? probably not, as aws ecs allows service to be deleted with serviceMembers left.

	params := &dynamodb.DeleteItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]*dynamodb.AttributeValue{
			tablePartitionKey: {
				S: aws.String(servicePartitionKeyPrefix + clusterName),
			},
			tableSortKey: {
				S: aws.String(serviceName),
			},
		},
		ConditionExpression: aws.String(tableSortKeyDelCondition),
	}

	resp, err := dbsvc.DeleteItem(params)

	if err != nil {
		if err.(awserr.Error).Code() == ConditionalCheckFailedException {
			glog.Infoln("service not found", serviceName, "cluster", clusterName, "requuid", requuid, "resp", resp)
			return db.ErrDBRecordNotFound
		}
		glog.Errorln("failed to delete service", serviceName,
			"cluster", clusterName, "error", err, "requuid", requuid)
		return d.convertError(err)
	}

	glog.Infoln("deleted service", serviceName, "cluster", clusterName, "requuid", requuid)
	return nil
}

// ListServices lists all services
func (d *DynamoDB) ListServices(ctx context.Context, clusterName string) (services []*common.Service, err error) {
	return d.listServicesWithLimit(ctx, clusterName, 0)
}

func (d *DynamoDB) listServicesWithLimit(ctx context.Context, clusterName string, limit int64) (services []*common.Service, err error) {
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
					S: aws.String(servicePartitionKeyPrefix + clusterName),
				},
			},
			ConsistentRead: aws.Bool(true),
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
			glog.Errorln("failed to list services, cluster", clusterName,
				"limit", limit, "lastEvaluatedKey", lastEvaluatedKey, "error", err, "requuid", requuid)
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

			glog.Infoln("no more service item, cluster", clusterName, "services", len(services), "requuid", requuid)
			return services, nil
		}

		for _, item := range resp.Items {
			svc := db.CreateService(clusterName, *(item[tableSortKey].S), *(item[db.ServiceUUID].S))
			services = append(services, svc)
		}

		glog.Infoln("list", len(services), "services, cluster", clusterName,
			"LastEvaluatedKey", lastEvaluatedKey, "requuid", requuid)

		if len(lastEvaluatedKey) == 0 {
			// no more services
			break
		}
	}

	return services, nil
}
