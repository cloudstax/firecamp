package awsec2

import (
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/server"
	"github.com/cloudstax/openmanage/utils"
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
	defaultSleepTime            = 2 * time.Second
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
func (s *AWSEc2) DetachVolume(ctx context.Context, volID string, instanceID string, devName string) error {
	requuid := utils.GetReqIDFromContext(ctx)

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
				"got IncorrectState", err, "return ErrVolumeIncorrectState, requuid", requuid)
			return server.ErrVolumeIncorrectState
		}
		glog.Errorln("failed to DetachVolume", volID, "instance", instanceID,
			"device", devName, "error", err, "requuid", requuid)
		return err
	}

	glog.Infoln("DetachVolume success", volID, "instance", instanceID,
		"device", devName, "requuid", requuid, "resp", resp)
	return nil
}

// WaitVolumeDetached waits till volume is detached, state will be available
func (s *AWSEc2) WaitVolumeDetached(ctx context.Context, volID string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	for i := 0; i < maxRetryCount; i++ {
		resp, err := s.describeVolumes(ctx, volID)
		if err != nil {
			glog.Errorln("WaitVolumeDetached failed to describe volume", volID, "error", err, "requuid", requuid)
			return err
		}

		// describeVolumes may not return Attachments when volume is detached.
		// the state should be available
		if len(resp.Volumes[0].Attachments) == 0 {
			state := *(resp.Volumes[0].State)
			if state != volumeStateAvailable {
				glog.Errorln("describeVolumes returns no Attachments, but state is not available",
					"volume", volID, "requuid", requuid, "resp", resp)
				return server.ErrVolumeIncorrectState
			}
			glog.Infoln("volume", volID, "detached", "requuid", requuid)
			return nil
		}

		attachState := *(resp.Volumes[0].Attachments[0].State)
		switch attachState {
		case volumeAttachmentStateDetached:
			glog.Infoln("volume", volID, "detached, requuid", requuid)
			return nil
		case volumeAttachmentStateDetaching:
			glog.Infoln("volume", volID, "is still detaching, requuid", requuid)
			time.Sleep(defaultSleepTime)
		default:
			glog.Errorln("volume", volID, "is at wrong attach state", attachState, "requuid", requuid)
			return server.ErrVolumeIncorrectState
		}
	}

	glog.Errorln("volume", volID, "is still not detached, requuid", requuid)
	return common.ErrTimeout
}

// GetVolumeState returns volume state, valid state:
//   creating | available | in-use | deleting | deleted | error
func (s *AWSEc2) GetVolumeState(ctx context.Context, volID string) (state string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	info, err := s.GetVolumeInfo(ctx, volID)
	if err != nil {
		glog.Errorln("GetVolumeState", volID, "error", err, "requuid", requuid)
		return "", nil
	}
	return info.State, nil
}

// GetVolumeInfo returns the volume information
func (s *AWSEc2) GetVolumeInfo(ctx context.Context, volID string) (info server.VolumeInfo, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	resp, err := s.describeVolumes(ctx, volID)
	if err != nil {
		glog.Errorln("GetVolumeInfo", volID, "error", err, "requuid", requuid)
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

	glog.Infoln("GetVolumeInfo", info, "requuid", requuid)
	return info, nil
}

func (s *AWSEc2) describeVolumes(ctx context.Context, volID string) (*ec2.DescribeVolumesOutput, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	params := &ec2.DescribeVolumesInput{
		VolumeIds: []*string{
			aws.String(volID),
		},
	}

	svc := ec2.New(s.sess)
	resp, err := svc.DescribeVolumes(params)

	if err != nil {
		glog.Errorln("failed to DescribeVolumes", volID, "error", err, "requuid", requuid)
		return nil, err
	}
	if len(resp.Volumes) != 1 {
		glog.Errorln("get more than 1 volume, volID", volID, "requuid", requuid, "resp", resp)
		return nil, common.ErrInternal
	}

	glog.Infoln("describe volume", volID, "requuid", requuid, "resp", resp)
	return resp, nil
}

// AttachVolume attaches the volume
func (s *AWSEc2) AttachVolume(ctx context.Context, volID string, instanceID string, devName string) error {
	requuid := utils.GetReqIDFromContext(ctx)

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
				"instance", instanceID, "device", devName, "requuid", requuid, "resp", resp)
			return server.ErrVolumeInUse
		}
		glog.Errorln("failed to AttachVolume", volID, "instance", instanceID,
			"device", devName, "error", err, "requuid", requuid, "resp", resp)
		return err
	}

	glog.Infoln("AttachVolume success", volID, "instance", instanceID,
		"device", devName, "requuid", requuid, "resp", resp)
	return nil
}

// WaitVolumeAttached waits till volume is attached, e.g. volume state becomes in-use
func (s *AWSEc2) WaitVolumeAttached(ctx context.Context, volID string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	for i := 0; i < maxRetryCount; i++ {
		resp, err := s.describeVolumes(ctx, volID)
		if err != nil {
			glog.Errorln("WaitVolumeAttached failed to describe volume", volID, "error", err, "requuid", requuid)
			return err
		}

		// describeVolumes may not return Attachments when volume is attached.
		// the state should be in-use
		if len(resp.Volumes[0].Attachments) == 0 {
			state := *(resp.Volumes[0].State)
			if state != volumeStateInUse {
				glog.Errorln("describeVolumes returns no Attachments, but state is not in-use",
					"volume", volID, "requuid", requuid, "resp", resp)
				return server.ErrVolumeIncorrectState
			}
			glog.Infoln("volume", volID, "attached, requuid", requuid)
			return nil
		}

		attachState := *(resp.Volumes[0].Attachments[0].State)
		switch attachState {
		case volumeAttachmentStateAttached:
			glog.Infoln("volume", volID, "attached, requuid", requuid)
			return nil
		case volumeAttachmentStateAttaching:
			glog.Infoln("volume", volID, "is still attaching, requuid", requuid)
			time.Sleep(defaultSleepTime)
		default:
			glog.Errorln("volume", volID, "is at wrong attach state", attachState, "requuid", requuid)
			return server.ErrVolumeIncorrectState
		}
	}

	glog.Errorln("volume", volID, "is still not attached, requuid", requuid)
	return common.ErrTimeout
}

// CreateVolume creates one volume
func (s *AWSEc2) CreateVolume(ctx context.Context, az string, volSizeGB int64) (volID string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	params := &ec2.CreateVolumeInput{
		AvailabilityZone: aws.String(az),
		Size:             aws.Int64(volSizeGB),
		VolumeType:       aws.String(server.VolumeTypeGPSSD),
	}

	svc := ec2.New(s.sess)
	resp, err := svc.CreateVolume(params)

	if err != nil {
		glog.Errorln("failed to CreateVolume, AvailabilityZone", az, "error", err, "requuid", requuid)
		return "", err
	}

	volID = *(resp.VolumeId)

	glog.Infoln("created volume", volID, "requuid", requuid, "resp", resp)
	return volID, nil
}

// WaitVolumeCreated waits till volume is created
func (s *AWSEc2) WaitVolumeCreated(ctx context.Context, volID string) error {
	return s.waitVolumeState(ctx, volID, volumeStateCreating, volumeStateAvailable)
}

// DeleteVolume deletes the volume
func (s *AWSEc2) DeleteVolume(ctx context.Context, volID string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	params := &ec2.DeleteVolumeInput{
		VolumeId: aws.String(volID),
	}

	svc := ec2.New(s.sess)
	resp, err := svc.DeleteVolume(params)

	if err != nil {
		glog.Errorln("failed to DeleteVolume", volID, "error", err, "requuid", requuid)
		return err
	}

	glog.Infoln("deleted volume", volID, "requuid", requuid, "resp", resp)
	return nil
}

func (s *AWSEc2) waitVolumeState(ctx context.Context, volID string, state string, expectedState string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	for i := 0; i < maxRetryCount; i++ {
		state1, err := s.GetVolumeState(ctx, volID)
		if err != nil {
			glog.Errorln("failed to GetVolumeState", volID, "error", err,
				"wait state", state, "to", expectedState, "requuid", requuid)
			return err
		}

		if state1 == expectedState {
			glog.Infoln("volume", volID, "state from", state, "to", expectedState, "requuid", requuid)
			return nil
		}

		if state1 != state {
			glog.Errorln("wait volume", volID, "state", state, "to", expectedState,
				"but get state", state1, "requuid", requuid)
			return common.ErrInternal
		}

		glog.Infoln("wait volume", volID, "state", state, "to", expectedState, "requuid", requuid)

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
func (s *AWSEc2) LaunchOneInstance(ctx context.Context, az string) (instanceID string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

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
		glog.Errorln("failed to RunOneInstance", params, "error", err, "requuid", requuid)
		return "", err
	}
	if len(resp.Instances) != 1 {
		glog.Errorln("launch more than 1 instance, requuid", requuid, resp)
		return "", common.ErrInternal
	}

	instanceID = *(resp.Instances[0].InstanceId)

	glog.Infoln("launch instance", instanceID, "az", az, "requuid", requuid)

	return instanceID, nil
}

// WaitInstanceRunning waits till the instance is at running state
func (s *AWSEc2) WaitInstanceRunning(ctx context.Context, instanceID string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	for i := 0; i < maxRetryCount; i++ {
		state1, err := s.GetInstanceState(ctx, instanceID)
		if err != nil {
			glog.Errorln("failed to get instance", instanceID, "state, error", err, "requuid", requuid)
			return err
		}

		if state1 == instanceStateRunning {
			return nil
		}

		glog.Infoln("instance", instanceID, "state", state1, "requuid", requuid)
		time.Sleep(defaultSleepTimeForInstance)
	}

	return common.ErrTimeout
}

// GetInstanceState returns instance state, valid state:
// 	 pending | running | shutting-down | terminated | stopping | stopped
func (s *AWSEc2) GetInstanceState(ctx context.Context, instanceID string) (state string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	params := &ec2.DescribeInstanceStatusInput{
		IncludeAllInstances: aws.Bool(true),
		InstanceIds: []*string{
			aws.String(instanceID),
		},
	}

	svc := ec2.New(s.sess)
	resp, err := svc.DescribeInstanceStatus(params)
	if err != nil {
		glog.Errorln("failed to DescribeInstanceStatus, instanceID", instanceID, "error", err, "requuid", requuid)
		return "", err
	}
	if len(resp.InstanceStatuses) != 1 {
		glog.Errorln("not get 1 instance status, instance", instanceID, "requuid", requuid, "resp", resp)
		return "", common.ErrInternal
	}

	state = *(resp.InstanceStatuses[0].InstanceState.Name)

	glog.Infoln("instance", instanceID, "state", state, "requuid", requuid)
	return state, nil
}

// TerminateInstance terminates one instance
func (s *AWSEc2) TerminateInstance(ctx context.Context, instanceID string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	params := &ec2.TerminateInstancesInput{
		InstanceIds: []*string{
			aws.String(instanceID),
		},
	}

	svc := ec2.New(s.sess)
	_, err := svc.TerminateInstances(params)

	if err != nil {
		glog.Errorln("failed to TerminateInstance", instanceID, "error", err, "requuid", requuid)
		return err
	}

	glog.Infoln("terminated instance", instanceID, "requuid", requuid)
	return nil
}

// CreateVpc creates one vpc
func (s *AWSEc2) CreateVpc(ctx context.Context) (vpcID string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	cidrBlock := "10.0.0.0/16"
	params := &ec2.CreateVpcInput{
		CidrBlock: aws.String(cidrBlock),
	}

	svc := ec2.New(s.sess)
	resp, err := svc.CreateVpc(params)
	if err != nil {
		glog.Errorln("CreateVpc error", err, "requuid", requuid)
		return "", err
	}

	glog.Infoln("created vpc", resp.Vpc.VpcId, "requuid", requuid, "resp", resp)
	return *(resp.Vpc.VpcId), nil
}

// DeleteVpc deletes the vpc
func (s *AWSEc2) DeleteVpc(ctx context.Context, vpcID string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	params := &ec2.DeleteVpcInput{
		VpcId: aws.String(vpcID),
	}

	svc := ec2.New(s.sess)
	_, err := svc.DeleteVpc(params)
	if err != nil {
		glog.Errorln("DeleteVpc error", err, "vpcID", vpcID, "requuid", requuid)
		return err
	}

	glog.Infoln("deleted vpc", vpcID, "requuid", requuid)
	return nil
}
