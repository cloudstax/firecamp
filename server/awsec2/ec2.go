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

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/server"
	"github.com/cloudstax/firecamp/utils"
)

// AWSEc2 handles the ec2 related functions
type AWSEc2 struct {
	sess *session.Session
}

const (
	// aws specific error
	errVolumeIncorrectState  = "IncorrectState"
	errVolumeInUse           = "VolumeInUse"
	errVolumeNotFound        = "InvalidVolume.NotFound"
	errInvalidParameterValue = "InvalidParameterValue"

	// max retry count for volume state
	// detach volume (in-use to available) may take more than 6 seconds.
	maxRetryCount               = 10
	defaultSleepTime            = 2 * time.Second
	defaultSleepTimeForInstance = 4 * time.Second

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
	devicePrefix        = "/dev/xvd"
	controldbDevice     = "/dev/xvdf"
	firstDevice         = "/dev/xvdg"
	lastDeviceSeq       = "z"

	filterAz               = "availability-zone"
	filterVpc              = "vpc-id"
	filterTagWorkerKey     = common.SystemName + "-" + common.ContainerPlatformRoleWorker
	filterTagWorker        = "tag:" + filterTagWorkerKey
	filterAttachInstanceID = "attachment.instance-id"
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
			"device", devName, "error", err, "requuid", requuid, "resp", resp)
		return err
	}

	glog.Infoln("DetachVolume success", volID, "instance", instanceID, "device", devName, "requuid", requuid)
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
		if err.(awserr.Error).Code() == errVolumeNotFound {
			return "", common.ErrNotFound
		}
		return "", err
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

	glog.Infoln("start describe volume", volID, "requuid", requuid)

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

	glog.Infoln("describe volume", volID, "state", resp.Volumes[0].State, "requuid", requuid)
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

	glog.Infoln("AttachVolume success", volID, "instance", instanceID, "device", devName, "requuid", requuid)
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
func (s *AWSEc2) CreateVolume(ctx context.Context, opts *server.CreateVolumeOptions) (volID string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	params := &ec2.CreateVolumeInput{
		AvailabilityZone: aws.String(opts.AvailabilityZone),
		VolumeType:       aws.String(opts.VolumeType),
		Size:             aws.Int64(opts.VolumeSizeGB),
	}
	if opts.VolumeType == common.VolumeTypeIOPSSSD {
		params.Iops = aws.Int64(opts.Iops)
	}
	if len(opts.TagSpecs) != 0 {
		tags := make([]*ec2.Tag, len(opts.TagSpecs))
		for i, tag := range opts.TagSpecs {
			ec2tag := &ec2.Tag{
				Key:   aws.String(tag.Key),
				Value: aws.String(tag.Value),
			}
			tags[i] = ec2tag
		}

		ec2tagspec := &ec2.TagSpecification{
			ResourceType: aws.String("volume"),
			Tags:         tags,
		}

		params.TagSpecifications = []*ec2.TagSpecification{ec2tagspec}
	}

	svc := ec2.New(s.sess)
	resp, err := svc.CreateVolume(params)

	if err != nil {
		glog.Errorln("CreateVolume error", err, "requuid", requuid, opts)
		return "", err
	}

	volID = *(resp.VolumeId)

	glog.Infoln("created volume", volID, "requuid", requuid)
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

// GetNetworkInterfaces gets all network interfaces in the cluster, vpc and zone.
func (s *AWSEc2) GetNetworkInterfaces(ctx context.Context, cluster string,
	vpcID string, zone string) (netInterfaces []*server.NetworkInterface, cidrBlock string, err error) {

	requuid := utils.GetReqIDFromContext(ctx)

	filters := []*ec2.Filter{
		{
			Name: aws.String(filterVpc),
			Values: []*string{
				aws.String(vpcID),
			},
		},
		{
			Name: aws.String(filterAz),
			Values: []*string{
				aws.String(zone),
			},
		},
		{
			Name: aws.String(filterTagWorker),
			Values: []*string{
				aws.String(cluster),
			},
		},
	}

	svc := ec2.New(s.sess)

	var instanceIDs []string
	nextToken := ""
	subnetID := ""
	for true {
		instanceIDs, subnetID, nextToken, err = s.describeInstances(ctx, filters, nextToken, svc, requuid)
		if err != nil {
			glog.Errorln("DescribeInstances error", err, "filter", filters, "token", nextToken, "requuid", requuid)
			return nil, "", err
		}

		glog.Infoln("list", len(instanceIDs), "instances, nextToken", nextToken, "filter", filters, "requuid", requuid)

		for _, instanceID := range instanceIDs {
			netInterface, err := s.getInstanceNetworkInterface(ctx, instanceID, svc, requuid)
			if err != nil {
				glog.Errorln("GetInstanceNetworkInterface error", err, "instance", instanceID, "filter", filters, "requuid", requuid)
				return nil, "", err
			}

			netInterfaces = append(netInterfaces, netInterface)
		}

		if len(nextToken) == 0 {
			// no more instances
			break
		}
	}

	// get subnet cidrBlock
	subnet, err := s.describeSubnet(ctx, subnetID, svc, requuid)
	if err != nil {
		glog.Errorln("describeSubnet error", err, subnetID, "requuid", requuid)
		return nil, "", err
	}

	glog.Infoln("get", len(netInterfaces), "network interfaces, subnet", subnet, "filter", filters, "requuid", requuid)
	return netInterfaces, *(subnet.CidrBlock), nil
}

// GetInstanceNetworkInterface gets the network interface of the instance.
// Assume one instance only has one network interface.
func (s *AWSEc2) GetInstanceNetworkInterface(ctx context.Context, instanceID string) (netInterface *server.NetworkInterface, err error) {
	requuid := utils.GetReqIDFromContext(ctx)
	svc := ec2.New(s.sess)

	return s.getInstanceNetworkInterface(ctx, instanceID, svc, requuid)
}

func (s *AWSEc2) getInstanceNetworkInterface(ctx context.Context, instanceID string, svc *ec2.EC2, requuid string) (netInterface *server.NetworkInterface, err error) {
	filters := []*ec2.Filter{
		{
			Name: aws.String(filterAttachInstanceID),
			Values: []*string{
				aws.String(instanceID),
			},
		},
	}

	input := &ec2.DescribeNetworkInterfacesInput{
		Filters: filters,
	}

	res, err := svc.DescribeNetworkInterfaces(input)
	if err != nil {
		glog.Errorln("DescribeNetworkInterfaces error", err, "instance", instanceID, "requuid", requuid)
		return nil, err
	}
	if len(res.NetworkInterfaces) != 1 {
		glog.Errorln("expect 1 network interface, get", len(res.NetworkInterfaces),
			res.NetworkInterfaces, "instance", instanceID, "requuid", requuid)
		return nil, common.ErrInternal
	}

	netInterface = &server.NetworkInterface{
		InterfaceID:      *(res.NetworkInterfaces[0].NetworkInterfaceId),
		ServerInstanceID: instanceID,
	}

	// get the private ips
	ips := []string{}
	for _, ip := range res.NetworkInterfaces[0].PrivateIpAddresses {
		if *(ip.Primary) {
			netInterface.PrimaryPrivateIP = *(ip.PrivateIpAddress)
			continue
		}
		ips = append(ips, *(ip.PrivateIpAddress))
	}

	netInterface.PrivateIPs = ips

	glog.Infoln("get network interface", netInterface, "for instance", instanceID, "requuid", requuid)
	return netInterface, err
}

// AssignStaticIP assigns the static ip to the network interface.
func (s *AWSEc2) AssignStaticIP(ctx context.Context, networkInterfaceID string, staticIP string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	svc := ec2.New(s.sess)

	input := &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String(networkInterfaceID),
		PrivateIpAddresses: []*string{aws.String(staticIP)},
	}

	_, err := svc.AssignPrivateIpAddresses(input)
	if err != nil {
		glog.Errorln("AssignPrivateIpAddresses error", err, "network interface id", networkInterfaceID, "ip", staticIP, "requuid", requuid)
		return err
	}

	glog.Infoln("assigned private ip", staticIP, "to network interface", networkInterfaceID, "requuid", requuid)
	return nil
}

// UnassignStaticIP unassigns the staticIP from the networkInterface. If the networkInterface does not
// own the ip, should return success.
func (s *AWSEc2) UnassignStaticIP(ctx context.Context, networkInterfaceID string, staticIP string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	svc := ec2.New(s.sess)

	input := &ec2.UnassignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String(networkInterfaceID),
		PrivateIpAddresses: []*string{aws.String(staticIP)},
	}

	_, err := svc.UnassignPrivateIpAddresses(input)
	if err != nil {
		if err.(awserr.Error).Code() == errInvalidParameterValue {
			glog.Errorln("network interface", networkInterfaceID, "does not own ip", staticIP, "requuid", requuid, "error", err)
			return nil
		}

		glog.Errorln("UnassignPrivateIpAddresses error", err, "network interface id", networkInterfaceID, "ip", staticIP, "requuid", requuid)
		return err
	}

	glog.Infoln("unassigned private ip", staticIP, "from network interface", networkInterfaceID, "requuid", requuid)
	return nil
}

// DescribeSubnet returns the ec2 subnet
func (s *AWSEc2) DescribeSubnet(ctx context.Context, subnetID string) (*ec2.Subnet, error) {
	requuid := utils.GetReqIDFromContext(ctx)
	svc := ec2.New(s.sess)
	return s.describeSubnet(ctx, subnetID, svc, requuid)
}

func (s *AWSEc2) describeSubnet(ctx context.Context, subnetID string, svc *ec2.EC2, requuid string) (*ec2.Subnet, error) {
	input := &ec2.DescribeSubnetsInput{
		SubnetIds: []*string{aws.String(subnetID)},
	}

	res, err := svc.DescribeSubnets(input)
	if err != nil {
		glog.Errorln("DescribeSubnets error", err, "subnetID", subnetID, "requuid", requuid)
		return nil, err
	}
	if len(res.Subnets) != 1 {
		glog.Errorln("get", len(res.Subnets), "subnets for subnetID", subnetID, "requuid", requuid)
		return nil, common.ErrInternal
	}

	glog.Infoln("get subnet", res.Subnets[0], "requuid", requuid)
	return res.Subnets[0], nil
}

func (s *AWSEc2) describeInstances(ctx context.Context, filters []*ec2.Filter, token string, svc *ec2.EC2, requuid string) (instanceIDs []string, subnetID string, nextToken string, err error) {
	input := &ec2.DescribeInstancesInput{
		Filters: filters,
	}
	if len(token) != 0 {
		input.NextToken = aws.String(token)
	}

	res, err := svc.DescribeInstances(input)
	if err != nil {
		glog.Errorln("DescribeInstances error", err, "token", token, "filters", filters, "requuid", requuid)
		return nil, "", "", err
	}

	for _, reservation := range res.Reservations {
		for _, instance := range reservation.Instances {
			glog.V(1).Infoln("DescribeInstances get instance", *(instance.InstanceId), "token", token, "requuid", requuid)
			instanceIDs = append(instanceIDs, *(instance.InstanceId))
			subnetID = *(instance.SubnetId)
		}
	}

	if res.NextToken != nil {
		nextToken = *(res.NextToken)
	}

	glog.Infoln("get", len(instanceIDs), "instances, subnet", subnetID, "next token", nextToken, "requuid", requuid)
	return instanceIDs, subnetID, nextToken, nil
}

// Below functions are for unit test only

// LaunchOneInstance launches a new instance at specified az
func (s *AWSEc2) LaunchOneInstance(ctx context.Context, imageID string, az string, cluster string) (instanceID string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	params := &ec2.RunInstancesInput{
		ImageId:      aws.String(imageID),
		MaxCount:     aws.Int64(1),
		MinCount:     aws.Int64(1),
		InstanceType: aws.String(defaultInstanceType),
		Placement: &ec2.Placement{
			AvailabilityZone: aws.String(az),
		},
		TagSpecifications: []*ec2.TagSpecification{
			&ec2.TagSpecification{
				ResourceType: aws.String("instance"),
				Tags: []*ec2.Tag{
					&ec2.Tag{
						Key:   aws.String(filterTagWorkerKey),
						Value: aws.String(cluster),
					},
				},
			},
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

	glog.Infoln("launch instance", instanceID, "az", az, "cluster", cluster, "requuid", requuid)

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

// DescribeInstance gets the ec2 instance.
func (s *AWSEc2) DescribeInstance(ctx context.Context, instanceID string) (*ec2.Instance, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	input := &ec2.DescribeInstancesInput{
		InstanceIds: []*string{
			aws.String(instanceID),
		},
	}

	svc := ec2.New(s.sess)
	res, err := svc.DescribeInstances(input)
	if err != nil {
		glog.Errorln("DescribeInstances error", err, "instanceID", instanceID, "requuid", requuid)
		return nil, err
	}
	if len(res.Reservations) != 1 {
		glog.Errorln("expect 1 reservation, get", len(res.Reservations), "requuid", requuid)
		return nil, common.ErrInternal
	}
	if len(res.Reservations[0].Instances) != 1 {
		glog.Errorln("expect 1 instance, get", len(res.Reservations[0].Instances), "requuid", requuid)
		return nil, common.ErrInternal
	}

	glog.Infoln("get instance", res.Reservations[0].Instances[0], "requuid", requuid)
	return res.Reservations[0].Instances[0], nil
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
