package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/golang/glog"

	"github.com/openconnectio/openmanage/db"
)

func main() {
	flag.Parse()

	tableName := "test-table"
	config := aws.NewConfig().WithRegion("us-west-1")
	sess, err := session.NewSession(config)
	if err != nil {
		glog.Errorln("CreateServiceAttr failed to create session, error", err)
		os.Exit(-1)
	}

	dbIns := dynamodb.New(sess)

	err = createTable(dbIns, tableName)
	if err != nil {
		return
	}

	defer deleteTable(dbIns, tableName)
	time.Sleep(10 * time.Second)

	cluster := "cluster1"
	device := "device1"

	getDevice(dbIns, tableName, cluster, device)
	deleteDevice(dbIns, tableName, cluster, device, false)
	deleteDevice(dbIns, tableName, cluster, device, true)
}

func createTable(dbIns *dynamodb.DynamoDB, tableName string) error {
	params := &dynamodb.CreateTableInput{
		TableName: aws.String(tableName),
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
			ReadCapacityUnits:  aws.Int64(1),
			WriteCapacityUnits: aws.Int64(1),
		},
	}
	resp, err := dbIns.CreateTable(params)

	if err != nil && err.(awserr.Error).Code() != "ResourceInUseException" {
		glog.Errorln("failed to create table", tableName, "error", err)
		return err
	}

	glog.Infoln("device table", tableName, "created or exists, resp", resp)
	return nil
}

func deleteTable(svc *dynamodb.DynamoDB, tableName string) error {
	params := &dynamodb.DeleteTableInput{
		TableName: aws.String(tableName),
	}
	resp, err := svc.DeleteTable(params)

	if err != nil {
		glog.Errorln("failed to delete table", tableName, "error", err)
		return err
	}

	glog.Infoln("deleted table", tableName, "resp", resp)
	return nil
}

func getDevice(svc *dynamodb.DynamoDB, tableName string, clusterName string, deviceName string) {
	params := &dynamodb.GetItemInput{
		TableName: aws.String(tableName),
		Key: map[string]*dynamodb.AttributeValue{
			db.ClusterName: {
				S: aws.String(clusterName),
			},
			db.DeviceName: {
				S: aws.String(deviceName),
			},
		},
		ConsistentRead:         aws.Bool(true),
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}
	resp, err := svc.GetItem(params)
	fmt.Println("getDevice, cluster", clusterName, "device", deviceName, "resp", resp, "error", err)

	// access empty map will cause "panic: runtime error: invalid memory address or nil pointer dereference"
	// fmt.Println("access empty map", *(resp.Item[deviceName].S))
}

func deleteDevice(svc *dynamodb.DynamoDB, tableName string, clusterName string, deviceName string, setCond bool) {
	params := &dynamodb.DeleteItemInput{
		TableName: aws.String(tableName),
		Key: map[string]*dynamodb.AttributeValue{
			db.ClusterName: {
				S: aws.String(clusterName),
			},
			db.DeviceName: {
				S: aws.String(deviceName),
			},
		},
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}
	if setCond {
		params.ConditionExpression = aws.String(db.DeviceDelCondition)
	}

	resp, err := svc.DeleteItem(params)
	fmt.Println("deleteDevice, cluster", clusterName, "device", deviceName, "setCond", setCond, "resp", resp, "error", err)
}
