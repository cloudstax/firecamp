package main

import (
	"flag"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/server"
	"github.com/cloudstax/firecamp/server/awsec2"
	"github.com/cloudstax/firecamp/utils"
)

func main() {
	flag.Parse()

	devName := "/dev/xvdf"

	cfg := aws.NewConfig().WithRegion("us-west-1")
	sess, err := session.NewSession(cfg)
	if err != nil {
		glog.Errorln("failed to create session, error", err)
		os.Exit(-1)
	}

	ctx := context.Background()

	e := awsec2.NewAWSEc2(sess)

	state, err := e.GetVolumeState(ctx, "vol-aaaaaa")
	if err != nil {
		glog.Errorln("get non-exist volume error", err, state)
	}

	imageID := "ami-327f5352"
	az := "us-west-1a"
	cluster := "test" + utils.GenUUID()

	az1bIns1, err := e.LaunchOneInstance(ctx, imageID, az, cluster)
	if err != nil {
		glog.Errorln("LaunchOneInstance error", err)
		return
	}
	defer e.TerminateInstance(ctx, az1bIns1)

	for true {
		state1, err := e.GetInstanceState(ctx, az1bIns1)
		if err != nil {
			glog.Errorln("failed to get instance", az1bIns1, "state, error", err)
			return
		}

		glog.Infoln(az1bIns1, state1)

		if state1 == "running" {
			break
		}

		time.Sleep(2 * time.Second)
	}

	opts := &server.CreateVolumeOptions{
		AvailabilityZone: az,
		VolumeType:       common.VolumeTypeGPSSD,
		VolumeSizeGB:     1,
		TagSpecs: []common.KeyValuePair{
			common.KeyValuePair{
				Key:   "Name",
				Value: cluster,
			},
		},
	}
	az1bVol1, err := e.CreateVolume(ctx, opts)
	if err != nil {
		glog.Errorln("CreateVolume error", err)
		return
	}
	e.WaitVolumeCreated(ctx, az1bVol1)

	e.AttachVolume(ctx, az1bVol1, az1bIns1, devName)
	e.WaitVolumeAttached(ctx, az1bVol1)

	e.DetachVolume(ctx, az1bVol1, az1bIns1, devName)
	err = e.DetachVolume(ctx, az1bVol1, az1bIns1, devName)
	glog.Infoln("DetachVolume the detaching volume again, error", err)
	e.WaitVolumeDetached(ctx, az1bVol1)

	e.DeleteVolume(ctx, az1bVol1)
	e.GetVolumeState(ctx, az1bVol1)
	time.Sleep(10 * time.Second)
	e.GetVolumeState(ctx, az1bVol1)

	e.GetVolumeState(ctx, "vol-3dacf381")

	// detach available volume
	e.DetachVolume(ctx, "vol-3dacf381", "i-134a54a6", devName)

	netInterface, err := e.GetInstanceNetworkInterface(ctx, az1bIns1)
	if err != nil {
		glog.Errorln("GetInstanceNetworkInterface error", err, az1bIns1)
		return
	}
	err = e.UnassignStaticIP(ctx, netInterface.InterfaceID, "10.0.0.1")
	glog.Errorln("unassign unexist ip, error", err)
}
