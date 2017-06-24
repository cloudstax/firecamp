package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/containersvc/awsecs"
	"github.com/cloudstax/openmanage/db/awsdynamodb"
	"github.com/cloudstax/openmanage/server/awsec2"
)

var (
	cluster = flag.String("cluster", "default", "The ECS cluster")
	region  = flag.String("region", "", "The AWS region")
	dbtype  = flag.String("dbtype", common.DBTypeCloudDB, "The db type, such as the AWS DynamoDB or the embedded controldb")
)

func main() {
	flag.Parse()

	var err error

	// check region and az
	awsRegion := *region
	if awsRegion == "" {
		awsRegion, err = awsec2.GetLocalEc2Region()
		if err != nil {
			fmt.Println("please input the correct AWS region")
			os.Exit(-1)
		}
	}

	cfg := aws.NewConfig().WithRegion(awsRegion)
	sess, err := session.NewSession(cfg)
	if err != nil {
		glog.Fatalln("failed to create session, error", err)
	}

	ecsIns := awsecs.NewAWSEcs(sess)

	// create context
	ctx := context.Background()

	// cluster should exist
	exist, err := ecsIns.IsClusterExist(ctx, *cluster)
	if err != nil {
		glog.Fatalln("IsClusterExist error", err, *cluster)
	}
	if !exist {
		glog.Fatalln("cluster not exist", *cluster)
	}

	// cleanup the db service
	cleanupDBService(ctx, *cluster, *dbtype, sess, ecsIns)

	// cleanup the manage service
	cleanupService(ctx, *cluster, common.ManageServiceName, ecsIns)
}

func cleanupDBService(ctx context.Context, cluster string, dbtype string,
	sess *session.Session, ecsIns *awsecs.AWSEcs) {
	switch dbtype {
	case common.DBTypeControlDB:
		// TODO delete controldb volume
		cleanupService(ctx, cluster, common.ControlDBServiceName, ecsIns)

	case common.DBTypeCloudDB:
		awsdynamo := awsdynamodb.NewDynamoDB(sess, cluster)
		// delete system tables
		err := awsdynamo.DeleteSystemTables(ctx)
		if err != nil {
			glog.Fatalln("dynamodb CreateSystemTables error", err)
		}
	default:
		glog.Fatalln("unknown db type", dbtype)
	}
}

func cleanupService(ctx context.Context, cluster string, service string, ecsIns *awsecs.AWSEcs) {
	// check if the service exists.
	exist, err := ecsIns.IsServiceExist(ctx, cluster, service)
	if err != nil {
		glog.Fatalln("check the service exist error", err, service, "cluster", cluster)
	}
	if !exist {
		fmt.Println("The service is already deleted")
		return
	}

	err = ecsIns.StopService(ctx, cluster, service)
	if err != nil {
		glog.Fatalln("StopService error", err, "service", service, "cluster", cluster)
	}

	err = ecsIns.DeleteService(ctx, cluster, service)
	if err != nil {
		glog.Fatalln("delete service from container platform error", err, "service", service, "cluster", cluster)
	}
}
