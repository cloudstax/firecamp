package awsec2

import (
	"flag"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/openconnectio/openmanage/common"
	"github.com/openconnectio/openmanage/server"
)

var e *AWSEc2
var az1bIns1 string
var az1bIns2 string
var az1cIns1 string
var az1bVol1 string

func createInstancesAndVolume(ctx context.Context) error {
	cfg := aws.NewConfig().WithRegion("us-west-1")
	sess, err := session.NewSession(cfg)
	if err != nil {
		glog.Errorln("failed to create session, error", err)
		return err
	}

	e = NewAWSEc2(sess)
	if e == nil {
		return common.ErrInternal
	}

	// launch 3 instances at 2 az and create one volume
	az1bIns1, err = e.LaunchOneInstance(ctx, "us-west-1b")
	if err != nil {
		glog.Errorln("failed to launch instance at us-west-1b, error", err)
		return err
	}

	az1bIns2, err = e.LaunchOneInstance(ctx, "us-west-1b")
	if err != nil {
		glog.Errorln("failed to launch instance at us-west-1b, error", err)
		return err
	}

	az1cIns1, err = e.LaunchOneInstance(ctx, "us-west-1c")
	if err != nil {
		glog.Errorln("failed to launch instance at us-west-1c, error", err)
		return err
	}

	// create one volume
	az1bVol1, err = e.CreateVolume(ctx, "us-west-1b", 1)
	if err != nil {
		glog.Errorln("failed to create volume at us-west-1b, error", err)
		return err
	}

	err = e.WaitVolumeCreated(ctx, az1bVol1)
	if err != nil {
		glog.Errorln("WaitVolumeCreated error", err)
		return err
	}

	err = e.WaitInstanceRunning(ctx, az1bIns1)
	if err != nil {
		glog.Errorln("WaitInstanceRunning error", err)
		return err
	}
	err = e.WaitInstanceRunning(ctx, az1bIns2)
	if err != nil {
		glog.Errorln("WaitInstanceRunning error", err)
		return err
	}
	err = e.WaitInstanceRunning(ctx, az1cIns1)
	if err != nil {
		glog.Errorln("WaitInstanceRunning error", err)
		return err
	}

	return nil
}

func cleanup(ctx context.Context) {
	// terminate the created instances and volume
	if len(az1bIns1) != 0 {
		e.TerminateInstance(ctx, az1bIns1)
	}
	if len(az1bIns2) != 0 {
		e.TerminateInstance(ctx, az1bIns2)
	}
	if len(az1cIns1) != 0 {
		e.TerminateInstance(ctx, az1cIns1)
	}
	if len(az1bVol1) != 0 {
		e.DeleteVolume(ctx, az1bVol1)
	}
}

func TestMain(m *testing.M) {
	flag.Parse()

	ctx := context.Background()

	err := createInstancesAndVolume(ctx)
	if err != nil {
		cleanup(ctx)
		return
	}

	m.Run()

	cleanup(ctx)
}

func TestVolume(t *testing.T) {
	var err error
	devName := "/dev/xvdf"

	ctx := context.Background()

	// negative case - detach available volume
	err = e.DetachVolume(ctx, az1bVol1, az1bIns1, devName)
	if err == nil && err != server.ErrVolumeIncorrectState {
		t.Fatalf("detach available volume, expected ErrVolumeIncorrectState but got %s, vol %s instance %s", err, az1bVol1, az1bIns1)
	}

	// negative case - attach volume to instance at differnt az
	err = e.AttachVolume(ctx, az1bVol1, az1cIns1, devName)
	if err == nil {
		t.Fatalf("attach volume to instance at different az, expected fail but success, vol %s instance %s", az1bVol1, az1cIns1)
	}

	// attach volume
	err = e.AttachVolume(ctx, az1bVol1, az1bIns1, devName)
	if err != nil {
		t.Fatalf("attach volume, expected success but fail, vol %s instance %s", az1bVol1, az1bIns1)
	}
	err = e.WaitVolumeAttached(ctx, az1bVol1)
	if err != nil {
		t.Fatalf("wait volume in-use, error %s", err)
	}

	// negative case - detach volume from wrong instanceID
	err = e.DetachVolume(ctx, az1bVol1, az1bIns2, devName)
	if err == nil {
		t.Fatalf("detach volume from wrong instance, expected fail but success, vol %s instance %s", az1bVol1, az1bIns2)
	}

	// negative case - detach volume with wrong devName
	err = e.DetachVolume(ctx, az1bVol1, az1bIns1, "/dev/xvdx")
	if err == nil {
		t.Fatalf("detach volume with wrong devName, expected fail but success, vol %s instance %s devName %s", az1bVol1, az1bIns1, devName)
	}

	// detach volume
	err = e.DetachVolume(ctx, az1bVol1, az1bIns1, devName)
	if err != nil {
		t.Fatalf("detach volume, expected success but failed, vol %s instance %s", az1bVol1, az1bIns1)
	}

	err = e.WaitVolumeDetached(ctx, az1bVol1)
	if err != nil && err != common.ErrTimeout {
		glog.Errorln("wait volume available, error %s", err)
	}
}

func TestDeviceName(t *testing.T) {
	dev, err := e.GetNextDeviceName("/dev/xvdf")
	if err != nil || dev != "/dev/xvdg" {
		t.Fatalf("/dev/xvdf expect /dev/xvdg, get %s, error %s", dev, err)
	}

	dev, err = e.GetNextDeviceName("/dev/xvdz")
	if err != nil || dev != "/dev/xvdba" {
		t.Fatalf("/dev/xvdz expect /dev/xvdba, get %s, error %s", dev, err)
	}

	dev, err = e.GetNextDeviceName("/dev/xvdba")
	if err != nil || dev != "/dev/xvdbb" {
		t.Fatalf("/dev/xvdba expect /dev/xvdbb, get %s, error %s", dev, err)
	}

	dev, err = e.GetNextDeviceName("/dev/xvdbh")
	if err != nil || dev != "/dev/xvdbi" {
		t.Fatalf("/dev/xvdbh expect /dev/xvdbi, get %s, error %s", dev, err)
	}

	dev, err = e.GetNextDeviceName("/dev/xvdbz")
	if err != nil || dev != "/dev/xvdca" {
		t.Fatalf("/dev/xvdbz expect /dev/xvdca, get %s, error %s", dev, err)
	}

	dev, err = e.GetNextDeviceName("/dev/xvdcy")
	if err != nil || dev != "/dev/xvdcz" {
		t.Fatalf("/dev/xvdcy expect /dev/xvdcz, get %s, error %s", dev, err)
	}

	// error cases
	dev, err = e.GetNextDeviceName("/dev/xvdcz")
	if err == nil {
		t.Fatalf("/dev/xvdcz expect error, but success", dev)
	}

	dev, err = e.GetNextDeviceName("/dev/xvc")
	if err == nil {
		t.Fatalf("/dev/xvc expect error, but success", dev)
	}

	dev, err = e.GetNextDeviceName("/dev/xvd")
	if err == nil {
		t.Fatalf("/dev/xvd expect error, but success", dev)
	}
}
