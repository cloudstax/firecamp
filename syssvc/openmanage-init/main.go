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
	"github.com/cloudstax/openmanage/containersvc"
	"github.com/cloudstax/openmanage/containersvc/awsecs"
	"github.com/cloudstax/openmanage/db"
	"github.com/cloudstax/openmanage/db/awsdynamodb"
	"github.com/cloudstax/openmanage/server/awsec2"
	"github.com/cloudstax/openmanage/utils"
)

var (
	cluster = flag.String("cluster", "default", "The ECS cluster")
	region  = flag.String("region", "", "The AWS region")
	az      = flag.String("availability-zone", "", "The AWS availability zone")
	dbtype  = flag.String("dbtype", common.DBTypeCloudDB, "The db type, such as the AWS DynamoDB or the embedded controldb")
)

func main() {
	flag.Parse()

	var err error

	// check region and az
	awsRegion := *region
	awsAz := *az
	if awsRegion == "" || awsAz == "" {
		awsRegion, err = awsec2.GetLocalEc2Region()
		if err != nil {
			fmt.Println("please input the correct AWS region")
			os.Exit(-1)
		}
		awsAz, err = awsec2.GetLocalEc2AZ()
		if err != nil {
			fmt.Println("please input the correct AWS availability zone")
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

	// create the db service
	createDBService(ctx, *cluster, *dbtype, sess, ecsIns, awsAz)

	// create the manage service
	createManageService(ctx, ecsIns, *cluster, *dbtype)

	// wait till the db and manage services ready
	waitServiceReady(ctx, sess, ecsIns, *dbtype, *cluster)
}

func createDBService(ctx context.Context, cluster string, dbtype string,
	sess *session.Session, ecsIns *awsecs.AWSEcs, awsAz string) {
	switch dbtype {
	case common.DBTypeControlDB:
		createControlDB(ctx, cluster, sess, ecsIns, awsAz)
	case common.DBTypeCloudDB:
		awsdynamo := awsdynamodb.NewDynamoDB(sess)
		tableStatus, ready, err := awsdynamo.SystemTablesReady(ctx)
		if err != nil && err != db.ErrDBResourceNotFound {
			glog.Fatalln("check dynamodb SystemTablesReady error", err, tableStatus)
		}
		if !ready {
			// create system tables
			err = awsdynamo.CreateSystemTables(ctx)
			if err != nil {
				glog.Fatalln("dynamodb CreateSystemTables error", err)
			}
		}
	default:
		glog.Fatalln("unknown db type", dbtype)
	}
}

func createControlDB(ctx context.Context, cluster string, sess *session.Session, ecsIns *awsecs.AWSEcs, awsAz string) {
	// check if the controldb service exists.
	exist, err := ecsIns.IsServiceExist(ctx, cluster, common.ControlDBServiceName)
	if err != nil {
		glog.Fatalln("check the controlDB service exist error", err, common.ControlDBServiceName)
	}
	if exist {
		fmt.Println("The controlDB service is already created")
		return
	}

	// create the controldb volume
	// TODO if some step fails, the volume will not be created. add tag to EBS volume, to reuse it.
	ec2Ins := awsec2.NewAWSEc2(sess)
	volID, err := ec2Ins.CreateVolume(ctx, awsAz, common.ControlDBVolumeSizeGB)
	if err != nil {
		glog.Fatalln("failed to create the controldb volume", err)
	}

	fmt.Println("create the controldb volume", volID)

	err = ec2Ins.WaitVolumeCreated(ctx, volID)
	if err != nil {
		ec2Ins.DeleteVolume(ctx, volID)
		glog.Fatalln("volume is not at available status", err, volID)
	}

	// create the controldb service
	serviceUUID := utils.GenControlDBServiceUUID(volID)

	commonOpts := &containersvc.CommonOptions{
		Cluster:        cluster,
		ServiceName:    common.ControlDBServiceName,
		ServiceUUID:    serviceUUID,
		ContainerImage: common.ControlDBContainerImage,
		Resource: &common.Resources{
			MaxCPUUnits:     int64(0),
			ReserveCPUUnits: common.ControlDBCPUUnits,
			MaxMemMB:        common.ControlDBReserveMemMB, // FIXME: set ReserveMemMB for demo only
			ReserveMemMB:    common.ControlDBReserveMemMB,
		},
	}

	kv := &common.EnvKeyValuePair{
		Name:  common.ENV_CONTAINER_PLATFORM,
		Value: common.ContainerPlatformECS,
	}

	createOpts := &containersvc.CreateServiceOptions{
		Common:        commonOpts,
		ContainerPath: common.ControlDBDefaultDir,
		Port:          common.ControlDBServerPort,
		Replicas:      int64(1),
		Envkvs:        []*common.EnvKeyValuePair{kv},
	}

	err = ecsIns.CreateService(ctx, createOpts)
	if err != nil {
		glog.Fatalln("create controlDB service error", err, common.ControlDBServiceName, "serviceUUID", serviceUUID)
	}

	fmt.Println("created the controlDB service")
}

func createManageService(ctx context.Context, ecsIns *awsecs.AWSEcs, cluster string, dbtype string) {
	exist, err := ecsIns.IsServiceExist(ctx, cluster, common.ManageServiceName)
	if err != nil {
		glog.Fatalln("check the manage http service exist error", err, common.ManageServiceName)
	}
	if exist {
		fmt.Println("The manage service is already created")
		return
	}

	// create the env variables for the manage service container
	kv1 := &common.EnvKeyValuePair{
		Name:  common.ENV_CONTAINER_PLATFORM,
		Value: common.ContainerPlatformECS,
	}
	kv2 := &common.EnvKeyValuePair{
		Name:  common.ENV_DB_TYPE,
		Value: dbtype,
	}
	envkvs := []*common.EnvKeyValuePair{kv1, kv2}

	// create the management http server service
	commonOpts := &containersvc.CommonOptions{
		Cluster:        cluster,
		ServiceName:    common.ManageServiceName,
		ServiceUUID:    "",
		ContainerImage: common.ManageContainerImage,
		Resource: &common.Resources{
			MaxCPUUnits:     int64(0),
			ReserveCPUUnits: common.ManageCPUUnits,
			MaxMemMB:        common.ManageReserveMemMB, // FIXME: set ReserveMemMB for demo only
			ReserveMemMB:    common.ManageReserveMemMB,
		},
	}

	createOpts := &containersvc.CreateServiceOptions{
		Common:        commonOpts,
		ContainerPath: "",
		Port:          common.ManageHTTPServerPort,
		Replicas:      int64(1),
		Envkvs:        envkvs,
	}

	err = ecsIns.CreateService(ctx, createOpts)
	if err != nil {
		glog.Fatalln("create the manage http service error", err, common.ManageServiceName)
	}

	fmt.Println("created the manage http service")
}

func waitServiceReady(ctx context.Context, sess *session.Session, ecsIns *awsecs.AWSEcs, dbtype string, cluster string) {
	maxWaitSeconds := int64(120)

	// wait the manage http container running
	err := ecsIns.WaitServiceRunning(ctx, cluster, common.ManageServiceName, 1, maxWaitSeconds)
	if err != nil {
		glog.Fatalln("wait the manage http container running error", err, common.ManageServiceName)
	}

	fmt.Println("The management http service is ready")

	// wait the db service ready
	switch dbtype {
	case common.DBTypeControlDB:
		// wait the controldb container running
		err := ecsIns.WaitServiceRunning(ctx, cluster, common.ControlDBServiceName, 1, maxWaitSeconds)
		if err != nil {
			glog.Fatalln("wait the controldb container running error", err, common.ControlDBServiceName)
		}

		fmt.Println("The controldb service is ready")

	case common.DBTypeCloudDB:
		// wait the DynamoDB tables ready
		awsdynamo := awsdynamodb.NewDynamoDB(sess)
		err := awsdynamo.WaitSystemTablesReady(ctx, maxWaitSeconds)
		if err != nil {
			glog.Fatalln("the DynamoDB system tables are not ready after", maxWaitSeconds, "seconds, please check it later")
		}

		fmt.Println("The DynamoDB is ready")

	default:
		glog.Fatalln("unknown db type", dbtype)
	}
}
