package awsec2

import (
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/glog"

	"github.com/openconnectio/openmanage/common"
	"github.com/openconnectio/openmanage/server"
)

// AWSEc2 handles the ec2 related functions
type AWSEc2 struct {
	sess *session.Session
}

const (
	// aws specific error
	errVolumeIncorrectState = "IncorrectState"
	errVolumeInUse          = "VolumeInUse"

	// max retry count for volume state
	// detach volume (in-use to available) may take more than 6 seconds.
	maxRetryCount               = 10
	defaultSleepTime            = 1 * time.Second
	defaultSleepTimeForInstance = 3 * time.Second

	// aws volume state
	volumeStateCreating  = "creating"
	volumeStateAvailable = "available"
	volumeStateInUse     = "in-use"
	volumeStateDeleting  = "deleting"
	volumeStateDeleted   = "deleted"
	volumeStateError     = "error"

	// aws volume attachment(attach/detach) state
	// attachment.status - The attachment state (attaching | attached | detaching | detached ).
	volumeAttachmentStateAttaching = "attaching"
	volumeAttachmentStateAttached  = "attached"
	volumeAttachmentStateDetaching = "detaching"
	volumeAttachmentStateDetached  = "detached"

	// aws instance state
	instanceStatePending      = "pending"
	instanceStateRunning      = "running"
	instanceStateShuttingDown = "shutting-down"
	instanceStateTerminated   = "terminated"
	instanceStateStopping     = "stopping"
	instanceStateStopped      = "stopped"

	// misc default value
	defaultInstanceType = "t2.micro"
	defaultImageID      = "ami-07713767"
	devicePrefix        = "/dev/xvd"
	controldbDevice     = "/dev/xvdf"
	firstDevice         = "/dev/xvdg"
	lastDeviceSeq       = "z"
)

// NewAWSEc2 creates a AWSEc2 instance
func NewAWSEc2(sess *session.Session) *AWSEc2 {
	s := &AWSEc2{
		sess: sess,
	}
	return s
}

// DetachVolume detaches the volume
// return error:
//   - IncorrectState if volume is at like available state
func (s *AWSEc2) DetachVolume(volID string, instanceID string, devName string) error {
	params := &ec2.DetachVolumeInput{
		VolumeId:   aws.String(volID),
		Device:     aws.String(devName),
		InstanceId: aws.String(instanceID),
	}

	svc := ec2.New(s.sess)
	resp, err := svc.DetachVolume(params)

	if err != nil {
		if err.(awserr.Error).Code() == errVolumeIncorrectState {
			glog.Infoln("DetachVolume", volID, "instanceID", instanceID, "device", devName,
				"got IncorrectState", err, "return ErrVolumeIncorrectState")
			return server.ErrVolumeIncorrectState
		}
		glog.Errorln("failed to DetachVolume", volID,
			"instance", instanceID, "device", devName, "error", err)
		return err
	}

	glog.Infoln("DetachVolume success", volID,
		"instance", instanceID, "device", devName, "resp", resp)
	return nil
}

// WaitVolumeDetached waits till volume is detached, state will be available
func (s *AWSEc2) WaitVolumeDetached(volID string) error {
	for i := 0; i < maxRetryCount; i++ {
		resp, err := s.describeVolumes(volID)
		if err != nil {
			glog.Errorln("WaitVolumeDetached failed to describe volume", volID, "error", err)
			return err
		}

		// describeVolumes may not return Attachments when volume is detached.
		// the state should be available
		if len(resp.Volumes[0].Attachments) == 0 {
			state := *(resp.Volumes[0].State)
			if state != volumeStateAvailable {
				glog.Errorln("describeVolumes returns no Attachments, but state is not available",
					"volume", volID, "resp", resp)
				return server.ErrVolumeIncorrectState
			}
			glog.Infoln("volume", volID, "detached")
			return nil
		}

		attachState := *(resp.Volumes[0].Attachments[0].State)
		switch attachState {
		case volumeAttachmentStateDetached:
			glog.Infoln("volume", volID, "detached")
			return nil
		case volumeAttachmentStateDetaching:
			glog.Infoln("volume", volID, "is still detaching")
			time.Sleep(defaultSleepTime)
		default:
			glog.Errorln("volume", volID, "is at wrong attach state", attachState)
			return server.ErrVolumeIncorrectState
		}
	}

	glog.Errorln("volume", volID, "is still not detached")
	return common.ErrTimeout
}

// GetVolumeState returns volume state, valid state:
//   creating | available | in-use | deleting | deleted | error
func (s *AWSEc2) GetVolumeState(volID string) (state string, err error) {
	info, err := s.GetVolumeInfo(volID)
	if err != nil {
		glog.Errorln("GetVolumeState", volID, "error", err)
		return "", nil
	}
	return info.State, nil
}

// GetVolumeInfo returns the volume information
func (s *AWSEc2) GetVolumeInfo(volID string) (info server.VolumeInfo, err error) {
	resp, err := s.describeVolumes(volID)
	if err != nil {
		glog.Errorln("GetVolumeInfo", volID, "error", err)
		return info, err
	}

	info = server.VolumeInfo{
		VolID: volID,
		State: *(resp.Volumes[0].State),
		Size:  *(resp.Volumes[0].Size),
	}

	// check the detail Attachments
	if len(resp.Volumes[0].Attachments) != 0 {
		attachment := resp.Volumes[0].Attachments[0]
		// If volume is at in-use state, describeVolumes may return Attachments with
		// attaching or detaching.
		switch *(attachment.State) {
		case volumeAttachmentStateDetaching:
			info.State = server.VolumeStateDetaching
		case volumeAttachmentStateAttaching:
			info.State = server.VolumeStateAttaching
		}

		info.AttachInstanceID = *(attachment.InstanceId)
		info.Device = *(attachment.Device)
	}

	glog.Infoln("GetVolumeInfo", info)
	return info, nil
}

func (s *AWSEc2) describeVolumes(volID string) (*ec2.DescribeVolumesOutput, error) {
	params := &ec2.DescribeVolumesInput{
		VolumeIds: []*string{
			aws.String(volID),
		},
	}

	svc := ec2.New(s.sess)
	resp, err := svc.DescribeVolumes(params)

	if err != nil {
		glog.Errorln("failed to DescribeVolumes", volID, "error", err)
		return nil, err
	}
	if len(resp.Volumes) != 1 {
		glog.Errorln("get more than 1 volume, volID", volID, "resp", resp)
		return nil, common.ErrInternal
	}

	glog.Infoln("describe volume", volID, "resp", resp)
	return resp, nil
}

// AttachVolume attaches the volume
func (s *AWSEc2) AttachVolume(volID string, instanceID string, devName string) error {
	params := &ec2.AttachVolumeInput{
		VolumeId:   aws.String(volID),
		Device:     aws.String(devName),
		InstanceId: aws.String(instanceID),
	}

	svc := ec2.New(s.sess)
	resp, err := svc.AttachVolume(params)

	if err != nil {
		if err.(awserr.Error).Code() == errVolumeInUse {
			glog.Errorln("AttachVolume VolumeInUse error", err, "volID", volID,
				"instance", instanceID, "device", devName, "resp", resp)
			return server.ErrVolumeInUse
		}
		glog.Errorln("failed to AttachVolume", volID, "instance", instanceID,
			"device", devName, "error", err, "resp", resp)
		return err
	}

	glog.Infoln("AttachVolume success", volID,
		"instance", instanceID, "device", devName, "resp", resp)
	return nil
}

// WaitVolumeAttached waits till volume is attached, e.g. volume state becomes in-use
func (s *AWSEc2) WaitVolumeAttached(volID string) error {
	for i := 0; i < maxRetryCount; i++ {
		resp, err := s.describeVolumes(volID)
		if err != nil {
			glog.Errorln("WaitVolumeAttached failed to describe volume", volID, "error", err)
			return err
		}

		// describeVolumes may not return Attachments when volume is attached.
		// the state should be in-use
		if len(resp.Volumes[0].Attachments) == 0 {
			state := *(resp.Volumes[0].State)
			if state != volumeStateInUse {
				glog.Errorln("describeVolumes returns no Attachments, but state is not in-use",
					"volume", volID, "resp", resp)
				return server.ErrVolumeIncorrectState
			}
			glog.Infoln("volume", volID, "attached")
			return nil
		}

		attachState := *(resp.Volumes[0].Attachments[0].State)
		switch attachState {
		case volumeAttachmentStateAttached:
			glog.Infoln("volume", volID, "attached")
			return nil
		case volumeAttachmentStateAttaching:
			glog.Infoln("volume", volID, "is still attaching")
			time.Sleep(defaultSleepTime)
		default:
			glog.Errorln("volume", volID, "is at wrong attach state", attachState)
			return server.ErrVolumeIncorrectState
		}
	}

	glog.Errorln("volume", volID, "is still not attached")
	return common.ErrTimeout
}

// CreateVolume creates one volume
func (s *AWSEc2) CreateVolume(az string, volSizeGB int64) (volID string, err error) {
	params := &ec2.CreateVolumeInput{
		AvailabilityZone: aws.String(az),
		Size:             aws.Int64(volSizeGB),
		VolumeType:       aws.String(server.VolumeTypeGPSSD),
	}

	svc := ec2.New(s.sess)
	resp, err := svc.CreateVolume(params)

	if err != nil {
		glog.Errorln("failed to CreateVolume, AvailabilityZone", az, "error", err)
		return "", err
	}

	volID = *(resp.VolumeId)

	glog.Infoln("created volume", volID, "resp", resp)
	return volID, nil
}

// WaitVolumeCreated waits till volume is created
func (s *AWSEc2) WaitVolumeCreated(volID string) error {
	return s.waitVolumeState(volID, volumeStateCreating, volumeStateAvailable)
}

// DeleteVolume deletes the volume
func (s *AWSEc2) DeleteVolume(volID string) error {
	params := &ec2.DeleteVolumeInput{
		VolumeId: aws.String(volID),
	}

	svc := ec2.New(s.sess)
	resp, err := svc.DeleteVolume(params)

	if err != nil {
		glog.Errorln("failed to DeleteVolume", volID, "error", err)
		return err
	}

	glog.Infoln("deleted volume", volID, "resp", resp)
	return nil
}

func (s *AWSEc2) waitVolumeState(volID string, state string, expectedState string) error {
	for i := 0; i < maxRetryCount; i++ {
		state1, err := s.GetVolumeState(volID)
		if err != nil {
			glog.Errorln("failed to GetVolumeState", volID, "error", err,
				"wait state", state, "to", expectedState)
			return err
		}

		if state1 == expectedState {
			glog.Infoln("volume", volID, "state from", state, "to", expectedState)
			return nil
		}

		if state1 != state {
			glog.Errorln("wait volume", volID, "state", state, "to", expectedState, "but get state", state1)
			return common.ErrInternal
		}

		glog.Infoln("wait volume", volID, "state", state, "to", expectedState)

		time.Sleep(defaultSleepTime)
	}
	return common.ErrTimeout
}

// GetControlDBDeviceName returns the default controldb device name
func (s *AWSEc2) GetControlDBDeviceName() string {
	return controldbDevice
}

// GetFirstDeviceName returns the first device name
// AWS EBS only supports device names, /dev/xvd[b-c][a-z], recommend /dev/xvd[f-p]
func (s *AWSEc2) GetFirstDeviceName() string {
	return firstDevice
}

// GetNextDeviceName returns the next device name
func (s *AWSEc2) GetNextDeviceName(lastDev string) (devName string, err error) {
	// format check
	if !strings.HasPrefix(lastDev, devicePrefix) {
		glog.Errorln("device should have prefix", devicePrefix, "lastDev", lastDev)
		return "", common.ErrInternal
	}

	devSeq := strings.TrimPrefix(lastDev, devicePrefix)

	if len(devSeq) != 1 && len(devSeq) != 2 {
		glog.Errorln("invalid lastDev", lastDev)
		return "", common.ErrInternal
	}

	if len(devSeq) == 1 {
		if devSeq != lastDeviceSeq {
			// devSeq != "z"
			devName = devicePrefix + string(devSeq[0]+1)
		} else {
			// devSeq == "z"
			devName = devicePrefix + "ba"
		}

		glog.Infoln("lastDev", lastDev, "assign next device", devName)
		return devName, nil
	}

	// lastDev is like /dev/xvdba
	firstSeq := string(devSeq[0])
	secondSeq := string(devSeq[1])

	if secondSeq != lastDeviceSeq {
		devName = devicePrefix + firstSeq + string(devSeq[1]+1)
	} else {
		// secondSeq is "z"
		if firstSeq == "c" {
			glog.Errorln("reach aws max allowed devices, lastDev", lastDev)
			return "", common.ErrInternal
		}
		devName = devicePrefix + "ca"
	}

	glog.Infoln("lastDev", lastDev, "assign next device", devName)
	return devName, nil
}

// Below functions are for unit test only

// LaunchOneInstance launches a new instance at specified az
func (s *AWSEc2) LaunchOneInstance(az string) (instanceID string, err error) {
	params := &ec2.RunInstancesInput{
		ImageId:      aws.String(defaultImageID),
		MaxCount:     aws.Int64(1),
		MinCount:     aws.Int64(1),
		InstanceType: aws.String(defaultInstanceType),
		Placement: &ec2.Placement{
			AvailabilityZone: aws.String(az),
		},
	}

	svc := ec2.New(s.sess)
	resp, err := svc.RunInstances(params)

	if err != nil {
		glog.Errorln("failed to RunOneInstance", params, "error", err)
		return "", err
	}
	if len(resp.Instances) != 1 {
		glog.Errorln("launch more than 1 instance", resp)
		return "", common.ErrInternal
	}

	instanceID = *(resp.Instances[0].InstanceId)

	glog.Infoln("launch instance", instanceID, "az", az)

	return instanceID, nil
}

// WaitInstanceRunning waits till the instance is at running state
func (s *AWSEc2) WaitInstanceRunning(instanceID string) error {
	for i := 0; i < maxRetryCount; i++ {
		state1, err := s.GetInstanceState(instanceID)
		if err != nil {
			glog.Errorln("failed to get instance", instanceID, "state, error", err)
			return err
		}

		if state1 == instanceStateRunning {
			return nil
		}

		glog.Infoln("instance", instanceID, "state", state1)
		time.Sleep(defaultSleepTimeForInstance)
	}

	return common.ErrTimeout
}

// GetInstanceState returns instance state, valid state:
// 	 pending | running | shutting-down | terminated | stopping | stopped
func (s *AWSEc2) GetInstanceState(instanceID string) (state string, err error) {
	params := &ec2.DescribeInstanceStatusInput{
		IncludeAllInstances: aws.Bool(true),
		InstanceIds: []*string{
			aws.String(instanceID),
		},
	}

	svc := ec2.New(s.sess)
	resp, err := svc.DescribeInstanceStatus(params)
	if err != nil {
		glog.Errorln("failed to DescribeInstanceStatus, instanceID", instanceID, "error", err)
		return "", err
	}
	if len(resp.InstanceStatuses) != 1 {
		glog.Errorln("not get 1 instance status, instance", instanceID, "resp", resp)
		return "", common.ErrInternal
	}

	state = *(resp.InstanceStatuses[0].InstanceState.Name)

	glog.Infoln("instance", instanceID, "state", state)
	return state, nil
}

// TerminateInstance terminates one instance
func (s *AWSEc2) TerminateInstance(instanceID string) error {
	params := &ec2.TerminateInstancesInput{
		InstanceIds: []*string{
			aws.String(instanceID),
		},
	}

	svc := ec2.New(s.sess)
	_, err := svc.TerminateInstances(params)

	if err != nil {
		glog.Errorln("failed to TerminateInstance", instanceID, "error", err)
		return err
	}

	glog.Infoln("terminated instance", instanceID)
	return nil
}

// CreateVpc creates one vpc
func (s *AWSEc2) CreateVpc() (vpcID string, err error) {
	cidrBlock := "10.0.0.0/16"
	params := &ec2.CreateVpcInput{
		CidrBlock: aws.String(cidrBlock),
	}

	svc := ec2.New(s.sess)
	resp, err := svc.CreateVpc(params)
	if err != nil {
		glog.Errorln("CreateVpc error", err)
		return "", err
	}

	glog.Infoln("created vpc", resp.Vpc.VpcId, "resp", resp)
	return *(resp.Vpc.VpcId), nil
}

// DeleteVpc deletes the vpc
func (s *AWSEc2) DeleteVpc(vpcID string) error {
	params := &ec2.DeleteVpcInput{
		VpcId: aws.String(vpcID),
	}

	svc := ec2.New(s.sess)
	_, err := svc.DeleteVpc(params)
	if err != nil {
		glog.Errorln("DeleteVpc error", err, "vpcID", vpcID)
		return err
	}

	glog.Infoln("deleted vpc", vpcID)
	return nil
}
