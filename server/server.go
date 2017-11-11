package server

import (
	"errors"

	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
)

var (
	ErrVolumeIncorrectState = errors.New("IncorrectState")
	ErrVolumeInUse          = errors.New("VolumeInUse")
)

// http://docs.aws.amazon.com/cli/latest/reference/ec2/describe-volumes.html
// EBS volume has 2 states,
// The status of the volume (creating | available | in-use | deleting | deleted | error).
// The attachment state (attaching | attached | detaching | detached).
// The attachment state would be returned when volume status is in-use.
// Q: when will the attachment state be detached? After detached, volume status should be available.
const (
	VolumeStateCreating  = "creating"
	VolumeStateAvailable = "available"
	VolumeStateInUse     = "in-use"
	VolumeStateDetaching = "detaching"
	VolumeStateAttaching = "attaching"
	VolumeStateDeleting  = "deleting"
	VolumeStateDeleted   = "deleted"
	VolumeStateError     = "error"
)

// CreateVolumeOptions includes the creation parameters for the volume.
type CreateVolumeOptions struct {
	AvailabilityZone string
	VolumeType       string
	IOPS             int64
	VolumeSizeGB     int64
	TagSpecs         []common.KeyValuePair
}

// VolumeInfo records the volume's information.
type VolumeInfo struct {
	VolID string
	State string
	// the instance that the volume is attached to.
	AttachInstanceID string
	// the device that the volume is attached to.
	Device string
	Size   int64
}

type NetworkInterface struct {
	InterfaceID      string
	ServerInstanceID string
	PrimaryPrivateIP string
	PrivateIPs       []string
}

// Server defines the volume and device related operations for one host such as EC2
type Server interface {
	AttachVolume(ctx context.Context, volID string, instanceID string, devName string) error
	WaitVolumeAttached(ctx context.Context, volID string) error
	GetVolumeState(ctx context.Context, volID string) (state string, err error)
	GetVolumeInfo(ctx context.Context, volID string) (info VolumeInfo, err error)
	DetachVolume(ctx context.Context, volID string, instanceID string, devName string) error
	WaitVolumeDetached(ctx context.Context, volID string) error
	CreateVolume(ctx context.Context, opts *CreateVolumeOptions) (volID string, err error)
	WaitVolumeCreated(ctx context.Context, volID string) error
	DeleteVolume(ctx context.Context, volID string) error

	GetControlDBDeviceName() string
	GetFirstDeviceName() string
	GetNextDeviceName(lastDev string) (devName string, err error)

	// GetNetworkInterfaces returns the network interfaces in the firecamp cluster, vpc and zone,
	// and the secondary IPs of the network interfaces.
	// Every instance in the firecamp cluster will have the specific cluster tag, as we don't want
	// to get the application servers run on the same vpc and zone.
	GetNetworkInterfaces(ctx context.Context, cluster string, vpcID string, zone string) (netInterfaces []*NetworkInterface, cidrBlock string, err error)
	// The firecamp cluster instance has only one network interface.
	GetInstanceNetworkInterface(ctx context.Context, instanceID string) (netInterface *NetworkInterface, err error)
	AssignStaticIP(ctx context.Context, networkInterfaceID string, staticIP string) error
	// UnassignStaticIP unassigns the staticIP from the networkInterface. If the networkInterface does not
	// own the ip, should return success.
	UnassignStaticIP(ctx context.Context, networkInterfaceID string, staticIP string) error
}

// Info defines the operations for the local server related info
type Info interface {
	GetPrivateIP() string
	GetLocalAvailabilityZone() string
	GetLocalRegion() string
	GetLocalInstanceID() string
	GetLocalVpcID() string
	GetLocalRegionAZs() []string
}
