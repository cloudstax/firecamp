package db

import (
	"github.com/openconnectio/openmanage/common"
)

// DB defines the DB interfaces
//
// The design aims to provide the flexibility to support different type of key-value DBs.
// For example, could use the simple embedded controlDB, DynamoDB, etcd, zk, etc.
// There are 2 requirements:
// 1) conditional creation/update (create-if-not-exist and update-if-match).
// 2) strong consistency on get/list.
//
// The device/service/serviceattr/volume creations are create-if-not-exist.
// If item exists in DB, the ErrDBConditionalCheckFailed error will be returned.
//
// The serviceattr/volume updates are also update-if-match the old item.
// Return ErrDBConditionalCheckFailed as well if not match.
type DB interface {
	CreateSystemTables() error
	SystemTablesReady() (tableStatus string, ready bool, err error)
	DeleteSystemTables() error

	CreateDevice(dev *common.Device) error
	GetDevice(clusterName string, deviceName string) (dev *common.Device, err error)
	DeleteDevice(clusterName string, deviceName string) error
	ListDevices(clusterName string) (devs []*common.Device, err error)

	CreateService(svc *common.Service) error
	GetService(clusterName string, serviceName string) (svc *common.Service, err error)
	DeleteService(clusterName string, serviceName string) error
	ListServices(clusterName string) (svcs []*common.Service, err error)

	CreateServiceAttr(attr *common.ServiceAttr) error
	UpdateServiceAttr(oldAttr *common.ServiceAttr, newAttr *common.ServiceAttr) error
	GetServiceAttr(serviceUUID string) (attr *common.ServiceAttr, err error)
	DeleteServiceAttr(serviceUUID string) error

	CreateVolume(vol *common.Volume) error
	UpdateVolume(oldVol *common.Volume, newVol *common.Volume) error
	GetVolume(serviceUUID string, volumeID string) (vol *common.Volume, err error)
	ListVolumes(serviceUUID string) (vols []*common.Volume, err error)
	DeleteVolume(serviceUUID string, volumeID string) error

	CreateConfigFile(cfg *common.ConfigFile) error
	GetConfigFile(serviceUUID string, fileID string) (cfg *common.ConfigFile, err error)
	DeleteConfigFile(serviceUUID string, fileID string) error
}
