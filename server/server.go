package server

import (
	"errors"

	"golang.org/x/net/context"
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

	VolumeTypeHDD     = "standard"
	VolumeTypeGPSSD   = "gp2"
	VolumeTypeIOPSSSD = "io1"
)

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

// Server defines the volume and device related operations for one host such as EC2
type Server interface {
	AttachVolume(ctx context.Context, volID string, instanceID string, devName string) error
	WaitVolumeAttached(ctx context.Context, volID string) error
	GetVolumeState(ctx context.Context, volID string) (state string, err error)
	GetVolumeInfo(ctx context.Context, volID string) (info VolumeInfo, err error)
	DetachVolume(ctx context.Context, volID string, instanceID string, devName string) error
	WaitVolumeDetached(ctx context.Context, volID string) error
	CreateVolume(ctx context.Context, az string, volSizeGB int64) (volID string, err error)
	WaitVolumeCreated(ctx context.Context, volID string) error
	DeleteVolume(ctx context.Context, volID string) error

	GetControlDBDeviceName() string
	GetFirstDeviceName() string
	GetNextDeviceName(lastDev string) (devName string, err error)
	// TODO some services may be deleted, should reuse the device name
}

// Info defines the operations for the local server related info
type Info interface {
	GetLocalHostname() string
	GetLocalAvailabilityZone() string
	GetLocalRegion() string
	GetLocalInstanceID() string
	GetLocalVpcID() string
	GetLocalRegionAZs() []string
}
