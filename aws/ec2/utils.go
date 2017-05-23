package awsec2

import (
	"io/ioutil"
	"net/http"

	"github.com/golang/glog"
)

const ec2InstanceIDURL = "http://169.254.169.254/latest/meta-data/instance-id"
const ec2AZURL = "http://169.254.169.254/latest/meta-data/placement/availability-zone"
const ec2HostnameURL = "http://169.254.169.254/latest/meta-data/hostname"
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
	resp, err := http.Get(url)
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
