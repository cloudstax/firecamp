package main

import (
	"flag"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/golang/glog"

	"github.com/cloudzzzz/sc/aws/ec2"
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

	e := awsec2.NewAWSEc2(sess)

	az1bIns1, err := e.LaunchOneInstance("us-west-1b")
	if err != nil {
		glog.Errorln("LaunchOneInstance error", err)
		return
	}
	defer e.TerminateInstance(az1bIns1)

	for true {
		state1, err := e.GetInstanceState(az1bIns1)
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

	az1bVol1, err := e.CreateVolume("us-west-1b", 1)
	if err != nil {
		glog.Errorln("CreateVolume error", err)
		return
	}
	e.WaitVolumeCreated(az1bVol1)

	e.AttachVolume(az1bVol1, az1bIns1, devName)
	e.WaitVolumeAttached(az1bVol1)

	e.DetachVolume(az1bVol1, az1bIns1, devName)
	err = e.DetachVolume(az1bVol1, az1bIns1, devName)
	glog.Infoln("DetachVolume the detaching volume again, error", err)
	e.WaitVolumeDetached(az1bVol1)

	e.DeleteVolume(az1bVol1)
	e.GetVolumeState(az1bVol1)
	time.Sleep(10 * time.Second)
	e.GetVolumeState(az1bVol1)

	e.GetVolumeState("vol-3dacf381")

	// detach available volume
	e.DetachVolume("vol-3dacf381", "i-134a54a6", devName)
}
