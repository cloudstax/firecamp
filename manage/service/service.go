package manageservice

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

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

// ManageService implements the service operation details.
type ManageService struct {
	dbIns     db.DB
	serverIns server.Server
	dnsIns    dns.DNS
}

// NewManageService allocates a ManageService instance
func NewManageService(dbIns db.DB, serverIns server.Server, dnsIns dns.DNS) *ManageService {
	return &ManageService{
		dbIns:     dbIns,
		serverIns: serverIns,
		dnsIns:    dnsIns,
	}
}

// CreateService implements step 2.
// The cfgFileContents could either be one content for all replicas or one for each replica.
func (s *ManageService) CreateService(ctx context.Context, req *manage.CreateServiceRequest,
	domainName string, vpcID string) (serviceUUID string, err error) {
	// check args
	if len(req.Service.Cluster) == 0 || len(req.Service.ServiceName) == 0 ||
		len(req.Service.Region) == 0 || req.Replicas <= 0 || req.VolumeSizeGB <= 0 || len(vpcID) == 0 {
		glog.Errorln("invalid args", req.Service, "Replicas", req.Replicas, "VolumeSizeGB", req.VolumeSizeGB, "vpc", vpcID)
		return "", common.ErrInvalidArgs
	}
	if int64(len(req.ReplicaConfigs)) != req.Replicas {
		errmsg := fmt.Sprintf("invalid args, every replica should have a config file, replica number %d, config file number %d",
			len(req.ReplicaConfigs), req.Replicas)
		glog.Errorln(errmsg)
		return "", errors.New(errmsg)
	}

	requuid := utils.GetReqIDFromContext(ctx)

	// check and create system tables if necessary
	err = s.checkAndCreateSystemTables(ctx)
	if err != nil {
		glog.Errorln("checkAndCreateSystemTables error", err, req.Service, "requuid", requuid)
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
		hostedZoneID, err = s.dnsIns.GetOrCreateHostedZoneIDByName(ctx, domainName, vpcID, req.Service.Region, private)
		if err != nil {
			glog.Errorln("GetOrCreateHostedZoneIDByName error", err, "domain", domainName,
				"vpc", vpcID, "requuid", requuid, req.Service)
			return "", err
		}
		glog.Infoln("get hostedZoneID", hostedZoneID, "for domain", domainName,
			"vpc", vpcID, "requuid", requuid, req.Service)
	}

	// create device item
	deviceName, err := s.createDevice(ctx, req.Service.Cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("createDevice error", err, "requuid", requuid, req.Service)
		return "", err
	}

	glog.Infoln("assigned device", deviceName, "to service", req.Service.ServiceName, "requuid", requuid)

	// create service item
	serviceUUID, err = s.createService(ctx, req.Service.Cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("createService error", err, "requuid", requuid, req.Service)
		return "", err
	}

	// create service attr
	created, serviceAttr, err := s.checkAndCreateServiceAttr(ctx, serviceUUID, deviceName, domainName, hostedZoneID, req)
	if err != nil {
		glog.Errorln("checkAndCreateServiceAttr error", err, "requuid", requuid, req.Service)
		return "", err
	}
	if created {
		glog.Infoln("service is already created, requuid", requuid, serviceAttr)
		return serviceAttr.ServiceUUID, nil
	}

	glog.Infoln("created service attr, requuid", requuid, serviceAttr)

	// create the desired number of volumes
	err = s.checkAndCreateServiceVolumes(ctx, serviceUUID, deviceName, req)
	if err != nil {
		glog.Errorln("checkAndCreateServiceVolumes failed", err, "requuid", requuid, "service", serviceAttr)
		return "", err
	}

	// update service status to INITIALIZING
	newAttr := db.UpdateServiceAttr(serviceAttr, common.ServiceStatusInitializing)
	err = s.dbIns.UpdateServiceAttr(ctx, serviceAttr, newAttr)
	if err != nil {
		glog.Errorln("UpdateServiceAttr error", err, "requuid", requuid, "service", newAttr)
		return "", err
	}

	glog.Infoln("successfully created service, requuid", requuid, newAttr)
	return serviceUUID, nil
}

// SetServiceInitialized updates the service status from INITIALIZING to ACTIVE
func (s *ManageService) SetServiceInitialized(ctx context.Context, cluster string, servicename string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	svc, err := s.dbIns.GetService(ctx, cluster, servicename)
	if err != nil {
		glog.Errorln("GetService error", err, "requuid", requuid, servicename)
		return err
	}

	sattr, err := s.dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
	if err != nil || sattr == nil {
		glog.Errorln("GetServiceAttr failed", err, "requuid", requuid, "service attr", sattr)
		return err
	}

	switch sattr.ServiceStatus {
	case common.ServiceStatusCreating:
		glog.Errorln("service is at the creating status, requuid", requuid, sattr)
		return db.ErrDBConditionalCheckFailed

	case common.ServiceStatusInitializing:
		newAttr := db.UpdateServiceAttr(sattr, common.ServiceStatusActive)
		err = s.dbIns.UpdateServiceAttr(ctx, sattr, newAttr)
		if err != nil {
			glog.Errorln("UpdateServiceAttr error", err, "requuid", requuid, "service", newAttr)
			return err
		}
		glog.Infoln("set service initialized, requuid", requuid, newAttr)
		return nil

	case common.ServiceStatusActive:
		glog.Infoln("service already initialized, requuid", requuid, sattr)
		return nil

	case common.ServiceStatusDeleting:
		glog.Errorln("service is at the deleting status, requuid", requuid, sattr)
		return db.ErrDBConditionalCheckFailed

	case common.ServiceStatusDeleted:
		glog.Errorln("service is at the deleted status, requuid", requuid, sattr)
		return db.ErrDBConditionalCheckFailed

	default:
		glog.Errorln("service is at an unknown status, requuid", requuid, sattr)
		return db.ErrDBInternal
	}
}

// ListVolumes return all volumes of the service. DeleteService will not delete the
// actual volume in cloud, such as EBS. Customer should ListVolumes before DeleteService,
// and delete the actual volume manually.
func (s *ManageService) ListVolumes(ctx context.Context, cluster string, service string) (volumes []string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// get service
	sitem, err := s.dbIns.GetService(ctx, cluster, service)
	if err != nil {
		glog.Errorln("DeleteService error", err, "service", service, "cluster", cluster, "requuid", requuid)
		return nil, err
	}

	// get service volumes
	vols, err := s.dbIns.ListVolumes(ctx, sitem.ServiceUUID)
	if err != nil {
		glog.Errorln("ListVolumes error", err, "requuid", requuid, "service", sitem)
		return nil, err
	}

	volumes = make([]string, len(vols))
	for i, v := range vols {
		volumes[i] = v.VolumeID
	}

	glog.Infoln("cluster", cluster, "service", service, "has", len(volumes), "volumes, requuid", requuid)
	return volumes, nil
}

// DeleteVolume actually deletes one volume
func (s *ManageService) DeleteVolume(ctx context.Context, volID string) error {
	return s.serverIns.DeleteVolume(ctx, volID)
}

// GetServiceUUID gets the serviceUUID
func (s *ManageService) GetServiceUUID(ctx context.Context, cluster string, service string) (serviceUUID string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// get service
	sitem, err := s.dbIns.GetService(ctx, cluster, service)
	if err != nil {
		glog.Errorln("GetService error", err, "service", service, "cluster", cluster, "requuid", requuid)
		return "", err
	}
	return sitem.ServiceUUID, nil
}

// ListClusters lists all clusters in the system
func (s *ManageService) ListClusters(ctx context.Context) ([]string, error) {
	return nil, nil
}

// ListServices lists all services of the cluster
func (s *ManageService) ListServices(ctx context.Context, cluster string) (svcs []*common.Service, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	svcs, err = s.dbIns.ListServices(ctx, cluster)
	if err != nil {
		glog.Errorln("ListServices error", err, "cluster", cluster, "requuid", requuid)
		return nil, err
	}

	glog.Infoln("list", len(svcs), "services, cluster", cluster, "requuid", requuid)
	return svcs, nil
}

// DeleteService deletes the service record from db.
// Notes:
//   - caller should check whether service is really stopped in ECS.
//   - the actual cloud volumes of this service are not deleted, customer needs
//     to delete them manually.
func (s *ManageService) DeleteService(ctx context.Context, cluster string, service string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// get service
	sitem, err := s.dbIns.GetService(ctx, cluster, service)
	if err != nil {
		glog.Errorln("GetService error", err, "service", service, "cluster", cluster, "requuid", requuid)
		return err
	}

	// get service attr
	sattr, err := s.dbIns.GetServiceAttr(ctx, sitem.ServiceUUID)
	if err != nil {
		if err == db.ErrDBRecordNotFound {
			glog.Infoln("service attr not found, delete service item, requuid", requuid, sitem)
			err = s.dbIns.DeleteService(ctx, cluster, service)
			return err
		}
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, "service", sitem)
		return err
	}

	if sattr.ServiceStatus != common.ServiceStatusDeleting {
		// set service status to deleting
		newAttr := db.UpdateServiceAttr(sattr, common.ServiceStatusDeleting)
		err = s.dbIns.UpdateServiceAttr(ctx, sattr, newAttr)
		if err != nil {
			glog.Errorln("set service deleting status error", err, "requuid", requuid, "service attr", sattr)
			return err
		}

		sattr = newAttr
		glog.Infoln("set service to deleting status, requuid", requuid, sattr)
	} else {
		glog.Infoln("service is already at deleting status, requuid", requuid, sattr)
	}

	// get service volumes
	vols, err := s.dbIns.ListVolumes(ctx, sattr.ServiceUUID)
	if err != nil {
		glog.Errorln("ListVolumes error", err, "requuid", requuid, "service attr", sattr)
		return err
	}

	// delete the config files
	glog.Infoln("deleting the config files from DB, service attr, requuid", requuid, sattr)
	for _, v := range vols {
		for _, c := range v.Configs {
			err := s.dbIns.DeleteConfigFile(ctx, v.ServiceUUID, c.FileID)
			if err != nil && err != db.ErrDBRecordNotFound {
				glog.Errorln("DeleteConfigFile error", err, "requuid", requuid, "config", c, "volume", v)
				return err
			}
			glog.V(1).Infoln("deleted config file", c.FileID, v.ServiceUUID, "requuid", requuid)
		}
	}

	// delete all volume records in DB
	glog.Infoln("deleting volumes from DB, service attr", sattr, "volumes", len(vols), "requuid", requuid)
	for _, v := range vols {
		err := s.dbIns.DeleteVolume(ctx, v.ServiceUUID, v.VolumeID)
		if err != nil && err != db.ErrDBRecordNotFound {
			glog.Errorln("DeleteVolume error", err, "requuid", requuid, v)
			return err
		}
		glog.V(1).Infoln("deleted volume, requuid", requuid, v)
	}
	glog.Infoln("deleted", len(vols), "volumes from DB, service attr", sattr, "requuid", requuid)

	// delete the device
	err = s.dbIns.DeleteDevice(ctx, cluster, sattr.DeviceName)
	if err != nil && err != db.ErrDBRecordNotFound {
		glog.Errorln("DeleteDevice error", err, "requuid", requuid, sattr)
		return err
	}
	glog.Infoln("deleted device", sattr, "requuid", requuid)

	// delete service attr
	err = s.dbIns.DeleteServiceAttr(ctx, sattr.ServiceUUID)
	if err != nil && err != db.ErrDBRecordNotFound {
		glog.Errorln("DeleteServiceAttr error", err, sattr)
		return err
	}
	glog.Infoln("deleted service attr", sattr, "requuid", requuid)

	// delete service
	err = s.dbIns.DeleteService(ctx, cluster, service)
	if err != nil && err != db.ErrDBRecordNotFound {
		glog.Errorln("DeleteService error", err, "service", service, "cluster", cluster, "requuid", requuid)
		return err
	}

	glog.Infoln("delete service complete, service", service, "cluster", cluster, "requuid", requuid)
	return nil
}

// DeleteSystemTables deletes all system tables.
// User has to delete all services first.
func (s *ManageService) DeleteSystemTables(ctx context.Context) error {
	// TODO check if any service is still in system.
	return s.dbIns.DeleteSystemTables(ctx)
}

func (s *ManageService) checkAndCreateSystemTables(ctx context.Context) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// get system table status
	status, ready, err := s.dbIns.SystemTablesReady(ctx)
	if err != nil {
		if err != db.ErrDBTableNotFound {
			glog.Errorln("SystemTablesReady check failed", err, "ready", ready, "status", status, "requuid", requuid)
			return err
		}

		// table not exist, create system tables
		err = s.dbIns.CreateSystemTables(ctx)
		if err != nil {
			glog.Errorln("CreateSystemTables failed", err, "requuid", requuid)
			return err
		}
	} else if ready {
		// err == nil and ready
		glog.Infoln("SystemTables are ready, status", status, "requuid", requuid)
		return nil
	}

	glog.Infoln("SystemTables are not ready", ready, "status", status, "requuid", requuid)

	// wait till all tables are ready
	for i := 0; i < maxRetryCount; i++ {
		time.Sleep(defaultSleepTime)

		status, ready, err = s.dbIns.SystemTablesReady(ctx)
		if err != nil {
			glog.Errorln("SystemTablesReady check failed", err, "ready", ready, "status", status, "requuid", requuid)
			return err
		}
		if ready {
			break
		}
		glog.Infoln("SystemTables are not ready, wait, requuid", requuid)
	}

	if !ready {
		glog.Errorln("SystemTablesReady is still not ready after",
			maxRetryCount*defaultSleepTime, "seconds, requuid", requuid)
		return common.ErrSystemCreating
	}

	glog.Infoln("SystemTables are ready, status", status, "requuid", requuid)
	return nil
}

// lists device items, assigns the next block device name to the service,
// and creates the device item in DB.
func (s *ManageService) createDevice(ctx context.Context, cluster string, service string) (devName string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	for i := 0; i < maxRetryCount; i++ {
		// assign the device name
		devName, err := s.assignDeviceName(ctx, cluster, service)
		if err != nil {
			if err == errServiceHasDevice && len(devName) != 0 {
				glog.Infoln("device", devName, "is already assigned to service",
					service, "cluster", cluster, "requuid", requuid)
				return devName, nil
			}

			glog.Errorln("AssignDeviceName failed, cluster", cluster,
				"service", service, "error", err, "requuid", requuid)
			return "", err
		}

		// create Device in DB
		dev := db.CreateDevice(cluster, devName, service)
		err = s.dbIns.CreateDevice(ctx, dev)
		if err != nil {
			if err == db.ErrDBConditionalCheckFailed {
				glog.Errorln("retry, someone else created the device first", dev, "requuid", requuid)
				continue
			}

			glog.Errorln("failed to create device", dev, "error", err, "requuid", requuid)
			return "", err
		}

		return devName, nil
	}

	glog.Errorln("failed to create device after retry", maxRetryCount,
		"times, cluster", cluster, "service", service, "requuid", requuid)
	return "", common.ErrInternal
}

func (s *ManageService) createService(ctx context.Context, cluster string, service string) (serviceUUID string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// generate service uuid
	serviceUUID = utils.GenUUID()

	svc := db.CreateService(cluster, service, serviceUUID)
	err = s.dbIns.CreateService(ctx, svc)
	if err == nil {
		glog.Infoln("created service", svc, "requuid", requuid)
		return serviceUUID, nil
	}

	if err != db.ErrDBConditionalCheckFailed {
		// err != nil && not service exist
		glog.Errorln("CreateService error", err, "service", service, "cluster", cluster, "requuid", requuid)
		return "", err
	}

	// service exists, get it and return the uuid
	currItem, err := s.dbIns.GetService(ctx, cluster, service)
	if err != nil {
		glog.Errorln("GetService error", err, "service", service, "cluster", cluster, "requuid", requuid)
		return "", err
	}

	glog.Infoln("service exists", currItem, "requuid", requuid)
	return currItem.ServiceUUID, nil
}

func (s *ManageService) assignDeviceName(ctx context.Context, cluster string, service string) (devName string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// list current device items
	devs, err := s.dbIns.ListDevices(ctx, cluster)
	if err != nil {
		glog.Errorln("DB ListDevices failed, cluster", cluster, "error", err, "requuid", requuid)
		return "", err
	}

	firstDevice := s.serverIns.GetFirstDeviceName()

	if len(devs) == 0 {
		glog.Infoln("assign first device", firstDevice, "cluster", cluster, "requuid", requuid)
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
			glog.Infoln("device is already assigned to service", x, "requuid", requuid)
			return x.DeviceName, errServiceHasDevice
		}

		dev := x.DeviceName

		glog.V(1).Infoln("cluster", cluster, "service", service,
			"lastDev", lastDev, "current dev", dev, "requuid", requuid)

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

	glog.Infoln("cluster", cluster, "service", service, "lastDev", lastDev, len(devs), "requuid", requuid)

	// get the next device name
	return s.serverIns.GetNextDeviceName(lastDev)
}

func (s *ManageService) checkAndCreateServiceAttr(ctx context.Context, serviceUUID string, deviceName string,
	domainName string, hostedZoneID string, req *manage.CreateServiceRequest) (serviceCreated bool, sattr *common.ServiceAttr, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// create service attr
	serviceAttr := db.CreateInitialServiceAttr(serviceUUID, req.Replicas, req.VolumeSizeGB,
		req.Service.Cluster, req.Service.ServiceName, deviceName, req.HasMembership, domainName, hostedZoneID)
	err = s.dbIns.CreateServiceAttr(ctx, serviceAttr)
	if err == nil {
		glog.Infoln("created service attr in db", serviceAttr, "requuid", requuid)
		return false, serviceAttr, err
	}

	// CreateServiceAttr failed
	if err != db.ErrDBConditionalCheckFailed {
		glog.Errorln("CreateServiceAttr failed", err, "service attr", serviceAttr, "requuid", requuid)
		return false, nil, err
	}

	// service attr exists in db
	sattr, err = s.dbIns.GetServiceAttr(ctx, serviceUUID)
	if err != nil || sattr == nil {
		glog.Errorln("GetServiceAttr failed", err, "service attr", serviceAttr, "requuid", requuid)
		return false, nil, err
	}

	glog.Infoln("get existing service attr", sattr, "requuid", requuid)

	// check if it is a retry request. If not, return error
	skipMtime := true
	serviceAttr.ServiceStatus = sattr.ServiceStatus
	if !db.EqualServiceAttr(serviceAttr, sattr, skipMtime) {
		glog.Errorln("service exists, old service", sattr, "new", serviceAttr, "requuid", requuid)
		return false, sattr, common.ErrServiceExist
	}

	// a retry request, check the service status
	switch sattr.ServiceStatus {
	case common.ServiceStatusCreating:
		// service is at creating status, continue the creation
		glog.V(0).Infoln("service is at the creating status", sattr, "requuid", requuid)
		return false, sattr, nil

	case common.ServiceStatusInitializing:
		glog.V(0).Infoln("service already created, at the initializing status", sattr, "requuid", requuid)
		return true, sattr, nil

	case common.ServiceStatusActive:
		glog.V(0).Infoln("service already created, at the active status", sattr, "requuid", requuid)
		return true, sattr, nil

	case common.ServiceStatusDeleting:
		glog.Errorln("service is under deleting", sattr, "requuid", requuid)
		return true, sattr, common.ErrServiceDeleting

	case common.ServiceStatusDeleted:
		glog.Errorln("service is deleted", sattr, "requuid", requuid)
		return true, sattr, common.ErrServiceDeleted

	default:
		glog.Errorln("service is at unknown status", sattr, "requuid", requuid)
		return false, nil, common.ErrInternal
	}
}

func (s *ManageService) checkAndCreateServiceVolumes(ctx context.Context, serviceUUID string, devName string,
	req *manage.CreateServiceRequest) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// list to find out how many volumes were already created
	vols, err := s.dbIns.ListVolumes(ctx, serviceUUID)
	if err != nil {
		glog.Errorln("ListVolumes failed", err, "serviceUUID", serviceUUID, "requuid", requuid)
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
		cfgs, err := s.checkAndCreateConfigFile(ctx, serviceUUID, memberName, replicaCfg)
		if err != nil {
			glog.Errorln("checkAndCreateConfigFile error", err, "service", serviceUUID,
				"member", memberName, "requuid", requuid)
			return err
		}

		vol, err := s.createServiceVolume(ctx, serviceUUID, req.VolumeSizeGB,
			req.ReplicaConfigs[i].Zone, devName, memberName, cfgs)
		if err != nil {
			glog.Errorln("create volume failed, serviceUUID", serviceUUID, "member", memberName,
				"az", req.ReplicaConfigs[i].Zone, "error", err, "requuid", requuid)
			return err
		}
		allVols[i] = vol
	}

	glog.Infoln("created", req.Replicas-int64(len(vols)), "volumes for serviceUUID",
		serviceUUID, req.Service, "requuid", requuid)

	// EBS volume creation is async in the background. volume state will be creating,
	// then available. block waiting here, as EBS volume creation is pretty fast,
	// usually 3 seconds. see ec2_test.go output.
	for _, vol := range allVols {
		err = s.serverIns.WaitVolumeCreated(ctx, vol.VolumeID)
		if err != nil {
			glog.Errorln("WaitVolumeCreated error", err, "volume", vol, "requuid", requuid)
			return err
		}
		glog.Infoln("created volume", vol.VolumeID, req.Service, "requuid", requuid)
	}

	return nil
}

func (s *ManageService) checkAndCreateConfigFile(ctx context.Context, serviceUUID string, memberName string,
	replicaCfg *manage.ReplicaConfig) ([]*common.MemberConfig, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// the first version of the config file
	version := int64(0)

	configs := make([]*common.MemberConfig, len(replicaCfg.Configs))
	for i, cfg := range replicaCfg.Configs {
		fileID := utils.GenServiceMemberConfigID(memberName, cfg.FileName, version)
		dbcfg, err := s.createConfigFile(ctx, serviceUUID, fileID, cfg)
		if err != nil {
			glog.Errorln("createConfigFile error", err, "fileID", fileID, "service", serviceUUID, "requuid", requuid)
			return nil, err
		}
		configs[i] = &common.MemberConfig{FileName: cfg.FileName, FileID: fileID, FileMD5: dbcfg.FileMD5}
	}
	return configs, nil
}

func (s *ManageService) createConfigFile(ctx context.Context, serviceUUID string, fileID string,
	cfg *manage.ReplicaConfigFile) (*common.ConfigFile, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	dbcfg := db.CreateInitialConfigFile(serviceUUID, fileID, cfg.FileName, cfg.Content)
	err := s.dbIns.CreateConfigFile(ctx, dbcfg)
	if err == nil {
		glog.Infoln("created config file", fileID, "file name", cfg.FileName, "service", serviceUUID, "requuid", requuid)
		return dbcfg, nil
	}

	if err != db.ErrDBConditionalCheckFailed {
		glog.Errorln("CreateConfigFile error", err, "fileID", fileID, "service", serviceUUID, "requuid", requuid)
		return nil, err
	}

	// config file exists, check whether it is a retry request.
	currcfg, err := s.dbIns.GetConfigFile(ctx, serviceUUID, fileID)
	if err != nil {
		glog.Errorln("get existing config file error", err, "fileID", fileID, "service", serviceUUID, "requuid", requuid)
		return nil, err
	}

	skipMtime := true
	skipContent := true
	if !db.EqualConfigFile(currcfg, dbcfg, skipMtime, skipContent) {
		glog.Errorln("config file not match, current", db.PrintConfigFile(currcfg),
			"new", db.PrintConfigFile(dbcfg), "requuid", requuid)
		return nil, common.ErrConfigMismatch
	}

	glog.Infoln("config file exists", db.PrintConfigFile(currcfg), "requuid", requuid)
	return currcfg, nil
}

func (s *ManageService) createServiceVolume(ctx context.Context, serviceUUID string, volSize int64, az string,
	devName string, memberName string, cfgs []*common.MemberConfig) (vol *common.Volume, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	volID, err := s.serverIns.CreateVolume(ctx, az, volSize)
	if err != nil {
		glog.Errorln("CreateVolume failed, volID", volID,
			"serviceUUID", serviceUUID, "az", az, "error", err, "requuid", requuid)
		return nil, err
	}

	vol = db.CreateInitialVolume(serviceUUID, volID, devName, az, memberName, cfgs)
	err = s.dbIns.CreateVolume(ctx, vol)
	if err != nil {
		glog.Errorln("CreateVolume in DB failed, Volume", vol, "error", err, "requuid", requuid)
		return nil, err
	}

	glog.Infoln("created volume", vol, "requuid", requuid)
	return vol, nil
}
