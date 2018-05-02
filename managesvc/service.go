package managesvc

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/manage"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/server"
	"github.com/cloudstax/firecamp/utils"
)

// Service creation requires below steps
// 1. decide the cluster and service name, the number of replicas for the service,
//    serviceMember size and available zone.
// 2. call CreateService() to create the service with all service attributes in DB.
// 3. create the task definition and service in ECS in the target cluster with
//    the same service name and replica number.
// 4. create other related resources such as aws cloudwatch log group.
// 5. create the container service on the container orchestration framework.
// 6. add the service init task if necessary. The init task will set the service state to ACTIVE.

// Here is the implementation for step 2.
// - create device: check device items, assign the next block device name to the service,
//	 and create the device item.
//	 If there are concurrent creation, may need to retry. Should also check whether the
//   service already has the device item, this is possible as failure could happen at any time.
// - create the service item in DynamoDB
// - create the service attribute record with CREATING state.
// - create the desired number of serviceMembers, and create IDLE serviceMember items in DB.
// - update the service attribute state to INITIALIZING.
// For the single replica stateless service, only create the service item and service attribute record in DB.
// Currently, support the single replica stateless service. So there is no need to create
// the ServiceMember records. If the stateless service needs multiple replicas, could use
// Load Balancer such as ELB.
//
// The failure handling is simple. If the script failed at some step, user will get error and retry.
// Could have a background scanner to clean up the dead service, in case user didn't retry.

const (
	maxRetryCount    = 3
	defaultSleepTime = 2 * time.Second
)

var errServiceHasDevice = errors.New("device is already assigned to service")

// ManageService implements the service operation details.
type ManageService struct {
	dbIns           db.DB
	serverInfo      server.Info
	serverIns       server.Server
	dnsIns          dns.DNS
	containersvcIns containersvc.ContainerSvc

	// the lock to protect the possible concurrent static ip creation
	iplock *sync.Mutex
}

// NewManageService allocates a ManageService instance
func NewManageService(dbIns db.DB, serverInfo server.Info, serverIns server.Server, dnsIns dns.DNS, containersvcIns containersvc.ContainerSvc) *ManageService {
	return &ManageService{
		dbIns:           dbIns,
		serverInfo:      serverInfo,
		serverIns:       serverIns,
		dnsIns:          dnsIns,
		containersvcIns: containersvcIns,
		iplock:          &sync.Mutex{},
	}
}

// CreateService implements step 2.
// The cfgFileContents could either be one content for all replicas or one for each replica.
func (s *ManageService) CreateService(ctx context.Context, req *manage.CreateServiceRequest,
	domainName string, vpcID string) (serviceUUID string, err error) {
	// check args
	if len(req.Service.Cluster) == 0 || len(req.Service.ServiceName) == 0 ||
		len(req.Service.Region) == 0 || len(vpcID) == 0 {
		glog.Errorln("invalid args", req.Service, "vpc", vpcID)
		return "", common.ErrInvalidArgs
	}
	if req.ServiceType != common.ServiceTypeStateless {
		if req.Replicas <= 0 || req.Volume.VolumeSizeGB <= 0 {
			glog.Errorln("invalid args", req.Service, "Replicas", req.Replicas, "VolumeSizeGB", req.Volume.VolumeSizeGB)
			return "", common.ErrInvalidArgs
		}
		if int64(len(req.ReplicaConfigs)) != req.Replicas {
			errmsg := fmt.Sprintf("invalid args, every replica should have a config file, replica number %d, config file number %d",
				len(req.ReplicaConfigs), req.Replicas)
			glog.Errorln(errmsg)
			return "", errors.New(errmsg)
		}
	}

	requuid := utils.GetReqIDFromContext(ctx)

	// check and create system tables if necessary
	err = s.checkAndCreateSystemTables(ctx)
	if err != nil {
		glog.Errorln("checkAndCreateSystemTables error", err, req.Service, "requuid", requuid)
		return "", err
	}

	if len(domainName) == 0 {
		// use the default domainName
		domainName = dns.GenDefaultDomainName(req.Service.Cluster)
	}

	// check if the hosted zone exists
	private := true
	hostedZoneID, err := s.dnsIns.GetOrCreateHostedZoneIDByName(ctx, domainName, vpcID, req.Service.Region, private)
	if err != nil {
		glog.Errorln("GetOrCreateHostedZoneIDByName error", err, "domain", domainName,
			"vpc", vpcID, "requuid", requuid, req.Service)
		return "", err
	}
	glog.Infoln("get hostedZoneID", hostedZoneID, "for domain", domainName, "vpc", vpcID, "requuid", requuid, req.Service)

	// create service volumes
	svols := &common.ServiceVolumes{}
	if req.ServiceType != common.ServiceTypeStateless {
		svols, err = s.createServiceVolumes(ctx, req)
		if err != nil {
			glog.Errorln("createServiceVolumes error", err, "requuid", requuid, req.Service)
			return "", err
		}
	}

	// create service item
	serviceUUID, err = s.createService(ctx, req.Service.Cluster, req.Service.ServiceName)
	if err != nil {
		glog.Errorln("createService error", err, "requuid", requuid, req.Service)
		return "", err
	}

	// create service attr
	created, serviceAttr, err := s.checkAndCreateServiceAttr(ctx, serviceUUID, svols, domainName, hostedZoneID, req)
	if err != nil {
		glog.Errorln("checkAndCreateServiceAttr error", err, "requuid", requuid, req.Service)
		return "", err
	}
	if created {
		glog.Infoln("service is already created, requuid", requuid, serviceAttr)
		return serviceAttr.ServiceUUID, nil
	}

	glog.Infoln("created service attr, requuid", requuid, serviceAttr)

	// create the desired number of serviceMembers
	err = s.CheckAndCreateServiceMembers(ctx, req.Replicas, serviceAttr, req.ReplicaConfigs)
	if err != nil {
		glog.Errorln("CheckAndCreateServiceMembers failed", err, "requuid", requuid, "service", serviceAttr)
		return "", err
	}

	// update service status to INITIALIZING
	newAttr := db.UpdateServiceStatus(serviceAttr, common.ServiceStatusInitializing)
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

	switch sattr.Meta.ServiceStatus {
	case common.ServiceStatusCreating:
		glog.Errorln("service is at the creating status, requuid", requuid, sattr)
		return db.ErrDBConditionalCheckFailed

	case common.ServiceStatusInitializing:
		newAttr := db.UpdateServiceStatus(sattr, common.ServiceStatusActive)
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

// ListServiceVolumes return all volumes of the service. DeleteService will not delete the
// actual volume in cloud, such as EBS. Customer should ListServiceVolumes before DeleteService,
// and delete the actual volumes manually.
func (s *ManageService) ListServiceVolumes(ctx context.Context, cluster string, service string) (serviceVolumes []string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// get service
	sitem, err := s.dbIns.GetService(ctx, cluster, service)
	if err != nil {
		glog.Errorln("GetService error", err, "service", service, "cluster", cluster, "requuid", requuid)
		return nil, err
	}

	// get service serviceMembers
	members, err := s.dbIns.ListServiceMembers(ctx, sitem.ServiceUUID)
	if err != nil {
		glog.Errorln("ListServiceMembers error", err, "requuid", requuid, "service", sitem)
		return nil, err
	}

	for _, m := range members {
		if len(m.Spec.Volumes.PrimaryVolumeID) != 0 {
			serviceVolumes = append(serviceVolumes, m.Spec.Volumes.PrimaryVolumeID)
		}
		if len(m.Spec.Volumes.JournalVolumeID) != 0 {
			serviceVolumes = append(serviceVolumes, m.Spec.Volumes.JournalVolumeID)
		}
	}

	glog.Infoln("cluster", cluster, "service", service, "has", len(serviceVolumes), "serviceVolumes, requuid", requuid)
	return serviceVolumes, nil
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
//   - caller should stop and delete the service on the container platform.
//   - the actual cloud serviceMembers of this service are not deleted, customer needs
//     to delete them manually.
func (s *ManageService) DeleteService(ctx context.Context, cluster string, service string) (volIDs []string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// get service
	sitem, err := s.dbIns.GetService(ctx, cluster, service)
	if err != nil {
		glog.Errorln("GetService error", err, "service", service, "cluster", cluster, "requuid", requuid)
		return volIDs, err
	}

	// get service attr
	sattr, err := s.dbIns.GetServiceAttr(ctx, sitem.ServiceUUID)
	if err != nil {
		if err == db.ErrDBRecordNotFound {
			glog.Infoln("service attr not found, delete service item, requuid", requuid, sitem)
			err = s.dbIns.DeleteService(ctx, cluster, service)
			return volIDs, err
		}
		glog.Errorln("GetServiceAttr error", err, "requuid", requuid, "service", sitem)
		return volIDs, err
	}

	if sattr.Meta.ServiceStatus != common.ServiceStatusDeleting {
		// set service status to deleting
		newAttr := db.UpdateServiceStatus(sattr, common.ServiceStatusDeleting)
		err = s.dbIns.UpdateServiceAttr(ctx, sattr, newAttr)
		if err != nil {
			glog.Errorln("set service deleting status error", err, "requuid", requuid, "service attr", sattr)
			return volIDs, err
		}

		sattr = newAttr
		glog.Infoln("set service to deleting status, requuid", requuid, sattr)
	} else {
		glog.Infoln("service is already at deleting status, requuid", requuid, sattr)
	}

	// get service serviceMembers
	members, err := s.dbIns.ListServiceMembers(ctx, sattr.ServiceUUID)
	if err != nil {
		glog.Errorln("ListServiceMembers error", err, "requuid", requuid, "service attr", sattr)
		return volIDs, err
	}

	// delete the member's dns record, static ip and config files
	glog.Infoln("deleting the config files from DB, service attr, requuid", requuid, sattr)
	for _, m := range members {
		// delete the member's dns record
		dnsname := dns.GenDNSName(m.MemberName, sattr.Spec.DomainName)
		s.deleteDNSRecord(ctx, dnsname, sattr.Spec.HostedZoneID, requuid)

		// delete the member's static ip
		if sattr.Spec.RequireStaticIP {
			err = s.dbIns.DeleteServiceStaticIP(ctx, m.Spec.StaticIP)
			if err != nil && err != db.ErrDBRecordNotFound {
				glog.Errorln("DeleteServiceStaticIP error", err, "requuid", requuid, "member", m)
				return volIDs, err
			}
			glog.Infoln("deleted member's static ip, requuid", requuid, m)
		}

		// delete the member's config files
		for _, c := range m.Spec.Configs {
			err := s.dbIns.DeleteConfigFile(ctx, m.ServiceUUID, c.FileID)
			if err != nil && err != db.ErrDBRecordNotFound {
				glog.Errorln("DeleteConfigFile error", err, "requuid", requuid, "config", c, "serviceMember", m)
				return volIDs, err
			}
			glog.V(1).Infoln("deleted config file", c.FileID, m.ServiceUUID, "requuid", requuid)
		}

		if s.containersvcIns.GetContainerSvcType() != common.ContainerPlatformK8s {
			// detach the member's volumes for ecs and swarm.
			// k8s statefulset takes care of the volume, no need to detach.
			err = s.detachVolumes(ctx, m, requuid)
			if err != nil {
				glog.Errorln("detach volume error", err, "requuid", requuid, m.Spec.Volumes, m)
				return volIDs, err
			}
		}
	}

	// delete all serviceMember records in DB
	glog.Infoln("deleting serviceMembers from DB, service attr", sattr, "serviceMembers", len(members), "requuid", requuid)
	for _, m := range members {
		// delete the container service volume, the possible error is skipped
		if len(m.Spec.Volumes.PrimaryVolumeID) != 0 {
			s.containersvcIns.DeleteServiceVolume(ctx, sattr.Meta.ServiceName, m.MemberName, false)
		}
		if len(m.Spec.Volumes.JournalVolumeID) != 0 {
			s.containersvcIns.DeleteServiceVolume(ctx, sattr.Meta.ServiceName, m.MemberName, true)
		}

		err := s.dbIns.DeleteServiceMember(ctx, m.ServiceUUID, m.MemberName)
		if err != nil && err != db.ErrDBRecordNotFound {
			glog.Errorln("DeleteServiceMember error", err, "requuid", requuid, m)
			return volIDs, err
		}

		glog.V(1).Infoln("deleted serviceMember, requuid", requuid, m)

		if len(m.Spec.Volumes.PrimaryVolumeID) != 0 {
			volIDs = append(volIDs, m.Spec.Volumes.PrimaryVolumeID)
		}
		if len(m.Spec.Volumes.JournalVolumeID) != 0 {
			volIDs = append(volIDs, m.Spec.Volumes.JournalVolumeID)
		}
	}
	glog.Infoln("deleted", len(members), "serviceMembers from DB, service attr", sattr, "requuid", requuid)

	// TODO the static ip record is created before the service member record.
	// some static ip record may be left in DB. scan to delete them.

	// delete the service's config files
	for _, c := range sattr.Spec.ServiceConfigs {
		err := s.dbIns.DeleteConfigFile(ctx, sattr.ServiceUUID, c.FileID)
		if err != nil && err != db.ErrDBRecordNotFound {
			glog.Errorln("DeleteConfigFile error", err, "requuid", requuid, "config", c)
			return volIDs, err
		}
		glog.V(1).Infoln("deleted config file", c.FileID, sattr.ServiceUUID, "requuid", requuid)
	}

	// delete the devices
	err = s.deleteDevices(ctx, sattr, requuid)
	if err != nil {
		glog.Errorln("delete device error", err, "requuid", requuid, sattr.Spec.Volumes, sattr)
		return volIDs, err
	}

	// delete service attr
	err = s.dbIns.DeleteServiceAttr(ctx, sattr.ServiceUUID)
	if err != nil && err != db.ErrDBRecordNotFound {
		glog.Errorln("DeleteServiceAttr error", err, sattr)
		return volIDs, err
	}
	glog.Infoln("deleted service attr", sattr, "requuid", requuid)

	// delete service
	err = s.dbIns.DeleteService(ctx, cluster, service)
	if err != nil && err != db.ErrDBRecordNotFound {
		glog.Errorln("DeleteService error", err, "service", service, "cluster", cluster, "requuid", requuid)
		return volIDs, err
	}

	glog.Infoln("delete service complete, service", service, "cluster", cluster, "requuid", requuid)
	return volIDs, nil
}

func (s *ManageService) deleteDNSRecord(ctx context.Context, dnsname string, hostedZoneID string, requuid string) {
	hostname, err := s.dnsIns.GetDNSRecord(ctx, dnsname, hostedZoneID)
	if err == nil {
		// delete the dns record
		err = s.dnsIns.DeleteDNSRecord(ctx, dnsname, hostname, hostedZoneID)
		if err == nil {
			glog.Infoln("deleted dns record, dnsname", dnsname, "HostedZone", hostedZoneID, "requuid", requuid)
		} else if err != dns.ErrDNSRecordNotFound && err != dns.ErrHostedZoneNotFound {
			// skip the dns record deletion error
			glog.Errorln("DeleteDNSRecord error", err, "dnsname", dnsname, "HostedZone", hostedZoneID, "requuid", requuid)
		}
		return
	}

	if err != dns.ErrDNSRecordNotFound && err != dns.ErrHostedZoneNotFound {
		// skip the unknown dns error, as it would only leave a garbage in the dns system.
		glog.Errorln("GetDNSRecord error", err, "dnsname", dnsname, "HostedZone", hostedZoneID, "requuid", requuid)
	}
}

func (s *ManageService) detachVolumes(ctx context.Context, m *common.ServiceMember, requuid string) error {
	// check if the volume is still in-use. This might be possible at some corner case.
	// Probably happens at the below sequence during the test: 1) volume attached to the node,
	// 2) volume driver gets restarted, 3) delete the service. The volume is not detached.
	// Checked the volume driver log, looks volume driver only received the “Get” request and
	// returned volume not mounted. No unmount call to the volume driver.

	// detach the journal volume
	if len(m.Spec.Volumes.JournalVolumeID) != 0 {
		err := s.detachVolume(ctx, m.Spec.Volumes.JournalVolumeID, m.Spec.ServerInstanceID, m.Spec.Volumes.JournalDeviceName, requuid)
		if err != nil {
			glog.Errorln("detach journal volume error", err, m.Spec.Volumes.JournalVolumeID, "requuid", requuid, m)
			return err
		}

		glog.Infoln("detached journal volume", m.Spec.Volumes.JournalVolumeID, "requuid", requuid, m.MemberName)
	}

	// detach the primary volume
	if len(m.Spec.Volumes.PrimaryVolumeID) != 0 {
		err := s.detachVolume(ctx, m.Spec.Volumes.PrimaryVolumeID, m.Spec.ServerInstanceID, m.Spec.Volumes.PrimaryDeviceName, requuid)
		if err != nil {
			glog.Errorln("detach volume error", err, m.Spec.Volumes.PrimaryVolumeID, "requuid", requuid, m)
			return err
		}

		glog.Infoln("detached volume", m.Spec.Volumes.PrimaryVolumeID, "requuid", requuid, m.MemberName)
	}

	return nil
}

func (s *ManageService) detachVolume(ctx context.Context, volID string, serverInstanceID string, deviceName string, requuid string) error {
	volState, err := s.serverIns.GetVolumeState(ctx, volID)
	if err != nil && err != common.ErrNotFound {
		glog.Errorln("GetVolumeState error", err, volID, "requuid", requuid)
		return err
	}
	if volState == server.VolumeStateInUse || volState == server.VolumeStateAttaching {
		glog.Errorln("the service volume", volID, "is still in-use or attaching, detach it, requuid", requuid)
		err = s.serverIns.DetachVolume(ctx, volID, serverInstanceID, deviceName)
		if err != nil {
			glog.Errorln("DetachVolume error", err, volID, "requuid", requuid)
			return err
		}
	}

	return nil
}

func (s *ManageService) deleteDevices(ctx context.Context, sattr *common.ServiceAttr, requuid string) error {
	if len(sattr.Spec.Volumes.PrimaryDeviceName) != 0 {
		// delete the primary device
		err := s.dbIns.DeleteDevice(ctx, sattr.Meta.ClusterName, sattr.Spec.Volumes.PrimaryDeviceName)
		if err != nil && err != db.ErrDBRecordNotFound {
			glog.Errorln("delete primary device error", err, "requuid", requuid, sattr.Spec.Volumes, sattr)
			return err
		}
		glog.Infoln("deleted primary device, requuid", requuid, sattr.Spec.Volumes, sattr)
	}

	if len(sattr.Spec.Volumes.JournalDeviceName) != 0 {
		err := s.dbIns.DeleteDevice(ctx, sattr.Meta.ClusterName, sattr.Spec.Volumes.JournalDeviceName)
		if err != nil && err != db.ErrDBRecordNotFound {
			glog.Errorln("delete journal device error", err, "requuid", requuid, sattr.Spec.Volumes, sattr)
			return err
		}
		glog.Infoln("deleted journal device, requuid", requuid, sattr.Spec.Volumes, sattr)
	}

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
func (s *ManageService) createServiceVolumes(ctx context.Context, req *manage.CreateServiceRequest) (*common.ServiceVolumes, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// create the device for the primary volume
	excludeDevice := ""
	primaryDevName, err := s.createDevice(ctx, req.Service.Cluster, req.Service.ServiceName, excludeDevice, requuid)
	if err != nil {
		glog.Errorln("create primary device error", err, "requuid", requuid, req.Service)
		return nil, err
	}

	glog.Infoln("created primary device", primaryDevName, "requuid", requuid, req.Service)

	svols := &common.ServiceVolumes{
		PrimaryDeviceName: primaryDevName,
		PrimaryVolume:     *req.Volume,
	}

	if req.JournalVolume != nil {
		excludeDevice = primaryDevName
		journalDevName, err := s.createDevice(ctx, req.Service.Cluster, req.Service.ServiceName, excludeDevice, requuid)
		if err != nil {
			glog.Errorln("create journal device error", err, "requuid", requuid, req.Service)
			return nil, err
		}

		glog.Infoln("created journal device", journalDevName, "requuid", requuid, req.Service)

		svols.JournalDeviceName = journalDevName
		svols.JournalVolume = *req.JournalVolume
	}

	return svols, nil
}

func (s *ManageService) createDevice(ctx context.Context, cluster string, service string, excludeDevice string, requuid string) (devName string, err error) {
	for i := 0; i < maxRetryCount; i++ {
		// assign the device name
		devName, err := s.assignDeviceName(ctx, cluster, service, excludeDevice)
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

func (s *ManageService) assignDeviceName(ctx context.Context, cluster string, service string, excludeDevice string) (devName string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// list current device items
	devs, err := s.dbIns.ListDevices(ctx, cluster)
	if err != nil {
		glog.Errorln("DB ListDevices failed, cluster", cluster, "error", err, "requuid", requuid)
		return "", err
	}

	sort.Slice(devs, func(i, j int) bool {
		return len(devs[i].DeviceName) < len(devs[j].DeviceName) || devs[i].DeviceName < devs[j].DeviceName
	})

	firstDevice := s.serverIns.GetFirstDeviceName()

	if len(devs) == 0 {
		if firstDevice != excludeDevice {
			glog.Infoln("assign first device", firstDevice, "service", service, "requuid", requuid)
			return firstDevice, nil
		}

		secondDevice, err := s.serverIns.GetNextDeviceName(firstDevice)
		glog.Infoln("assign the second device", secondDevice, "error", err, "service", service, "requuid", requuid)
		return secondDevice, err
	}

	// get the last device
	// TODO support recycle device in the middle, for example, xvdf~p are used,
	//			then service for xvdg is deleted.
	lastDev := firstDevice
	for _, x := range devs {
		if x.DeviceName == excludeDevice {
			lastDev = x.DeviceName
			continue
		}

		// The service creation involves multiple steps, and may fail at any step.
		// The customer may retry the creation. Check if device is already assigned
		// to one service.
		if x.ServiceName == service {
			glog.Infoln("device is already assigned to service", x, "requuid", requuid)
			return x.DeviceName, errServiceHasDevice
		}
		// The different lower/upper case letter service name is not allowed, as the dns service
		// does not distinguish the uppercase and lowercase.
		if strings.ToLower(x.ServiceName) == strings.ToLower(service) {
			glog.Errorln("service exists with different lower/upper case letter, existing name",
				x.ServiceName, "new service name", service, "requuid", requuid)
			return "", common.ErrServiceExist
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

func (s *ManageService) checkAndCreateServiceAttr(ctx context.Context, serviceUUID string, svols *common.ServiceVolumes,
	domainName string, hostedZoneID string, req *manage.CreateServiceRequest) (serviceCreated bool, sattr *common.ServiceAttr, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// check and create the config file first
	cfgs, err := s.checkAndCreateConfigFile(ctx, serviceUUID, req.Service.ServiceName, req.ServiceConfigs)
	if err != nil {
		glog.Errorln("checkAndCreateConfigFile error", err, "service", req.Service.ServiceName, "requuid", requuid)
		return false, nil, err
	}

	// create service attr
	meta := db.CreateServiceMeta(req.Service.Cluster, req.Service.ServiceName, time.Now().UnixNano(), req.ServiceType, common.ServiceStatusCreating)
	spec := db.CreateServiceSpec(req.Replicas, req.Resource, req.RegisterDNS, domainName, hostedZoneID, req.RequireStaticIP, cfgs, req.CatalogServiceType, svols)
	serviceAttr := db.CreateServiceAttr(serviceUUID, 0, meta, spec)
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
	skipRevision := true
	serviceAttr.Meta.ServiceStatus = sattr.Meta.ServiceStatus
	if !db.EqualServiceAttr(serviceAttr, sattr, skipMtime, skipRevision) {
		glog.Errorln("service exists, old service", sattr, "new", serviceAttr, "requuid", requuid)
		return false, sattr, common.ErrServiceExist
	}

	// a retry request, check the service status
	switch sattr.Meta.ServiceStatus {
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

// CheckAndCreateServiceMembers check and create the service members
func (s *ManageService) CheckAndCreateServiceMembers(ctx context.Context, replicas int64, sattr *common.ServiceAttr, replicaConfigs []*manage.ReplicaConfig) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// list to find out how many serviceMembers were already created
	members, err := s.dbIns.ListServiceMembers(ctx, sattr.ServiceUUID)
	if err != nil {
		glog.Errorln("ListServiceMembers failed", err, "serviceUUID", sattr.ServiceUUID, "requuid", requuid)
		return err
	}

	var staticIPs map[string][]*common.ServiceStaticIP
	if sattr.Spec.RequireStaticIP {
		staticIPs, err = s.createStaticIPs(ctx, sattr, replicaConfigs, members)
		if err != nil {
			glog.Errorln("createStaticIPs error", err, "serviceUUID", sattr.ServiceUUID, "requuid", requuid)
			return err
		}
	}

	// TODO check the config in the existing serviceMembers.
	// if config file does not match, return error. The customer should not do like:
	// 1) create the service, but failed as the node crashes
	// 2) the customer retries the creation with the different config.
	// Currently we don't support this case, as some serviceMembers may have been created with the
	// old config. The retry will not recreate those serviceMembers.
	// If the customer wants to retry with the different config, should delete the current service first.

	allMembers := make([]*common.ServiceMember, replicas)
	copy(allMembers, members)

	for i := len(members); i < int(replicas); i++ {
		replicaCfg := replicaConfigs[i]

		// create the dns record with faked ip. This could help to reduce the initial dns lookup wait time (60s).
		// sometimes, the dns lookup wait could be reduced to like 15s in the volume driver.
		dnsname := dns.GenDNSName(replicaCfg.MemberName, sattr.Spec.DomainName)

		// faked host ip for DNS update
		// TODO could get all server instance IDs and ips, assign the member to one instance.
		//      this may help to reduce the initial 1m dns lookup waiting.
		hostIP := common.DefaultHostIP
		if sattr.Spec.RequireStaticIP {
			// get one static ip
			ips, ok := staticIPs[replicaCfg.Zone]
			if !ok {
				glog.Errorln("internal error, not find static ip for zone", replicaCfg.Zone,
					"serviceUUID", sattr.ServiceUUID, "requuid", requuid)
				return common.ErrInternal
			}
			if len(ips) == 0 {
				glog.Errorln("internal error, not more static ip for zone", replicaCfg.Zone,
					"serviceUUID", sattr.ServiceUUID, "requuid", requuid)
				return common.ErrInternal
			}

			hostIP = ips[0].StaticIP

			glog.Infoln("get static ip for member", replicaCfg.MemberName, "requuid", requuid, ips[0])

			// remove the assigned static ip
			ips = ips[1:]
			staticIPs[replicaCfg.Zone] = ips
		}

		err = s.dnsIns.UpdateDNSRecord(ctx, dnsname, hostIP, sattr.Spec.HostedZoneID)
		if err != nil {
			// ignore the error. When container is created, the actual dns record will be created.
			glog.Errorln("UpdateDNSRecord error", err, "dnsname", dnsname, "serviceUUID", sattr.ServiceUUID, "requuid", requuid)
		}
		glog.Infoln("updated member dns", dnsname, "to", hostIP, "serviceUUID", sattr.ServiceUUID, "requuid", requuid)

		// check and create the config file first
		cfgs, err := s.checkAndCreateConfigFile(ctx, sattr.ServiceUUID, replicaCfg.MemberName, replicaCfg.Configs)
		if err != nil {
			glog.Errorln("checkAndCreateConfigFile error", err, "service", sattr.ServiceUUID,
				"member", replicaCfg.MemberName, "requuid", requuid)
			return err
		}

		member, err := s.createServiceMember(ctx, sattr, replicaCfg.Zone, replicaCfg.MemberName, hostIP, cfgs)
		if err != nil {
			glog.Errorln("create serviceMember failed, serviceUUID", sattr.ServiceUUID, "member", replicaCfg.MemberName,
				"az", replicaCfg.Zone, "error", err, "requuid", requuid)
			return err
		}
		allMembers[i] = member
	}

	glog.Infoln("created", replicas-int64(len(members)), "serviceMembers for service",
		sattr, "requuid", requuid)

	if sattr.Meta.ServiceType != common.ServiceTypeStateless {
		// EBS volume creation is async in the background. Volume state will be creating,
		// then available. block waiting here, as EBS Volume creation is pretty fast,
		// usually 3 seconds. see ec2_test.go output.
		for _, member := range allMembers {
			if len(member.Spec.Volumes.PrimaryVolumeID) != 0 {
				err = s.serverIns.WaitVolumeCreated(ctx, member.Spec.Volumes.PrimaryVolumeID)
				if err != nil {
					glog.Errorln("wait primary volume created error", err, "requuid", requuid, member.Spec.Volumes, member)
					return errors.New("wait volume created error: " + err.Error())
				}
			}

			if len(member.Spec.Volumes.JournalVolumeID) != 0 {
				err = s.serverIns.WaitVolumeCreated(ctx, member.Spec.Volumes.JournalVolumeID)
				if err != nil {
					glog.Errorln("wait journal volume created error", err, "requuid", requuid, member.Spec.Volumes, member)
					return errors.New("wait volume created error: " + err.Error())
				}
			}

			glog.Infoln("created volumes, requuid", requuid, member.Spec.Volumes, member)
		}
	}

	return nil
}

func (s *ManageService) checkAndCreateConfigFile(ctx context.Context, serviceUUID string,
	prefix string, cfgs []*manage.ConfigFileContent) (configs []common.ConfigID, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// the first version of the config file
	version := int64(0)
	for _, cfg := range cfgs {
		config, err := s.CreateConfig(ctx, serviceUUID, prefix, cfg, version, requuid)
		if err != nil {
			glog.Errorln("createConfigFile error", err, "service", serviceUUID, "prefix", prefix, "requuid", requuid)
			return nil, err
		}
		configs = append(configs, *config)
	}
	return configs, nil
}

// CreateConfig creates the config file
func (s *ManageService) CreateConfig(ctx context.Context, serviceUUID string, prefix string,
	cfg *manage.ConfigFileContent, version int64, requuid string) (*common.ConfigID, error) {
	fileID := utils.GenConfigFileID(prefix, cfg.FileName, version)
	initcfgfile := db.CreateInitialConfigFile(serviceUUID, fileID, cfg.FileName, cfg.FileMode, cfg.Content)
	cfgfile, err := createConfigFile(ctx, s.dbIns, initcfgfile, requuid)
	if err != nil {
		glog.Errorln("createConfigFile error", err, "fileID", fileID, "service", serviceUUID, "requuid", requuid)
		return nil, err
	}
	config := &common.ConfigID{FileName: cfg.FileName, FileID: fileID, FileMD5: cfgfile.Spec.FileMD5}
	return config, nil
}

func (s *ManageService) createServiceMember(ctx context.Context, sattr *common.ServiceAttr,
	az string, memberName string, staticIP string, cfgs []common.ConfigID) (member *common.ServiceMember, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	mvols := common.MemberVolumes{}

	if len(sattr.Spec.Volumes.PrimaryDeviceName) != 0 {
		volID, err := s.createVolume(ctx, sattr, az, memberName, false)
		if err != nil {
			glog.Errorln("create primary volume error", err, "member", memberName, "az", az, "requuid", requuid, sattr)
			return nil, err
		}

		mvols.PrimaryVolumeID = volID
		mvols.PrimaryDeviceName = sattr.Spec.Volumes.PrimaryDeviceName

		glog.Infoln("created primary volume", volID, "member", memberName, "az", az, "requuid", requuid, sattr)
	}

	if len(sattr.Spec.Volumes.JournalDeviceName) != 0 {
		volID, err := s.createVolume(ctx, sattr, az, memberName, true)
		if err != nil {
			glog.Errorln("create journal volume error", err, "member", memberName, "az", az, "requuid", requuid, sattr)
			return nil, err
		}

		mvols.JournalVolumeID = volID
		mvols.JournalDeviceName = sattr.Spec.Volumes.JournalDeviceName

		glog.Infoln("created journal volume", volID, "az", az, "requuid", requuid, sattr)
	}

	meta := db.CreateMemberMeta(time.Now().UnixNano(), common.ServiceMemberStatusActive)
	spec := db.CreateInitialMemberSpec(az, &mvols, staticIP, cfgs)
	member = db.CreateServiceMember(sattr.ServiceUUID, memberName, 0, meta, spec)
	err = s.dbIns.CreateServiceMember(ctx, member)
	if err != nil {
		glog.Errorln("CreateServiceMember in DB failed", member, "error", err, "requuid", requuid)
		return nil, err
	}

	glog.Infoln("created serviceMember", member, "requuid", requuid)
	return member, nil
}

func (s *ManageService) createVolume(ctx context.Context, sattr *common.ServiceAttr,
	az string, memberName string, journal bool) (volID string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	nameTag := sattr.Meta.ClusterName + common.NameSeparator + memberName
	vol := &sattr.Spec.Volumes.PrimaryVolume
	if journal {
		nameTag += common.NameSeparator + containersvc.JournalVolumeNamePrefix
		vol = &sattr.Spec.Volumes.JournalVolume
	}

	volOpts := &server.CreateVolumeOptions{
		AvailabilityZone: az,
		VolumeType:       vol.VolumeType,
		VolumeSizeGB:     vol.VolumeSizeGB,
		Iops:             vol.Iops,
		Encrypted:        vol.Encrypted,
		TagSpecs: []common.KeyValuePair{
			common.KeyValuePair{
				Key:   "Name",
				Value: nameTag,
			},
		},
	}

	// TODO managesvc may be restarted at any time. check whether there are newly created available volumes.
	volID, err = s.serverIns.CreateVolume(ctx, volOpts)
	if err != nil {
		glog.Errorln("create the volume error", err, "member", memberName, "az", az, "requuid", requuid, sattr)
		return "", err
	}

	glog.Infoln("created volume", volID, "member", memberName, "az", az, "requuid", requuid, sattr)

	existingVolID, err := s.containersvcIns.CreateServiceVolume(ctx, sattr.Meta.ServiceName, memberName, volID, vol.VolumeSizeGB, journal)
	if err != nil {
		if err != containersvc.ErrVolumeExist {
			glog.Errorln("create the container service volume error", err, "volume", volID, "member", memberName, "requuid", requuid, sattr)
			// not safe to delete the EBS volume here, as CreateServiceVolume may time out and k8s PV is actually created.
			return "", err
		}

		glog.Infoln("the container service volume exists", existingVolID, "member", memberName, "requuid", requuid, sattr)

		if volID != existingVolID {
			// try to delete the newly created volume, ignore the possible error
			s.serverIns.DeleteVolume(ctx, volID)
		}

		volID = existingVolID
	}

	return volID, nil
}

func (s *ManageService) createStaticIPs(ctx context.Context, sattr *common.ServiceAttr,
	replicaConfigs []*manage.ReplicaConfig, members []*common.ServiceMember) (map[string][]*common.ServiceStaticIP, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// the lock to protect the concurrent creations
	s.iplock.Lock()
	defer s.iplock.Unlock()

	// get the number of replicas per zone
	pendingReplicas := make(map[string]int)
	for _, cfg := range replicaConfigs {
		replicas, ok := pendingReplicas[cfg.Zone]
		if ok {
			replicas++
		} else {
			replicas = 1
		}
		pendingReplicas[cfg.Zone] = replicas
	}

	// get the assigned static ips
	assignedIPs := make(map[string]string)
	for _, m := range members {
		assignedIPs[m.Spec.StaticIP] = m.MemberName

		replicas, ok := pendingReplicas[m.Spec.AvailableZone]
		if !ok {
			glog.Errorln("internal error, member zone is invalid", m, "requuid", requuid)
			return nil, common.ErrInternal
		}
		replicas--
		pendingReplicas[m.Spec.AvailableZone] = replicas
	}

	glog.Infoln("create static ip for the pending replicas", pendingReplicas, "serviceUUID", sattr.ServiceUUID, "requuid", requuid)

	// create the static IPs
	zoneStaticIPs := make(map[string][]*common.ServiceStaticIP)

	for zone, pendingCounts := range pendingReplicas {
		unassignedIPs, err := s.createStaticIPsForZone(ctx, sattr, assignedIPs, pendingCounts, zone)
		if err != nil {
			glog.Errorln("createStaticIPsForZone error", err, "zone", zone, "serviceUUID", sattr.ServiceUUID, "requuid", requuid)
			return nil, err
		}

		glog.Infoln("created", len(unassignedIPs), "static ips at zone", zone, "serviceUUID", sattr.ServiceUUID, "requuid", requuid)

		zoneStaticIPs[zone] = unassignedIPs
	}

	return zoneStaticIPs, nil
}

func (s *ManageService) createStaticIPsForZone(ctx context.Context, sattr *common.ServiceAttr,
	assignedIPs map[string]string, counts int, zone string) ([]*common.ServiceStaticIP, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// get all existing ips
	netInterfaces, cidrBlock, err := s.serverIns.GetNetworkInterfaces(ctx, sattr.Meta.ClusterName, s.serverInfo.GetLocalVpcID(), zone)
	if err != nil {
		glog.Errorln("GetNetworkInterfaceAndIPs error", err, "vpc", s.serverInfo.GetLocalVpcID(),
			"zone", zone, "serviceUUID", sattr.ServiceUUID, "requuid", requuid)
		return nil, err
	}

	// get unassigned ips. the node could crash at any time, some ip may already be created.
	unassignedIPs, err := s.getUnassignedIPs(ctx, sattr, netInterfaces, zone, assignedIPs, requuid)
	if err != nil {
		return nil, err
	}

	glog.Infoln("there are", len(unassignedIPs), "unassigned ips, need", counts,
		"zone", zone, "serviceUUID", sattr.ServiceUUID, "requuid", requuid)

	// check if need to create more ips
	if len(unassignedIPs) >= counts {
		return unassignedIPs, nil
	}

	// create more static ips.

	// sort the network interfaces by the number of ips
	sort.Slice(netInterfaces, func(i, j int) bool {
		return len(netInterfaces[i].PrivateIPs) < len(netInterfaces[j].PrivateIPs)
	})

	// get all used ips
	usedIPs := make(map[string]bool)
	for _, netInterface := range netInterfaces {
		usedIPs[netInterface.PrimaryPrivateIP] = true
		for _, ip := range netInterface.PrivateIPs {
			usedIPs[ip] = true
		}
	}

	// ParseCIDR
	nextIP, ipnet, err := net.ParseCIDR(cidrBlock)
	if err != nil {
		glog.Errorln("ParseCIDR error", err, cidrBlock, "vpc", s.serverInfo.GetLocalVpcID(),
			"zone", zone, "serviceUUID", sattr.ServiceUUID, "requuid", requuid)
		return nil, err
	}

	pendingCounts := counts - len(unassignedIPs)
	for i := 0; i < pendingCounts; i++ {
		netInterfaceIdx := i % len(netInterfaces)
		netInterface := netInterfaces[netInterfaceIdx]

		// create the next unused IP
		nextIP, err = s.createNextIP(ctx, usedIPs, ipnet, nextIP, netInterface.InterfaceID)
		if err != nil {
			glog.Errorln("AssignStaticIP error", err, "network interface", netInterface.InterfaceID,
				"zone", zone, "serviceUUID", sattr.ServiceUUID, "requuid", requuid)
			return nil, err
		}

		ip := nextIP.String()
		spec := db.CreateStaticIPSpec(sattr.ServiceUUID, zone, netInterface.ServerInstanceID, netInterface.InterfaceID)
		serviceip := db.CreateServiceStaticIP(ip, 0, spec)

		glog.Infoln("assigned static ip", ip, "to network interface", serviceip, "requuid", requuid)

		// put ip into db
		err = s.dbIns.CreateServiceStaticIP(ctx, serviceip)
		if err != nil {
			glog.Errorln("CreateServiceStaticIP error", err, "requuid", requuid, serviceip)
			return nil, err
		}

		glog.Infoln("created service static ip in db, requuid", requuid, serviceip)

		unassignedIPs = append(unassignedIPs, serviceip)
	}

	return unassignedIPs, nil
}

func (s *ManageService) createNextIP(ctx context.Context, usedIPs map[string]bool,
	ipnet *net.IPNet, lastIP net.IP, netInterfaceID string) (nextIP net.IP, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// AWS may happen to assign the ip to the newly created EC2 around the same time.
	// allow to retry a few times.
	for i := 0; i < maxRetryCount; i++ {
		// get the next unused IP
		nextIP, err = utils.GetNextIP(usedIPs, ipnet, lastIP)

		// assign to one network interface
		ipstr := nextIP.String()
		err = s.serverIns.AssignStaticIP(ctx, netInterfaceID, ipstr)
		if err == nil {
			glog.Infoln("created next ip", ipstr, "lastIP", lastIP, "network interface", netInterfaceID, "requuid", requuid)
			return nextIP, nil
		}

		glog.Errorln("AssignStaticIP error", err, "ip", ipstr, "network interface", netInterfaceID, "requuid", requuid)
		lastIP = nextIP
	}

	glog.Errorln("not able to create next ip for network interface", netInterfaceID, "requuid", requuid)
	return nil, err
}

func (s *ManageService) getUnassignedIPs(ctx context.Context, sattr *common.ServiceAttr,
	netInterfaces []*server.NetworkInterface, zone string, assignedIPs map[string]string, requuid string) ([]*common.ServiceStaticIP, error) {

	unassignedIPs := []*common.ServiceStaticIP{}

	// check the existing ips.
	for _, netInterface := range netInterfaces {
		for _, ip := range netInterface.PrivateIPs {
			// check if ip is already assigned to member
			if memberName, ok := assignedIPs[ip]; ok {
				glog.Infoln("ip", ip, "is already assigned to member", memberName, "network interface",
					netInterface.InterfaceID, "serviceUUID", sattr.ServiceUUID, "requuid", requuid)
				continue
			}

			// ip is not assigned, check if ip belongs to the service.
			serviceip, err := s.dbIns.GetServiceStaticIP(ctx, ip)
			if err != nil {
				if err != db.ErrDBRecordNotFound {
					glog.Errorln("GetServiceStaticIP error", err, "ip", ip, "serviceUUID", sattr.ServiceUUID, "requuid", requuid)
					return nil, err
				}

				// ip does not belong to any service.
				glog.Infoln("ip", ip, "is available, serviceUUID", sattr.ServiceUUID, "requuid", requuid)

				// put ip into db
				spec := db.CreateStaticIPSpec(sattr.ServiceUUID, zone, netInterface.ServerInstanceID, netInterface.InterfaceID)
				serviceip = db.CreateServiceStaticIP(ip, 0, spec)
				err = s.dbIns.CreateServiceStaticIP(ctx, serviceip)
				if err != nil {
					glog.Errorln("CreateServiceStaticIP error", err, "requuid", requuid, serviceip)
					return nil, err
				}

				glog.Infoln("created service static ip in db, requuid", requuid, serviceip)

				unassignedIPs = append(unassignedIPs, serviceip)
				continue
			}

			// get ServiceStaticIP from db
			if serviceip.Spec.ServiceUUID == sattr.ServiceUUID {
				// ip belongs to the service but not assigned to any member yet.
				glog.Infoln("static ip belongs to the current service", sattr.ServiceUUID, "requuid", requuid, serviceip)

				unassignedIPs = append(unassignedIPs, serviceip)
				continue
			}

			glog.Infoln("static ip not belongs to the current service", sattr.ServiceUUID, "requuid", requuid, serviceip)
		}
	}

	glog.Infoln("get", len(unassignedIPs), "unassigned ips, service", sattr.ServiceUUID, "requuid", requuid)
	return unassignedIPs, nil
}
