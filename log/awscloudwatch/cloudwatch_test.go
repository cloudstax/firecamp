package awscloudwatch

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/utils"
)

func TestCloudWatchLog(t *testing.T) {
	region := "us-east-1"
	config := aws.NewConfig().WithRegion(region)
	sess, err := session.NewSession(config)
	if err != nil {
		t.Fatalf("NewSession error %s", err)
	}

	ins := NewLog(sess, region, common.ContainerPlatformECS)

	ctx := context.Background()
	uuid := utils.GenUUID()
	cluster := "cluster-" + uuid
	service := "service-" + uuid
	serviceUUID := "serviceuuid-" + uuid
	stream := "stream-" + uuid

	cfg := ins.CreateServiceLogConfig(ctx, cluster, service, serviceUUID)
	if cfg.Name != driverName {
		t.Fatalf("expect log driver name %s, got %s", driverName, cfg.Name)
	}

	cfg = ins.CreateLogConfigForStream(ctx, cluster, service, serviceUUID, stream)
	if cfg.Name != driverName {
		t.Fatalf("expect log driver name %s, got %s", driverName, cfg.Name)
	}
	if cfg.Options[logStreamPrefix] != stream {
		t.Fatalf("expect log stream %s, got %s", stream, cfg.Options)
	}

	err = ins.InitializeServiceLogConfig(ctx, cluster, service, serviceUUID)
	if err != nil {
		t.Fatalf("InitializeServiceLogConfig error %s, cluster %s", err, cluster)
	}

	// initialize again to test creating the same log group
	err = ins.InitializeServiceLogConfig(ctx, cluster, service, serviceUUID)
	if err != nil {
		t.Fatalf("InitializeServiceLogConfig again error %s, cluster %s", err, cluster)
	}

	err = ins.DeleteServiceLogConfig(ctx, cluster, service, serviceUUID)
	if err != nil {
		t.Fatalf("DeleteServiceLogConfig error %s, cluster %s", err, cluster)
	}

	// delete again to test deleting unexist log group
	err = ins.DeleteServiceLogConfig(ctx, cluster, service, serviceUUID)
	if err != nil {
		t.Fatalf("DeleteServiceLogConfig again error %s, cluster %s", err, cluster)
	}
}
