package awsec2

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
)

func TestEC2Utils(t *testing.T) {
	region := "us-west-2"
	cfg := aws.NewConfig().WithRegion(region)
	sess, err := session.NewSession(cfg)
	if err != nil {
		t.Fatalf("failed to create session, error %s", err)
	}

	azs, err := GetRegionAZs(region, sess)
	if err != nil {
		t.Fatalf("getRegionAZs error %s", err)
	}
	if len(azs) != 3 {
		t.Fatalf("expect 3 azs in region %s, get %s", region, azs)
	}
	//fmt.Println(region, azs)
}
