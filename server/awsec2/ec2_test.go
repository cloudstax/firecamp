package awsec2

import (
	"net"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/server"
	"github.com/cloudstax/firecamp/utils"
)

var e *AWSEc2
var cluster string
var az1aIns1 string
var az1aIns2 string
var az1cIns1 string
var az1aVol1 string

func TestAWSEC2(t *testing.T) {
	cfg := aws.NewConfig().WithRegion("us-west-1")
	sess, err := session.NewSession(cfg)
	if err != nil {
		t.Fatal("failed to create session, error", err)
	}

	e = NewAWSEc2(sess)
	ctx := context.Background()

	cluster = "test" + utils.GenUUID()

	imageID := "ami-327f5352"

	defer cleanup()

	// launch 3 instances at 2 az and create one volume
	az1aIns1, err = e.LaunchOneInstance(ctx, imageID, "us-west-1a", cluster)
	if err != nil {
		t.Fatal("failed to launch instance at us-west-1a, error", err)
	}

	az1aIns2, err = e.LaunchOneInstance(ctx, imageID, "us-west-1a", cluster)
	if err != nil {
		t.Fatal("failed to launch instance at us-west-1a, error", err)
	}

	az1cIns1, err = e.LaunchOneInstance(ctx, imageID, "us-west-1c", cluster)
	if err != nil {
		t.Fatal("failed to launch instance at us-west-1c, error", err)
	}

	// create one volume
	opts := &server.CreateVolumeOptions{
		AvailabilityZone: "us-west-1a",
		VolumeType:       common.VolumeTypeGPSSD,
		VolumeSizeGB:     1,
		TagSpecs: []common.KeyValuePair{
			common.KeyValuePair{
				Key:   "Name",
				Value: cluster,
			},
		},
	}
	az1aVol1, err = e.CreateVolume(ctx, opts)
	if err != nil {
		t.Fatal("failed to create volume at us-west-1a, error", err)
	}

	err = e.WaitVolumeCreated(ctx, az1aVol1)
	if err != nil {
		t.Fatal("WaitVolumeCreated error", err)
	}

	err = e.WaitInstanceRunning(ctx, az1aIns1)
	if err != nil {
		t.Fatal("WaitInstanceRunning error", err)
	}
	err = e.WaitInstanceRunning(ctx, az1aIns2)
	if err != nil {
		t.Fatal("WaitInstanceRunning error", err)
	}
	err = e.WaitInstanceRunning(ctx, az1cIns1)
	if err != nil {
		t.Fatal("WaitInstanceRunning error", err)
	}

	testEBSVolume(t)
	testEC2NetworkInterface(t)
}

func cleanup() {
	ctx := context.Background()

	// terminate the created instances and volume
	if len(az1aIns1) != 0 {
		e.TerminateInstance(ctx, az1aIns1)
	}
	if len(az1aIns2) != 0 {
		e.TerminateInstance(ctx, az1aIns2)
	}
	if len(az1cIns1) != 0 {
		e.TerminateInstance(ctx, az1cIns1)
	}
	if len(az1aVol1) != 0 {
		e.DeleteVolume(ctx, az1aVol1)
	}
}

func testEBSVolume(t *testing.T) {
	var err error
	devName := "/dev/xvdf"

	ctx := context.Background()

	// negative case - detach available volume
	err = e.DetachVolume(ctx, az1aVol1, az1aIns1, devName)
	if err != nil {
		t.Fatalf("detach available volume, expected success but got %s, vol %s instance %s", err, az1aVol1, az1aIns1)
	}

	// negative case - attach volume to instance at differnt az
	err = e.AttachVolume(ctx, az1aVol1, az1cIns1, devName)
	if err == nil {
		t.Fatalf("attach volume to instance at different az, expected fail but success, vol %s instance %s", az1aVol1, az1cIns1)
	}

	// attach volume
	err = e.AttachVolume(ctx, az1aVol1, az1aIns1, devName)
	if err != nil {
		t.Fatalf("attach volume, expected success but fail, vol %s instance %s", az1aVol1, az1aIns1)
	}
	err = e.WaitVolumeAttached(ctx, az1aVol1)
	if err != nil {
		t.Fatalf("wait volume in-use, error %s", err)
	}

	// negative case - detach volume from wrong instanceID
	err = e.DetachVolume(ctx, az1aVol1, az1aIns2, devName)
	if err == nil {
		t.Fatalf("detach volume from wrong instance, expected fail but success, vol %s instance %s", az1aVol1, az1aIns2)
	}

	// negative case - detach volume with wrong devName
	err = e.DetachVolume(ctx, az1aVol1, az1aIns1, "/dev/xvdx")
	if err == nil {
		t.Fatalf("detach volume with wrong devName, expected fail but success, vol %s instance %s devName %s", az1aVol1, az1aIns1, devName)
	}

	// detach volume
	err = e.DetachVolume(ctx, az1aVol1, az1aIns1, devName)
	if err != nil {
		t.Fatalf("detach volume, expected success but failed, vol %s instance %s", az1aVol1, az1aIns1)
	}

	err = e.WaitVolumeDetached(ctx, az1aVol1)
	if err != nil && err != common.ErrTimeout {
		t.Fatal("wait volume available, error %s", err)
	}
}

func TestEBSDeviceName(t *testing.T) {
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

func testEC2NetworkInterface(t *testing.T) {
	ctx := context.Background()

	instanceID := az1cIns1
	instance, err := e.DescribeInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("DescribeInstance %s error %s", instanceID, err)
	}

	vpcid := *(instance.VpcId)
	az := "us-west-1c"
	subnetID := *(instance.SubnetId)

	subnet, err := e.DescribeSubnet(ctx, subnetID)
	if err != nil {
		t.Fatalf("DescribeSubnet %s error %s", subnetID, err)
	}

	// test network interfaces
	netInterfaces, cidrBlock, err := e.GetNetworkInterfaces(ctx, cluster, vpcid, az)
	if err != nil {
		t.Fatalf("GetNetworkInterfaces error %s, cluster %s vpcid %s az %s", err, cluster, vpcid, az)
	}
	if len(netInterfaces) != 1 {
		t.Fatalf("expect 1 network interfaces, get %s", netInterfaces)
	}
	if len(netInterfaces[0].PrivateIPs) != 0 {
		t.Fatalf("expect 0 secondary ip, get %s", netInterfaces[0])
	}
	if cidrBlock != *(subnet.CidrBlock) {
		t.Fatalf("cidrBlock not match %s %s", cidrBlock, subnet.CidrBlock)
	}

	netInterface, err := e.GetInstanceNetworkInterface(ctx, instanceID)
	if err != nil {
		t.Fatalf("GetInstanceNetworkInterface error %s, instance %s", err, instanceID)
	}
	if netInterface.ServerInstanceID != instanceID {
		t.Fatalf("expect server instance id %s, get %s", instanceID, netInterface.ServerInstanceID)
	}
	if netInterface.InterfaceID != netInterfaces[0].InterfaceID ||
		netInterface.ServerInstanceID != netInterfaces[0].ServerInstanceID ||
		netInterface.PrimaryPrivateIP != netInterfaces[0].PrimaryPrivateIP ||
		len(netInterface.PrivateIPs) != len(netInterfaces[0].PrivateIPs) {
		t.Fatalf("interface not match, %s %s", netInterface, netInterfaces[0])
	}

	// test ip assign/unassign
	usedIPs := make(map[string]bool)
	usedIPs[netInterface.PrimaryPrivateIP] = true

	ip, ipnet, err := net.ParseCIDR(cidrBlock)
	if err != nil {
		t.Fatalf("ParseCIDR %s error %s", cidrBlock, err)
	}

	for i := 0; i < 3; i++ {
		ip, err = utils.GetNextIP(usedIPs, ipnet, ip)
		if err != nil {
			t.Fatalf("GetNextIP error %s, %s", err, ip)
		}

		ipstr := ip.String()
		usedIPs[ipstr] = true

		err = e.AssignStaticIP(ctx, netInterface.InterfaceID, ipstr)
		if err != nil {
			t.Fatalf("AssignStaticIP %s error %s, network interface %s", ip, err, netInterface)
		}

		netInterface, err = e.GetInstanceNetworkInterface(ctx, instanceID)
		if err != nil {
			t.Fatalf("GetInstanceNetworkInterface error %s, instance %s", err, instanceID)
		}
		if netInterface.ServerInstanceID != instanceID {
			t.Fatalf("expect server instance id %s, get %s", instanceID, netInterface.ServerInstanceID)
		}
		if len(netInterface.PrivateIPs) != 1 {
			t.Fatalf("expect 1 static ip, get %s", netInterface.PrivateIPs)
		}
		if netInterface.PrivateIPs[0] != ipstr {
			t.Fatalf("expect static ip %s, get %s", ipstr, netInterface.PrivateIPs)
		}

		err = e.UnassignStaticIP(ctx, netInterface.InterfaceID, ipstr)
		if err != nil {
			t.Fatalf("UnassignStaticIP %s error %s, %s", ipstr, err, netInterface)
		}

		// negative case: unassign ip that is not owned by network interface
		err = e.UnassignStaticIP(ctx, netInterface.InterfaceID, "10.0.0.1")
		if err != nil {
			t.Fatalf("unassign unexist ip, expect success, get error %s", err)
		}
	}

}
