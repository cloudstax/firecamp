package manageservice

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"

	"github.com/openconnectio/openmanage/common"
	"github.com/openconnectio/openmanage/db"
	"github.com/openconnectio/openmanage/dns"
	"github.com/openconnectio/openmanage/manage"
	"github.com/openconnectio/openmanage/server"
	"github.com/openconnectio/openmanage/utils"
)

// Service creation requires 3 steps
// 1. decide the cluster and service name, the number of replicas for the service,
//    volume size and available zone.
// 2. call our script to create the service with all service attributes.
// 3. create the task definition and service in ECS in the target cluster with
//    the same service name and replica number.

// Here is the implementation for step 2.
// - create device: check device items, assign the next block device name to the service,
//	 and create the device item.
//	 If there are concurrent creation, may need to retry. Should also check whether the
//   service already has the device item, this is possible as failure could happen at any time.
// - create the service item in DynamoDB
// - create the service attribute record with CREATING state.
// - create the desired number of volumes, and create IDLE volume items in DB.
// - update the service attribute state to ACTIVE.
// The failure handling is simple. If the script failed at some step, user will get error and retry.
// Could have a background scanner to clean up the dead service, in case user didn't retry.

const (
	maxRetryCount    = 3
	defaultSleepTime = 2 * time.Second
)

var errServiceHasDevice = errors.New("device is already assigned to service")

// SCService implements the service creation detail
type SCService struct {
	dbIns     db.DB
	serverIns server.Server
	dnsIns    dns.DNS
}

// NewSCService allocates a SCService instance
func NewSCService(dbIns db.DB, serverIns server.Server, dnsIns dns.DNS) *SCService {
	return &SCService{
		dbIns:     dbIns,
		serverIns: serverIns,
		dnsIns:    dnsIns,
	}
}

// CreateService implements step 2.
// The cfgFileContents could either be one content for all replicas or one for each replica.
func (s *SCService) CreateService(req *manage.CreateServiceRequest,
	domainName string, vpcID string) (serviceUUID string, err error) {
	// check args
	if len(req.Service.Cluster) == 0 || len(req.Service.ServiceName) == 0 ||
		len(req.Service.Zone) == 0 || len(req.Service.Region) == 0 ||
		req.Replicas <= 0 || req.VolumeSizeGB <= 0 || len(vpcID) == 0 {
		glog.Errorln("invalid args", req.Service, "Replicas", req.Replicas, "VolumeSizeGB", req.VolumeSizeGB, "vpc", vpcID)
		return "", common.ErrInvalidArgs
	}
	if int64(len(req.ReplicaConfigs)) != req.Replicas {
		errmsg := fmt.Sprintf("invalid args, every replica should have a config file, replica number %d, config file number %d",
			len(req.ReplicaConfigs), req.Replicas)
		glog.Errorln(errmsg)
		return "", errors.New(errmsg)
	}

	// check and create system tables if necessary
	err = s.checkAndCreateSystemTables()
	if err != nil {
		glog.Errorln("checkAndCreateSystemTables error", err, req.Service)
		return "", err
	}

	hostedZoneID := ""
	if req.HasMembership {
		if len(domainName) == 0 {
			// use the default domainName
			domainName = dns.GenDefaultDomainName(req.Service.Cluster)
		}

		// check if the hosted zone exists
		private := true
		hostedZoneID, err = s.dnsIns.GetOrCreateHostedZoneIDByName(domainName, vpcID, req.Service.Region, private)
		if err != nil {
			glog.Errorln("GetOrCreateHostedZoneIDByName error", err, "domain", domainName, "vpc", vpcID, req.Service)
			return "", err
		}
		glog.Infoln("get hostedZoneID", hostedZoneID, "for domain", domainName, "vpc", vpcID, req.Service)
	}

	// create device item
	deviceName, err := s.createDevice(req.Service.Cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("createDevice error", err, req.Service)
		return "", err
	}

	glog.Warningln("assigned device", deviceName, "to service", req.Service.ServiceName)

	// create service item
	serviceUUID, err = s.createService(req.Service.Cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("createService error", err, req.Service)
		return "", err
	}

	// create service attr
	created, serviceAttr, err := s.checkAndCreateServiceAttr(serviceUUID, deviceName, domainName, hostedZoneID, req)
	if err != nil {
		glog.Errorln("checkAndCreateServiceAttr error", err, req.Service)
		return "", err
	}
	if created {
		glog.Infoln("service is already created", serviceAttr)
		return serviceAttr.ServiceUUID, nil
	}

	glog.Infoln("created service attr", serviceAttr)

	// create the desired number of volumes
	err = s.checkAndCreateServiceVolumes(serviceUUID, deviceName, req)
	if err != nil {
		glog.Errorln("checkAndCreateServiceVolumes failed", err, "service", serviceAttr)
		return "", err
	}

	// update service status to INITIALIZING
	newAttr := db.UpdateServiceAttr(serviceAttr, common.ServiceStatusInitializing)
	err = s.dbIns.UpdateServiceAttr(serviceAttr, newAttr)
	if err != nil {
		glog.Errorln("UpdateServiceAttr error", err, "service", newAttr)
		return "", err
	}

	glog.Warningln("successfully created service", newAttr)
	return serviceUUID, nil
}

// SetServiceInitialized updates the service status from INITIALIZING to ACTIVE
func (s *SCService) SetServiceInitialized(cluster string, servicename string) error {
	svc, err := s.dbIns.GetService(cluster, servicename)
	if err != nil {
		glog.Errorln("GetService error", err, servicename)
		return err
	}

	sattr, err := s.dbIns.GetServiceAttr(svc.ServiceUUID)
	if err != nil || sattr == nil {
		glog.Errorln("GetServiceAttr failed", err, "service attr", sattr)
		return err
	}

	switch sattr.ServiceStatus {
	case common.ServiceStatusCreating:
		glog.Errorln("service is at the creating status", sattr)
		return db.ErrDBConditionalCheckFailed

	case common.ServiceStatusInitializing:
		newAttr := db.UpdateServiceAttr(sattr, common.ServiceStatusActive)
		err = s.dbIns.UpdateServiceAttr(sattr, newAttr)
		if err != nil {
			glog.Errorln("UpdateServiceAttr error", err, "service", newAttr)
			return err
		}
		glog.Infoln("set service initialized", newAttr)
		return nil

	case common.ServiceStatusActive:
		glog.Infoln("service already initialized", sattr)
		return nil

	case common.ServiceStatusDeleting:
		glog.Errorln("service is at the deleting status", sattr)
		return db.ErrDBConditionalCheckFailed

	case common.ServiceStatusDeleted:
		glog.Errorln("service is at the deleted status", sattr)
		return db.ErrDBConditionalCheckFailed

	default:
		glog.Errorln("service is at an unknown status", sattr)
		return db.ErrDBInternal
	}
}

// ListVolumes return all volumes of the service. DeleteService will not delete the
// actual volume in cloud, such as EBS. Customer should ListVolumes before DeleteService,
// and delete the actual volume manually.
func (s *SCService) ListVolumes(cluster string, service string) (volumes []string, err error) {
	// get service
	sitem, err := s.dbIns.GetService(cluster, service)
	if err != nil {
		glog.Errorln("DeleteService error", err, "service", service, "cluster", cluster)
		return nil, err
	}

	// get service volumes
	vols, err := s.dbIns.ListVolumes(sitem.ServiceUUID)
	if err != nil {
		glog.Errorln("ListVolumes error", err, "service", sitem)
		return nil, err
	}

	volumes = make([]string, len(vols))
	for i, v := range vols {
		volumes[i] = v.VolumeID
	}

	glog.Infoln("cluster", cluster, "service", service, "has volumes", volumes)
	return volumes, nil
}

// DeleteVolume actually deletes one volume
func (s *SCService) DeleteVolume(volID string) error {
	return s.serverIns.DeleteVolume(volID)
}

// GetServiceUUID gets the serviceUUID
func (s *SCService) GetServiceUUID(cluster string, service string) (serviceUUID string, err error) {
	// get service
	sitem, err := s.dbIns.GetService(cluster, service)
	if err != nil {
		glog.Errorln("GetService error", err, "service", service, "cluster", cluster)
		return "", err
	}
	return sitem.ServiceUUID, nil
}

// ListClusters lists all clusters in the system
func (s *SCService) ListClusters() ([]string, error) {
	return nil, nil
}

// ListServices lists all services of the cluster
func (s *SCService) ListServices(cluster string) (svcs []*common.Service, err error) {
	svcs, err = s.dbIns.ListServices(cluster)
	if err != nil {
		glog.Errorln("ListServices error", err, "cluster", cluster)
		return nil, err
	}

	glog.Infoln("list", len(svcs), "services, cluster", cluster)
	return svcs, nil
}

// DeleteService deletes the service record from db.
// Notes:
//   - caller should check whether service is really stopped in ECS.
//   - the actual cloud volumes of this service are not deleted, customer needs
//     to delete them manually.
func (s *SCService) DeleteService(cluster string, service string) error {
	// get service
	sitem, err := s.dbIns.GetService(cluster, service)
	if err != nil {
		glog.Errorln("GetService error", err, "service", service, "cluster", cluster)
		return err
	}

	// get service attr
	sattr, err := s.dbIns.GetServiceAttr(sitem.ServiceUUID)
	if err != nil {
		if err == db.ErrDBRecordNotFound {
			glog.Infoln("service attr not found, delete service item", sitem)
			err = s.dbIns.DeleteService(cluster, service)
			return err
		}
		glog.Errorln("GetServiceAttr error", err, "service", sitem)
		return err
	}

	if sattr.ServiceStatus != common.ServiceStatusDeleting {
		// set service status to deleting
		newAttr := db.UpdateServiceAttr(sattr, common.ServiceStatusDeleting)
		err = s.dbIns.UpdateServiceAttr(sattr, newAttr)
		if err != nil {
			glog.Errorln("set service deleting status error", err, "service attr", sattr)
			return err
		}

		sattr = newAttr
		glog.Infoln("set service to deleting status", sattr)
	} else {
		glog.Infoln("service is already at deleting status", sattr)
	}

	// get service volumes
	vols, err := s.dbIns.ListVolumes(sattr.ServiceUUID)
	if err != nil {
		glog.Errorln("ListVolumes error", err, "service attr", sattr)
		return err
	}

	// delete the config files
	glog.Infoln("deleting the config files from DB, service attr", sattr)
	for _, v := range vols {
		for _, c := range v.Configs {
			err := s.dbIns.DeleteConfigFile(v.ServiceUUID, c.FileID)
			if err != nil && err != db.ErrDBRecordNotFound {
				glog.Errorln("DeleteConfigFile error", err, "config", c, "volume", v)
				return err
			}
			glog.V(1).Infoln("deleted config file", c.FileID, v.ServiceUUID)
		}
	}

	// delete all volume records in DB
	glog.Infoln("deleting volumes from DB, service attr", sattr, "volumes", len(vols), vols)
	for _, v := range vols {
		err := s.dbIns.DeleteVolume(v.ServiceUUID, v.VolumeID)
		if err != nil && err != db.ErrDBRecordNotFound {
			glog.Errorln("DeleteVolume error", err, v)
			return err
		}
		glog.V(1).Infoln("deleted volume", v)
	}
	glog.Infoln("deleted", len(vols), "volumes from DB, service attr", sattr)

	// delete the device
	err = s.dbIns.DeleteDevice(cluster, sattr.DeviceName)
	if err != nil && err != db.ErrDBRecordNotFound {
		glog.Errorln("DeleteDevice error", err, sattr)
		return err
	}
	glog.Infoln("deleted device", sattr)

	// delete service attr
	err = s.dbIns.DeleteServiceAttr(sattr.ServiceUUID)
	if err != nil && err != db.ErrDBRecordNotFound {
		glog.Errorln("DeleteServiceAttr error", err, sattr)
		return err
	}
	glog.Infoln("deleted service attr", sattr)

	// delete service
	err = s.dbIns.DeleteService(cluster, service)
	if err != nil && err != db.ErrDBRecordNotFound {
		glog.Errorln("DeleteService error", err, "service", service, "cluster", cluster)
		return err
	}

	glog.Infoln("delete service complete, service", service, "cluster", cluster)
	return nil
}

// DeleteSystemTables deletes all system tables.
// User has to delete all services first.
func (s *SCService) DeleteSystemTables() error {
	// TODO check if any service is still in system.
	return s.dbIns.DeleteSystemTables()
}

func (s *SCService) checkAndCreateSystemTables() error {
	// get system table status
	status, ready, err := s.dbIns.SystemTablesReady()
	if err != nil {
		if err != db.ErrDBTableNotFound {
			glog.Errorln("SystemTablesReady check failed", err, "ready", ready, "status", status)
			return err
		}

		// table not exist, create system tables
		err = s.dbIns.CreateSystemTables()
		if err != nil {
			glog.Errorln("CreateSystemTables failed", err)
			return err
		}
	} else if ready {
		// err == nil and ready
		glog.Infoln("SystemTables are ready, status", status)
		return nil
	}

	glog.Infoln("SystemTables are not ready", ready, "status", status)

	// wait till all tables are ready
	for i := 0; i < maxRetryCount; i++ {
		time.Sleep(defaultSleepTime)

		status, ready, err = s.dbIns.SystemTablesReady()
		if err != nil {
			glog.Errorln("SystemTablesReady check failed", err, "ready", ready, "status", status)
			return err
		}
		if ready {
			break
		}
		glog.Warningln("SystemTables are not ready, wait")
	}

	if !ready {
		glog.Errorln("SystemTablesReady is still not ready after",
			maxRetryCount*defaultSleepTime, "seconds")
		return common.ErrSystemCreating
	}

	glog.Infoln("SystemTables are ready, status", status)
	return nil
}

// lists device items, assigns the next block device name to the service,
// and creates the device item in DB.
func (s *SCService) createDevice(cluster string, service string) (devName string, err error) {
	for i := 0; i < maxRetryCount; i++ {
		// assign the device name
		devName, err := s.assignDeviceName(cluster, service)
		if err != nil {
			if err == errServiceHasDevice && len(devName) != 0 {
				glog.Infoln("device", devName, "is already assigned to service",
					service, "cluster", cluster)
				return devName, nil
			}

			glog.Errorln("AssignDeviceName failed, cluster", cluster,
				"service", service, "error", err)
			return "", err
		}

		// create Device in DB
		dev := db.CreateDevice(cluster, devName, service)
		err = s.dbIns.CreateDevice(dev)
		if err != nil {
			if err == db.ErrDBConditionalCheckFailed {
				glog.Errorln("retry, someone else created the device first", dev)
				continue
			}

			glog.Errorln("failed to create device", dev, "error", err)
			return "", err
		}

		return devName, nil
	}

	glog.Errorln("failed to create device after retry", maxRetryCount,
		"times, cluster", cluster, "service", service)
	return "", common.ErrInternal
}

func (s *SCService) createService(cluster string, service string) (serviceUUID string, err error) {
	// generate service uuid
	serviceUUID = utils.GenUUID()

	svc := db.CreateService(cluster, service, serviceUUID)
	err = s.dbIns.CreateService(svc)
	if err == nil {
		glog.Infoln("created service", svc)
		return serviceUUID, nil
	}

	if err != db.ErrDBConditionalCheckFailed {
		// err != nil && not service exist
		glog.Errorln("CreateService error", err, "service", service, "cluster", cluster)
		return "", err
	}

	// service exists, get it and return the uuid
	currItem, err := s.dbIns.GetService(cluster, service)
	if err != nil {
		glog.Errorln("GetService error", err, "service", service, "cluster", cluster)
		return "", err
	}

	glog.Infoln("service exists", currItem)
	return currItem.ServiceUUID, nil
}

func (s *SCService) assignDeviceName(cluster string, service string) (devName string, err error) {
	// list current device items
	devs, err := s.dbIns.ListDevices(cluster)
	if err != nil {
		glog.Errorln("DB ListDevices failed, cluster", cluster, "error", err)
		return "", err
	}

	firstDevice := s.serverIns.GetFirstDeviceName()

	if len(devs) == 0 {
		glog.Infoln("assign first device", firstDevice, "cluster", cluster)
		return firstDevice, nil
	}

	// get the last device
	// TODO support recycle device in the middle, for example, xvdf~p are used,
	//			then service for xvdg is deleted.
	lastDev := firstDevice
	for _, x := range devs {
		// The service creation involves multiple steps, and may fail at any step.
		// The customer may retry the creation. Check if device is already assigned
		// to one service.
		if x.ServiceName == service {
			glog.Infoln("device is already assigned to service", x)
			return x.DeviceName, errServiceHasDevice
		}

		dev := x.DeviceName

		glog.V(1).Infoln("cluster", cluster, "service", service,
			"lastDev", lastDev, "current dev", dev)

		if len(dev) > len(lastDev) {
			// example, dev=/dev/xvdba and lastDev=/dev/xvda
			lastDev = dev
		} else if len(dev) == len(lastDev) {
			// check if dev > lastDev
			if strings.Compare(dev, lastDev) > 0 {
				lastDev = dev
			}
			// dev <= lastDev, continue
		}
		// len(dev) < len(lastDev), continue
	}

	glog.Infoln("cluster", cluster, "service", service, "lastDev", lastDev, len(devs))

	// get the next device name
	return s.serverIns.GetNextDeviceName(lastDev)
}

func (s *SCService) checkAndCreateServiceAttr(serviceUUID string, deviceName string,
	domainName string, hostedZoneID string, req *manage.CreateServiceRequest) (serviceCreated bool, sattr *common.ServiceAttr, err error) {
	// create service attr
	serviceAttr := db.CreateInitialServiceAttr(serviceUUID, req.Replicas, req.VolumeSizeGB,
		req.Service.Cluster, req.Service.ServiceName, deviceName, req.HasMembership, domainName, hostedZoneID)
	err = s.dbIns.CreateServiceAttr(serviceAttr)
	if err == nil {
		glog.Infoln("created service attr in db", serviceAttr)
		return false, serviceAttr, err
	}

	// CreateServiceAttr failed
	if err != db.ErrDBConditionalCheckFailed {
		glog.Errorln("CreateServiceAttr failed", err, "service attr", serviceAttr)
		return false, nil, err
	}

	// service attr exists in db
	sattr, err = s.dbIns.GetServiceAttr(serviceUUID)
	if err != nil || sattr == nil {
		glog.Errorln("GetServiceAttr failed", err, "service attr", serviceAttr)
		return false, nil, err
	}

	glog.Infoln("get existing service attr", sattr)

	// check if it is a retry request. If not, return error
	skipMtime := true
	serviceAttr.ServiceStatus = sattr.ServiceStatus
	if !db.EqualServiceAttr(serviceAttr, sattr, skipMtime) {
		glog.Errorln("service exists, old service", sattr, "new", serviceAttr)
		return false, sattr, common.ErrServiceExist
	}

	// a retry request, check the service status
	switch sattr.ServiceStatus {
	case common.ServiceStatusCreating:
		// service is at creating status, continue the creation
		glog.V(0).Infoln("service is at the creating status", sattr)
		return false, sattr, nil

	case common.ServiceStatusInitializing:
		glog.V(0).Infoln("service already created, at the initializing status", sattr)
		return true, sattr, nil

	case common.ServiceStatusActive:
		glog.V(0).Infoln("service already created, at the active status", sattr)
		return true, sattr, nil

	case common.ServiceStatusDeleting:
		glog.Errorln("service is under deleting", sattr)
		return true, sattr, common.ErrServiceDeleting

	case common.ServiceStatusDeleted:
		glog.Errorln("service is deleted", sattr)
		return true, sattr, common.ErrServiceDeleted

	default:
		glog.Errorln("service is at unknown status", sattr)
		return false, nil, common.ErrInternal
	}
}

func (s *SCService) checkAndCreateServiceVolumes(serviceUUID string, devName string,
	req *manage.CreateServiceRequest) error {

	// list to find out how many volumes were already created
	vols, err := s.dbIns.ListVolumes(serviceUUID)
	if err != nil {
		glog.Errorln("ListVolumes failed", err, "serviceUUID", serviceUUID)
		return err
	}

	// TODO check the config in the existing volumes.
	// if config file does not match, return error. The customer should not do like:
	// 1) create the service, but failed as the node crashes
	// 2) the customer retries the creation with the different config.
	// Currently we don't support this case, as some volumes may have been created with the
	// old config. The retry will not recreate those volumes.
	// If the customer wants to retry with the different config, should delete the current service first.

	allVols := make([]*common.Volume, req.Replicas)
	copy(allVols, vols)

	for i := int64(len(vols)); i < req.Replicas; i++ {
		memberName := ""
		if req.HasMembership {
			memberName = utils.GenServiceMemberName(req.Service.ServiceName, i)
		}

		// check and create the config file first
		replicaCfg := req.ReplicaConfigs[i]
		cfgs, err := s.checkAndCreateConfigFile(serviceUUID, memberName, replicaCfg)
		if err != nil {
			glog.Errorln("checkAndCreateConfigFile error", err, "service", serviceUUID, "member", memberName)
			return err
		}

		vol, err := s.createServiceVolume(serviceUUID, req.VolumeSizeGB, req.Service.Zone, devName, memberName, cfgs)
		if err != nil {
			glog.Errorln("create volume failed, serviceUUID", serviceUUID, "member", memberName,
				"az", req.Service.Zone, "error", err)
			return err
		}
		allVols[i] = vol
	}

	glog.Infoln("created", req.Replicas-int64(len(vols)), "volumes for serviceUUID", serviceUUID, req.Service)

	// EBS volume creation is async in the background. volume state will be creating,
	// then available. block waiting here, as EBS volume creation is pretty fast,
	// usually 3 seconds. see ec2_test.go output.
	for _, vol := range allVols {
		err = s.serverIns.WaitVolumeCreated(vol.VolumeID)
		if err != nil {
			glog.Errorln("WaitVolumeCreated error", err, "volume", vol)
			return err
		}
		glog.Warningln("created volume", vol.VolumeID, req.Service)
	}

	return nil
}

func (s *SCService) checkAndCreateConfigFile(serviceUUID string, memberName string,
	replicaCfg *manage.ReplicaConfig) ([]*common.MemberConfig, error) {
	// the first version of the config file
	version := int64(0)

	configs := make([]*common.MemberConfig, len(replicaCfg.Configs))
	for i, cfg := range replicaCfg.Configs {
		fileID := utils.GenServiceMemberConfigID(memberName, cfg.FileName, version)
		dbcfg, err := s.createConfigFile(serviceUUID, fileID, cfg)
		if err != nil {
			glog.Errorln("createConfigFile error", err, "fileID", fileID, "service", serviceUUID)
			return nil, err
		}
		configs[i] = &common.MemberConfig{FileName: cfg.FileName, FileID: fileID, FileMD5: dbcfg.FileMD5}
	}
	return configs, nil
}

func (s *SCService) createConfigFile(serviceUUID string, fileID string,
	cfg *manage.ReplicaConfigFile) (*common.ConfigFile, error) {
	dbcfg := db.CreateInitialConfigFile(serviceUUID, fileID, cfg.FileName, cfg.Content)
	err := s.dbIns.CreateConfigFile(dbcfg)
	if err == nil {
		glog.Infoln("created config file", fileID, "file name", cfg.FileName, "service", serviceUUID)
		return dbcfg, nil
	}

	if err != db.ErrDBConditionalCheckFailed {
		glog.Errorln("CreateConfigFile error", err, "fileID", fileID, "service", serviceUUID)
		return nil, err
	}

	// config file exists, check whether it is a retry request.
	currcfg, err := s.dbIns.GetConfigFile(serviceUUID, fileID)
	if err != nil {
		glog.Errorln("get existing config file error", err, "fileID", fileID, "service", serviceUUID)
		return nil, err
	}

	skipMtime := true
	skipContent := true
	if !db.EqualConfigFile(currcfg, dbcfg, skipMtime, skipContent) {
		glog.Errorln("config file not match, current", db.PrintConfigFile(currcfg), "new", db.PrintConfigFile(dbcfg))
		return nil, common.ErrConfigMismatch
	}

	glog.Infoln("config file exists", db.PrintConfigFile(currcfg))
	return currcfg, nil
}

func (s *SCService) createServiceVolume(serviceUUID string, volSize int64, az string,
	devName string, memberName string, cfgs []*common.MemberConfig) (vol *common.Volume, err error) {
	volID, err := s.serverIns.CreateVolume(az, volSize)
	if err != nil {
		glog.Errorln("CreateVolume failed, volID", volID,
			"serviceUUID", serviceUUID, "az", az, "error", err)
		return nil, err
	}

	vol = db.CreateInitialVolume(serviceUUID, volID, devName, az, memberName, cfgs)
	err = s.dbIns.CreateVolume(vol)
	if err != nil {
		glog.Errorln("CreateVolume in DB failed, Volume", vol, "error", err)
		return nil, err
	}

	glog.Infoln("created volume", vol)
	return vol, nil
}
