package awsec2

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/glog"

	"github.com/cloudstax/openmanage/common"
)

// Ec2Info implements server.Info
type Ec2Info struct {
	instanceID string
	hostname   string
	vpcID      string
	az         string
	region     string
}

// NewEc2Info creates a Ec2Info instance
func NewEc2Info(sess *session.Session) (*Ec2Info, error) {
	instanceID, err := GetLocalEc2InstanceID()
	if err != nil {
		glog.Errorln("GetLocalEc2InstanceID error", err)
		return nil, err
	}

	svc := ec2.New(sess)
	params := &ec2.DescribeInstancesInput{
		InstanceIds: []*string{
			aws.String(instanceID),
		},
	}
	resp, err := svc.DescribeInstances(params)
	if err != nil {
		glog.Errorln("DescribeInstances error", err, "instanceID", instanceID)
		return nil, err
	}
	if len(resp.Reservations) != 1 {
		glog.Errorln("DescribeInstances expect 1 Reservations, but got", len(resp.Reservations), resp)
		return nil, common.ErrInternal
	}
	if len(resp.Reservations[0].Instances) != 1 {
		glog.Errorln("DescribeInstances expect 1 instance, but got", len(resp.Reservations[0].Instances), resp)
		return nil, common.ErrInternal
	}

	instance := resp.Reservations[0].Instances[0]

	az := *(instance.Placement.AvailabilityZone)
	s := &Ec2Info{
		instanceID: instanceID,
		hostname:   *(instance.PrivateDnsName),
		vpcID:      *(instance.VpcId),
		az:         az,
		// az must be like us-west-1c, get region from it
		region: az[:len(az)-1],
	}
	return s, nil
}

func (s *Ec2Info) GetLocalHostname() string {
	return s.hostname
}

func (s *Ec2Info) GetLocalAvailabilityZone() string {
	return s.az
}

func (s *Ec2Info) GetLocalRegion() string {
	return s.region
}

func (s *Ec2Info) GetLocalInstanceID() string {
	return s.instanceID
}

func (s *Ec2Info) GetLocalVpcID() string {
	return s.vpcID
}
