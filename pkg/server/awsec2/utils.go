package awsec2

import (
	"io/ioutil"
	"net/http"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/golang/glog"
)

const ec2MDv2Token = "http://169.254.169.254/latest/api/token"
const ec2InstanceIDURL = "http://169.254.169.254/latest/meta-data/instance-id"
const ec2AZURL = "http://169.254.169.254/latest/meta-data/placement/availability-zone"
const ec2HostnameURL = "http://169.254.169.254/latest/meta-data/hostname"
const ec2LocalIPURL = "http://169.254.169.254/latest/meta-data/local-ipv4"
const ec2MacURL = "http://169.254.169.254/latest/meta-data/mac"
const ec2VpcURLPrefix = "http://169.254.169.254/latest/meta-data/network/interfaces/macs/"

// GetLocalEc2InstanceID gets the current ec2 node's instanceID
func GetLocalEc2InstanceID() (string, error) {
	return getEc2Metadata(ec2InstanceIDURL)
}

// GetLocalEc2AZ gets the current ec2 node's availability-zone
func GetLocalEc2AZ() (string, error) {
	az, err := getEc2Metadata(ec2AZURL)
	if err != nil || len(az) == 0 {
		glog.Errorln("get region error", err, "az", az)
		return "", err
	}
	return az, nil
}

// GetLocalEc2Region gets the current ec2 node's region
func GetLocalEc2Region() (string, error) {
	az, err := getEc2Metadata(ec2AZURL)
	if err != nil || len(az) == 0 {
		glog.Errorln("get region error", err, "az", az)
		return "", err
	}

	// az must be like us-west-1c, get region from it
	region := az[:len(az)-1]
	glog.Infoln("get ec2 region", region, "az", az)
	return region, nil
}

// GetLocalEc2Hostname gets the current ec2 node's hostname
func GetLocalEc2Hostname() (string, error) {
	return getEc2Metadata(ec2HostnameURL)
}

// GetLocalEc2IP gets the current ec2 node's local IP
func GetLocalEc2IP() (string, error) {
	return getEc2Metadata(ec2LocalIPURL)
}

// GetLocalEc2VpcID gets the current ec2 node's vpc id
func GetLocalEc2VpcID() (string, error) {
	mac, err := getEc2Metadata(ec2MacURL)
	if err != nil {
		glog.Errorln("get mac error", err)
		return "", err
	}

	vpcURL := ec2VpcURLPrefix + mac + "/vpc-id"
	vpcID, err := getEc2Metadata(vpcURL)
	if err != nil {
		glog.Errorln("get vpc id error", err)
		return "", err
	}
	return vpcID, nil
}

func getEc2Metadata(url string) (string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("PUT", ec2MDv2Token, nil)
	if err != nil {
		glog.Errorln("NewRequest", ec2MDv2Token, "error", err)
		return "", err
	}
	req.Header.Add("X-aws-ec2-metadata-token-ttl-seconds", "60")
	resp, err := client.Do(req)
	if err != nil {
		glog.Errorln("get metadata token", ec2MDv2Token, "error", err)
		return "", err
	}
	defer resp.Body.Close()

	token, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		glog.Errorln("read metadata token error", err, "url", ec2MDv2Token)
		return "", err
	}

	client = &http.Client{}
	req, err = http.NewRequest("GET", url, nil)
	if err != nil {
		glog.Errorln("NewRequest", url, "error", err)
		return "", err
	}
	req.Header.Add("X-aws-ec2-metadata-token", string(token))
	resp, err = client.Do(req)
	if err != nil {
		glog.Errorln("get url", url, "error", err)
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		glog.Errorln("read body error", err, "url", url)
		return "", err
	}

	return string(body), nil
}

// GetRegionAZs gets the AvailabilityZones in one region
func GetRegionAZs(region string, sess *session.Session) ([]string, error) {
	svc := ec2.New(sess)
	// describe AvailabilityZones in the region
	params := &ec2.DescribeAvailabilityZonesInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String(regionFilterName),
				Values: []*string{
					aws.String(region),
				},
			},
		},
	}
	res, err := svc.DescribeAvailabilityZones(params)
	if err != nil {
		glog.Errorln("DescribeAvailabilityZones error", err)
		return nil, err
	}
	if len(res.AvailabilityZones) == 0 {
		glog.Errorln("No AvailabilityZones in region", region)
		return nil, common.ErrInternal
	}
	azs := make([]string, len(res.AvailabilityZones))
	for i, zone := range res.AvailabilityZones {
		azs[i] = *(zone.ZoneName)
	}
	return azs, nil
}
