package openmanagedockervolume

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/go-plugins-helpers/volume"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/containersvc"
	"github.com/cloudstax/openmanage/db"
	"github.com/cloudstax/openmanage/dns"
	"github.com/cloudstax/openmanage/server"
	"github.com/cloudstax/openmanage/utils"
)

const (
	defaultRoot   = "/mnt/sc"
	defaultFStype = "ext4"
)

var defaultMountOptions = []string{"discard", "defaults"}

type driverVolume struct {
	volume *common.Volume
	// the number of connections to this volume. This is used for mountFrom.
	// for example, app binds 2 containers, one as storage container, the other
	// mounts the volume from the stroage container.
	// docker will call mount/unmount for the same volume when start/stop both containers.
	connections int
}

// OpenManageVolumeDriver is the docker volume plugin.
type OpenManageVolumeDriver struct {
	// the default root dir, volume will be mounted to /mnt/sc/serviceUUID
	root string

	// the local mounted volumes, key: serviceUUID
	volumes map[string]*driverVolume
	// lock to serialize the operations
	volumesLock *sync.Mutex

	// db instance to access Volume
	dbIns db.DB

	// dns service
	dnsIns dns.DNS

	// server instance (ec2 on AWS) to attach/detach volume
	serverIns  server.Server
	serverInfo server.Info

	// container service
	containersvcIns containersvc.ContainerSvc
	containerInfo   containersvc.Info
}

// NewVolumeDriver creates a new OpenManageVolumeDriver instance
func NewVolumeDriver(dbIns db.DB, dnsIns dns.DNS, serverIns server.Server, serverInfo server.Info,
	containersvcIns containersvc.ContainerSvc, containerInfo containersvc.Info) *OpenManageVolumeDriver {
	d := &OpenManageVolumeDriver{
		root:            defaultRoot,
		volumes:         map[string]*driverVolume{},
		volumesLock:     &sync.Mutex{},
		dbIns:           dbIns,
		dnsIns:          dnsIns,
		serverIns:       serverIns,
		serverInfo:      serverInfo,
		containersvcIns: containersvcIns,
		containerInfo:   containerInfo,
	}
	return d
}

// Create checks if volume is mounted or service exists in DB
func (d *OpenManageVolumeDriver) Create(r volume.Request) volume.Response {
	serviceUUID := r.Name

	// check if volume is in cache
	vol := d.getMountedVolume(serviceUUID)
	if vol != nil {
		glog.Infoln("Create, volume in cache", vol, "for serviceUUID", serviceUUID)
		return volume.Response{}
	}

	if utils.IsControlDBService(serviceUUID) {
		// controldb service volume, return success
		glog.Infoln("Create, controldb service volume", serviceUUID)
		return volume.Response{}
	}

	ctx := context.Background()

	// volume not in cache, check if service exists in DB
	_, err := d.dbIns.GetServiceAttr(ctx, serviceUUID)
	if err != nil {
		errmsg := fmt.Sprintf("Create, GetServiceAttr error %s req %s", err, r)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	glog.Infoln("Create request done", r)
	return volume.Response{}
}

// Remove simply tries to remove mounted volume
func (d *OpenManageVolumeDriver) Remove(r volume.Request) volume.Response {
	d.removeMountedVolume(r.Name)
	glog.Infoln("Remove request done", r)
	return volume.Response{}
}

// Get returns the mountPath if volume is mounted
func (d *OpenManageVolumeDriver) Get(r volume.Request) volume.Response {
	serviceUUID := r.Name
	mountPath := d.mountpoint(serviceUUID)

	vol := d.getMountedVolume(serviceUUID)
	if vol != nil {
		glog.Infoln("volume is mounted", r)
		return volume.Response{Volume: &volume.Volume{Name: serviceUUID, Mountpoint: mountPath}}
	}

	// Q: if volume is not mounted locally, should we fetch from DB?
	errmsg := fmt.Sprintf("service %s volume is not mounted on %s", serviceUUID, mountPath)
	glog.Infoln(errmsg)
	return volume.Response{Err: errmsg}
}

// Path returns the volume mountPath
func (d *OpenManageVolumeDriver) Path(r volume.Request) volume.Response {
	mountPath := d.mountpoint(r.Name)
	return volume.Response{Mountpoint: mountPath}
}

// List lists all mounted volumes
func (d *OpenManageVolumeDriver) List(r volume.Request) volume.Response {
	d.volumesLock.Lock()
	defer d.volumesLock.Unlock()

	var vols []*volume.Volume
	for _, v := range d.volumes {
		svcuuid := v.volume.ServiceUUID
		vols = append(vols, &volume.Volume{Name: svcuuid, Mountpoint: d.mountpoint(svcuuid)})
	}

	glog.Infoln("list", len(d.volumes), "volumes")
	return volume.Response{Volumes: vols}
}

// Capabilities is always local
func (d *OpenManageVolumeDriver) Capabilities(volume.Request) volume.Response {
	return volume.Response{
		Capabilities: volume.Capability{Scope: "local"},
	}
}

func (d *OpenManageVolumeDriver) genReqUUID(serviceUUID string) string {
	t := time.Now().Unix()
	requuid := d.serverInfo.GetLocalHostname() + common.NameSeparator +
		serviceUUID + common.NameSeparator + strconv.FormatInt(t, 10)
	return requuid
}

// Mount mounts the volume to the host.
func (d *OpenManageVolumeDriver) Mount(r volume.MountRequest) volume.Response {
	glog.Infoln("handle Mount ", r)

	// r.Name is serviceUUID
	serviceUUID := r.Name
	mountPath := d.mountpoint(serviceUUID)

	ctx := context.Background()
	requuid := d.genReqUUID(serviceUUID)
	ctx = utils.NewRequestContext(ctx, requuid)

	// if volume is already mounted, return
	vol := d.getMountedVolumeAndIncRef(serviceUUID)
	if vol != nil {
		glog.Infoln("Mount done, volume", vol, "is already mounted")
		return volume.Response{Mountpoint: mountPath}
	}

	var serviceAttr *common.ServiceAttr
	var err error
	// check if the volume is for the controldb service
	if utils.IsControlDBService(serviceUUID) {
		// get the hostedZoneID
		domainName := dns.GenDefaultDomainName(d.containerInfo.GetContainerClusterID())
		vpcID := d.serverInfo.GetLocalVpcID()
		vpcRegion := d.serverInfo.GetLocalRegion()
		private := true
		hostedZoneID, err := d.dnsIns.GetOrCreateHostedZoneIDByName(ctx, domainName, vpcID, vpcRegion, private)
		if err != nil {
			errmsg := fmt.Sprintf("GetOrCreateHostedZoneIDByName error %s domain %s vpc %s %s, service %s, requuid %s",
				err, domainName, vpcID, vpcRegion, serviceUUID, requuid)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}
		glog.Infoln("get hostedZoneID", hostedZoneID, "for service", serviceUUID,
			"domain", domainName, "vpc", vpcID, vpcRegion, "requuid", requuid)

		// detach the volume from the last owner
		err = d.detachControlDBVolume(ctx, utils.GetControlDBVolumeID(serviceUUID), requuid)
		if err != nil {
			errmsg := fmt.Sprintf("detachControlDBVolume error %s service %s, requuid %s", err, serviceUUID, requuid)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}

		// get the volume and serviceAttr for the controldb service
		vol, serviceAttr = d.getControlDBVolumeAndServiceAttr(serviceUUID, domainName, hostedZoneID)

		glog.Infoln("get controldb service volume", vol, "serviceAttr", serviceAttr, "requuid", requuid)
	} else {
		// the application service, talks with the controldb service for volume
		// get cluster and service names by serviceUUID
		serviceAttr, err = d.dbIns.GetServiceAttr(ctx, serviceUUID)
		if err != nil {
			errmsg := fmt.Sprintf("GetServiceAttr error %s serviceUUID %s requuid %s", err, serviceUUID, requuid)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}
		glog.Infoln("get application service attr", serviceAttr, "requuid", requuid)

		// get one volume for this mount
		vol, err = d.getVolumeForTask(ctx, serviceAttr, requuid)
		if err != nil {
			errmsg := fmt.Sprintf("Mount failed, get volume error %s, serviceUUID %s, requuid %s", err, serviceUUID, requuid)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}
		glog.Infoln("get application service volume", vol, "requuid", requuid)
	}

	// update DNS, this MUST be done after volume is assigned to this node (updated in DB).
	if serviceAttr.HasStrictMembership {
		dnsName := dns.GenDNSName(vol.MemberName, serviceAttr.DomainName)
		hostname := d.serverInfo.GetLocalHostname()
		err = d.dnsIns.UpdateServiceDNSRecord(ctx, dnsName, hostname, serviceAttr.HostedZoneID)
		if err != nil {
			errmsg := fmt.Sprintf("UpdateServiceDNSRecord error %s service %s volume %s, requuid %s",
				err, serviceAttr, vol, requuid)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}

		// wait till DNS record lookup returns local hostname. This is to make sure DB doesn't
		// get the invalid old host at the replication initialization.
		// TODO find way to update the local dns cache to avoid the potential long wait time.
		dnsHostname, err := d.dnsIns.WaitDNSRecordUpdated(ctx, dnsName, hostname, serviceAttr.HostedZoneID)
		if err != nil {
			errmsg := fmt.Sprintf("WaitDNSRecordUpdated error %s, expect hostname %s got %s, service %s volume %s, requuid %s",
				err, hostname, dnsHostname, serviceAttr, vol, requuid)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}
	}

	// create the mountPath if necessary
	err = d.checkAndCreateMountPath(mountPath)
	if err != nil {
		errmsg := fmt.Sprintf("Mount failed, check/create mount path %s error %s, requuid %s", mountPath, err, requuid)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	// attach volume to host
	glog.Infoln("attach volume", vol, "requuid", requuid)
	err = d.attachVolume(ctx, *vol, requuid)
	if err != nil {
		errmsg := fmt.Sprintf("Mount failed, attach volume error %s %s, requuid %s", err, vol, requuid)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	// format dev if necessary
	formatted := d.isFormatted(vol.DeviceName)
	if !formatted {
		err = d.formatFS(vol.DeviceName)
		if err != nil {
			errmsg := fmt.Sprintf("Mount failed, format device error %s Volume %s, requuid %s", err, vol, requuid)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}
	}

	// mount
	err = d.mountFS(vol.DeviceName, mountPath)
	if err != nil {
		errmsg := fmt.Sprintf("Mount failed, mount fs error %s volume %s, requuid %s", err, vol, requuid)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	// create the service config file if necessary
	err = d.createConfigFile(ctx, mountPath, vol, requuid)
	if err != nil {
		errmsg := fmt.Sprintf("create the config file error %s, service %s, requuid %s", err, serviceAttr, requuid)
		glog.Errorln(errmsg)

		// umount fs
		err = d.unmountFS(mountPath)
		if err == nil {
			// successfully umount, return error for the Mount()
			d.removeMountPath(mountPath)
			// detach volume, ignore the possible error.
			err = d.serverIns.DetachVolume(ctx, vol.VolumeID, d.serverInfo.GetLocalInstanceID(), vol.DeviceName)
			glog.Errorln("detached volume", vol, "error", err, "requuid", requuid)
			return volume.Response{Err: errmsg}
		}

		// umount failed, has to return successs. or else, volume will not be able to
		// be detached. Expect the container will check the config file and exits,
		// so Unmount() will be called and the task will be rescheduled.
		glog.Errorln("umount fs error", err, mountPath, serviceAttr, "requuid", requuid)
	}

	// cache mounted volume
	d.addMountedVolume(serviceUUID, vol)

	glog.Infoln("Mount done, volume", vol, "to", mountPath, "requuid", requuid)
	return volume.Response{Mountpoint: mountPath}
}

// Unmount the volume.
func (d *OpenManageVolumeDriver) Unmount(r volume.UnmountRequest) volume.Response {
	serviceUUID := r.Name
	mountPath := d.mountpoint(serviceUUID)

	ctx := context.Background()
	requuid := d.genReqUUID(serviceUUID)
	ctx = utils.NewRequestContext(ctx, requuid)

	// directly hold lock for Unmount, not easy to do in separate function.
	// if unmountFS fails, should not remove the volume from cache.
	d.volumesLock.Lock()
	defer d.volumesLock.Unlock()

	glog.Infoln("handle Unmount ", r, "requuid", requuid)

	// check if volume is mounted
	vol, ok := d.volumes[serviceUUID]
	if !ok {
		// if volume driver restarts, the in-memory info will be lost. check and umount the path.
		// This is ok even if some container is still using the device. Container internally
		// mounts the device directly. The volume detach will fail, as the device is still mounted.
		// So no other container could attach the device before the current container exits.
		d.checkAndUnmountNotCachedVolume(mountPath)

		errmsg := fmt.Sprintf("Unmount failed, volume is not mounted %s requuid %s", r, requuid)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	// volume mounted, check number of connections
	glog.Infoln("volume", vol.volume, "connections", vol.connections, "requuid", requuid)
	if (vol.connections - 1) > 0 {
		// update the volumes
		vol.connections--
		glog.Infoln("Unmount done, some container still uses the volume",
			vol.volume, "connections", vol.connections, "requuid", requuid)
		return volume.Response{}
	}

	// last container for the volume, umount fs
	glog.Infoln("last container for the volume", r, "unmount fs", mountPath, "requuid", requuid)
	err := d.unmountFS(mountPath)
	if err != nil {
		errmsg := fmt.Sprintf("Unmount failed, umount fs %s error %s requuid %s", mountPath, err, requuid)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	// successfully umount fs, remove mounted volume
	delete(d.volumes, serviceUUID)

	// remove mount path and detach
	// ignore the possible errors of below operations
	glog.Infoln("delete mount path", mountPath, r, "requuid", requuid)
	d.removeMountPath(mountPath)

	// detach volume, ignore the possible error.
	err = d.serverIns.DetachVolume(ctx, vol.volume.VolumeID,
		d.serverInfo.GetLocalInstanceID(), vol.volume.DeviceName)
	glog.Infoln("detached volume", vol.volume, "serverID",
		d.serverInfo.GetLocalInstanceID(), "error", err, "requuid", requuid)

	// don't wait till detach complete, detach may take long time and cause container stop timeout.
	// let the new task attachment handles it. If detach times out at new task start phase,
	// ECS will simply reschedule the task.

	glog.Infoln("Unmount done", r, "requuid", requuid)
	return volume.Response{}
}

func (d *OpenManageVolumeDriver) createConfigFile(ctx context.Context, mountPath string, vol *common.Volume, requuid string) error {
	// if the service does not need the config file, return
	if len(vol.Configs) == 0 {
		glog.Infoln("no config file", vol, "requuid", requuid)
		return nil
	}

	// check the md5 first.
	// The config file content may not be small. For example, the default cassandra.yaml is 45K.
	// The config files are rarely updated after creation. No need to read the content over.
	for _, cfg := range vol.Configs {
		// check and create the config file if necessary
		fpath := filepath.Join(mountPath, cfg.FileName)
		exist, err := utils.IsFileExist(fpath)
		if err != nil {
			glog.Errorln("check the config file error", err, fpath, "requuid", requuid, vol)
			return err
		}

		if exist {
			// the config file exists, check whether it is the same with the config in DB.
			fdata, err := ioutil.ReadFile(fpath)
			if err != nil {
				glog.Errorln("read the config file error", err, fpath, "requuid", requuid, vol)
				return err
			}

			fmd5 := md5.Sum(fdata)
			fmd5str := hex.EncodeToString(fmd5[:])
			if fmd5str == cfg.FileMD5 {
				glog.Infoln("the config file has the latest content", fpath, "requuid", requuid, vol)
				// check next config file
				continue
			}
			// the config content is changed, update the config file
			glog.Infoln("the config file changed, update it", fpath, "requuid", requuid, vol)
		}

		// the config file not exists or content is changed
		// read the content
		cfgFile, err := d.dbIns.GetConfigFile(ctx, vol.ServiceUUID, cfg.FileID)
		if err != nil {
			glog.Errorln("GetConfigFile error", err, fpath, "requuid", requuid, vol)
			return err
		}

		data := []byte(cfgFile.Content)
		err = d.writeFile(fpath, data, utils.DefaultFileMode)
		if err != nil {
			glog.Errorln("write the config file error", err, fpath, "requuid", requuid, vol)
			return err
		}

		glog.Infoln("write the config file done for service", fpath, "requuid", requuid, vol)
	}

	return nil
}

func (d *OpenManageVolumeDriver) writeFile(filename string, data []byte, perm os.FileMode) error {
	// TODO create a tmp file and rename
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_SYNC, perm)
	if err != nil {
		return err
	}
	n, err := f.Write(data)
	if err == nil && n < len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	if err1 := f.Close(); err == nil {
		err = err1
	}
	return err
}

func (d *OpenManageVolumeDriver) checkAndUnmountNotCachedVolume(mountPath string) {
	// check if the mount path exists
	_, err := os.Stat(mountPath)
	if err != nil {
		if os.IsNotExist(err) {
			glog.Errorln("unmount, mountPath not exist", mountPath)
			return
		}
		glog.Errorln("unmount, Stat error", err, mountPath)
		return
	}

	// mount path exists, check if device is mounted to it.
	if !d.isDeviceMountToPath(mountPath) {
		// no device mounted to the mountPath, directly return
		return
	}

	// device mounted to the mountPath, umount it
	err = d.unmountFS(mountPath)
	if err != nil {
		glog.Errorln("umount error", err, mountPath)
		return
	}

	glog.Infoln("umount success, delete mount path", mountPath)
	d.removeMountPath(mountPath)
}

// getVolumeForTask will get one idle volume, not used or owned by down task,
// detach it and update the new owner in DB.
func (d *OpenManageVolumeDriver) getVolumeForTask(ctx context.Context, serviceAttr *common.ServiceAttr, requuid string) (volume *common.Volume, err error) {
	// find one idle volume
	vol, err := d.findIdleVolume(ctx, serviceAttr, requuid)
	if err != nil {
		glog.Errorln("findIdleVolume error", err, "requuid", requuid, "service", serviceAttr)
		return nil, err
	}

	// detach volume from the last owner
	err = d.detachVolumeFromLastOwner(ctx, vol.VolumeID, vol.ServerInstanceID, vol.DeviceName, requuid)
	if err != nil {
		glog.Errorln("detachVolumeFromLastOwner error", err, "requuid", requuid, "volume", vol)
		return nil, err
	}

	glog.Infoln("got and detached volume", vol, "requuid", requuid)

	// get localTaskID
	localTaskID, err := d.containersvcIns.GetServiceTask(ctx, serviceAttr.ClusterName,
		serviceAttr.ServiceName, d.containerInfo.GetLocalContainerInstanceID())
	if err != nil {
		glog.Errorln("get local task id error", err, "requuid", requuid,
			"containerInsID", d.containerInfo.GetLocalContainerInstanceID(), "service attr", serviceAttr)
		return nil, err
	}

	// update volume in db.
	// TODO if there are concurrent failures, multiple tasks may select the same idle volume.
	// now, the task will fail and be scheduled again. better have some retries here.
	newVol := db.UpdateVolume(vol, localTaskID, d.containerInfo.GetLocalContainerInstanceID(), d.serverInfo.GetLocalInstanceID())
	err = d.dbIns.UpdateVolume(ctx, vol, newVol)
	if err != nil {
		glog.Errorln("UpdateVolume error", err, "requuid", requuid, "newVol", newVol, "oldVol", vol)
		return nil, err
	}

	glog.Infoln("updated volume", vol, "to", localTaskID, "requuid", requuid,
		d.containerInfo.GetLocalContainerInstanceID(), d.serverInfo.GetLocalInstanceID())

	return newVol, nil
}

func (d *OpenManageVolumeDriver) detachControlDBVolume(ctx context.Context, volID string, requuid string) error {
	volInfo, err := d.serverIns.GetVolumeInfo(ctx, volID)
	if err != nil {
		glog.Errorln("GetVolumeInfo error", err, volID, "requuid", requuid)
		return err
	}

	// sanity check
	if volInfo.State == server.VolumeStateDetaching || volInfo.State == server.VolumeStateAttaching {
		if volInfo.Device != d.serverIns.GetControlDBDeviceName() {
			errmsg := fmt.Sprintf("sanity error, expect controldb device %s, got %s, %s, requuid %s",
				d.serverIns.GetControlDBDeviceName(), volInfo.Device, volInfo, requuid)
			glog.Errorln(errmsg)
			return errors.New(errmsg)
		}
	}

	glog.Infoln("get controldb volume", volInfo, "requuid", requuid)

	if len(volInfo.AttachInstanceID) != 0 {
		// volume was attached to some server, detach it
		return d.detachVolumeFromLastOwner(ctx, volInfo.VolID, volInfo.AttachInstanceID, volInfo.Device, requuid)
	}
	return nil
}

func (d *OpenManageVolumeDriver) getControlDBVolumeAndServiceAttr(serviceUUID string, domainName string, hostedZoneID string) (*common.Volume, *common.ServiceAttr) {
	mtime := time.Now().UnixNano()
	// construct the vol for the controldb service
	// could actually get the taksID, as only one controldb task is running at any time.
	// but taskID is useless for the controldb service, so simply use empty taskID.
	taskID := ""
	vol := db.CreateVolume(serviceUUID, utils.GetControlDBVolumeID(serviceUUID), mtime,
		d.serverIns.GetControlDBDeviceName(), d.serverInfo.GetLocalAvailabilityZone(),
		taskID, d.containerInfo.GetLocalContainerInstanceID(),
		d.serverInfo.GetLocalInstanceID(), common.ControlDBServiceName, nil)

	// construct the serviceAttr for the controldb service
	// taskCounts and volSizeGB are useless for the controldb service
	taskCounts := int64(1)
	volSizeGB := int64(0)
	hasMembership := true
	attr := db.CreateServiceAttr(serviceUUID, common.ServiceStatusActive, mtime, taskCounts,
		volSizeGB, d.containerInfo.GetContainerClusterID(), common.ControlDBServiceName,
		d.serverIns.GetControlDBDeviceName(), hasMembership, domainName, hostedZoneID)

	return vol, attr
}

func (d *OpenManageVolumeDriver) findIdleVolume(ctx context.Context, serviceAttr *common.ServiceAttr, requuid string) (volume *common.Volume, err error) {
	// list all tasks of the service and get taskID of the current task,
	// assume only one task on one node for one service
	// docker swarm: docker node ps -f name=serviceName node
	// aws ecs: ListTasks with cluster, serviceName, containerInstance
	//   - driver could get the local node ServerInstanceID by curl -s http://169.254.169.254/latest/metadata/instance-id,
	//   - get containerInstanceID by 2 steps: 1) curl -s http://169.254.169.254/latest/meta-data/hostname,
	//      2) curl http://hostname:51678/v1/metadata
	// mesos marathon: GET /v2/apps/{appId}/tasks: List running tasks for app
	// https://mesosphere.github.io/marathon/docs/rest-api.html
	// k8: kubectl get pods -l app=nginx
	// http://kubernetes.io/docs/tutorials/stateless-application/run-stateless-application-deployment/
	taskIDs, err := d.containersvcIns.ListActiveServiceTasks(ctx, serviceAttr.ClusterName, serviceAttr.ServiceName)
	if err != nil {
		glog.Errorln("ListActiveServiceTasks error", err, "service attr", serviceAttr, "requuid", requuid)
		return nil, err
	}

	// list all volumes from DB, e.dbIns.ListServiceVolumes(service.ServiceUUID)
	volumes, err := d.dbIns.ListVolumes(ctx, serviceAttr.ServiceUUID)
	if err != nil {
		glog.Errorln("ListVolumes error", err, "service", serviceAttr, "requuid", requuid)
		return nil, err
	}

	// find one idle volume, the volume's taskID not in the task list
	for _, volume := range volumes {
		// TODO better to get all idle volumes and randomly select one, as multiple tasks may
		// try to find the idle volume around the same time.
		_, ok := taskIDs[volume.TaskID]
		if !ok {
			glog.Infoln("find idle volume", volume, "service", serviceAttr, "requuid", requuid)
			return volume, nil
		}

		// volume's task is still running
		// It is weird, but when testing mongodb container with volume, the same task
		// may mount and umount the volume, then try to mount again. Don't know why.
		if volume.ContainerInstanceID == d.containerInfo.GetLocalContainerInstanceID() {
			glog.Infoln("volume task is still running on local instance, reuse it", volume, "requuid", requuid)
			return volume, nil
		}

		glog.V(0).Infoln("volume", volume, "in use, service", serviceAttr, "requuid", requuid)
	}

	glog.Errorln("service has no idle volume", serviceAttr, "requuid", requuid)
	return nil, common.ErrInternal
}

func (d *OpenManageVolumeDriver) mountpoint(serviceUUID string) string {
	return filepath.Join(d.root, serviceUUID)
}

func (d *OpenManageVolumeDriver) attachVolume(ctx context.Context, vol common.Volume, requuid string) error {
	// attach volume to host
	err := d.serverIns.AttachVolume(ctx, vol.VolumeID, vol.ServerInstanceID, vol.DeviceName)
	if err != nil {
		glog.Errorln("failed to AttachVolume", vol, "error", err, "requuid", requuid)
		return err
	}

	// wait till attach complete
	err = d.serverIns.WaitVolumeAttached(ctx, vol.VolumeID)
	if err != nil {
		// TODO EBS volume attach may stuck. Is below ref still the case? Maybe not?
		// ref discussion: https://forums.aws.amazon.com/message.jspa?messageID=235023
		// caller should handle it. For example, get volume state, detach it if still
		// at attaching state, and try attach again.
		glog.Errorln("volume", vol, "is still not attached, requuid", requuid)
		return err
	}

	glog.Infoln("attached volume", vol, "requuid", requuid)
	return nil
}

func (d *OpenManageVolumeDriver) detachVolumeFromLastOwner(ctx context.Context, volID string,
	serverInsID string, device string, requuid string) error {
	// if volume is not attached to any instance, return
	if serverInsID == db.DefaultServerInstanceID {
		return nil
	}

	// check volume state
	state, err := d.serverIns.GetVolumeState(ctx, volID)
	if err != nil {
		glog.Errorln("GetVolumeState error", err, "volume", volID,
			"ServerInstanceID", serverInsID, "device", device, "requuid", requuid)
		return err
	}

	glog.Infoln("GetVolumeState", state, "volume", volID, "requuid", requuid)

	switch state {
	case server.VolumeStateAvailable:
		// volume is available, return
		return nil
	case server.VolumeStateInUse:
		// volume is in-use, detach it from the old owner
		// InUse may have 2 attachment state: attached or detaching.
		// Test DetachVolume right after DetachVolume call (volume is at detaching state),
		// see ec2/example, EBS returns success. So ok to directly call DetachVolume here.
		err = d.serverIns.DetachVolume(ctx, volID, serverInsID, device)
		//if err != nil && err != server.ErrVolumeIncorrectState {
		if err != nil {
			glog.Errorln("DetachVolume error", err, "volume", volID,
				"ServerInstanceID", serverInsID, "device", device, "requuid", requuid)
			return err
		}
		// wait till volume is detached
		err = d.serverIns.WaitVolumeDetached(ctx, volID)
		if err != nil {
			glog.Errorln("WaitVolumeDetached error", err, "volume", volID,
				"ServerInstanceID", serverInsID, "device", device, "requuid", requuid)
			return err
		}
		glog.Infoln("volume detached", volID, "ServerInstanceID", serverInsID, "device", device, "requuid", requuid)
		return nil
	case server.VolumeStateDetaching:
		// wait till volume is detached
		err = d.serverIns.WaitVolumeDetached(ctx, volID)
		if err != nil {
			glog.Errorln("WaitVolumeDetached error", err, "volume", volID,
				"ServerInstanceID", serverInsID, "device", device, "requuid", requuid)
			return err
		}
		glog.Infoln("volume detached", volID, "ServerInstanceID", serverInsID, "device", device, "requuid", requuid)
		return nil
	default:
		glog.Errorln("bad volume state", state, volID, "ServerInstanceID", serverInsID,
			"device", device, "requuid", requuid)
		return common.ErrInternal
	}
	return nil
}

func (d *OpenManageVolumeDriver) getMountedVolume(serviceUUID string) *common.Volume {
	d.volumesLock.Lock()
	defer d.volumesLock.Unlock()

	vol, ok := d.volumes[serviceUUID]
	if !ok {
		return nil
	}
	return vol.volume
}

func (d *OpenManageVolumeDriver) getMountedVolumeAndIncRef(serviceUUID string) *common.Volume {
	d.volumesLock.Lock()
	defer d.volumesLock.Unlock()

	vol, ok := d.volumes[serviceUUID]
	if !ok {
		return nil
	}

	vol.connections++

	glog.Infoln("get mounted volume", vol.volume, "connections", vol.connections)
	return vol.volume
}

func (d *OpenManageVolumeDriver) addMountedVolume(serviceUUID string, vol *common.Volume) {
	d.volumesLock.Lock()
	defer d.volumesLock.Unlock()

	d.volumes[serviceUUID] = &driverVolume{volume: vol, connections: 1}
}

func (d *OpenManageVolumeDriver) removeMountedVolume(serviceUUID string) {
	d.volumesLock.Lock()
	defer d.volumesLock.Unlock()

	vol, ok := d.volumes[serviceUUID]
	if !ok {
		glog.Errorln("volume is not mounted", serviceUUID)
		return
	}

	if vol.connections <= 0 {
		glog.Infoln("remove mounted volume", serviceUUID)
		delete(d.volumes, serviceUUID)
		return
	}
	glog.Infoln("volume is still in use, not remove", vol.volume, "connections", vol.connections)
}

func (d *OpenManageVolumeDriver) checkAndCreateMountPath(mountPath string) error {
	fi, err := os.Lstat(mountPath)
	if err != nil {
		if !os.IsNotExist(err) {
			glog.Errorln("Lstat mount path", mountPath, "error", err)
			return err
		}

		// not exist, create the mount path
		err = os.MkdirAll(mountPath, 0755)
		if err != nil {
			glog.Errorln("failed to create mount path", mountPath, "error", err)
			return err
		}

		glog.Infoln("created mount path", mountPath)
	} else {
		if fi != nil && !fi.IsDir() {
			glog.Errorln("mount path", mountPath, "already exist and not a directory")
			return errors.New("mount path already exist and not a directory")
		}
		glog.Infoln("mountPath exists", mountPath)
	}

	return nil
}

func (d *OpenManageVolumeDriver) removeMountPath(mountPath string) error {
	err := os.Remove(mountPath)
	if err != nil {
		glog.Errorln("failed to remove mountPath", mountPath, "error", err)
		return err
	}
	return nil
}

func (d *OpenManageVolumeDriver) mountFS(source string, target string) error {
	args := d.getMountArgs(source, target, defaultFStype, defaultMountOptions)

	command := exec.Command(args[0], args[1:]...)
	output, err := command.CombinedOutput()
	if err != nil {
		glog.Errorln("mount failed, arguments", args,
			"output", string(output[:]), "error", err)
		return err
	}

	return err
}

func (d *OpenManageVolumeDriver) getMountArgs(source, target, fstype string, options []string) []string {
	var args []string
	args = append(args, "mount")

	if len(fstype) > 0 {
		args = append(args, "-t", fstype)
	}

	if len(options) > 0 {
		args = append(args, "-o", strings.Join(options, ","))
	}

	args = append(args, source)
	args = append(args, target)

	return args
}

func (d *OpenManageVolumeDriver) unmountFS(target string) error {
	var args []string
	args = append(args, "umount", target)

	command := exec.Command(args[0], args[1:]...)
	output, err := command.CombinedOutput()
	if err != nil {
		glog.Errorln("umount failed, arguments", args,
			"output", string(output[:]), "error", err)
		return err
	}

	return nil
}

// Format formats the source device to ext4
func (d *OpenManageVolumeDriver) formatFS(source string) error {
	var args []string
	args = append(args, "mkfs.ext4", source)
	command := exec.Command(args[0], args[1:]...)
	output, err := command.CombinedOutput()
	if err != nil {
		glog.Errorln("mkfs.ext4 failed, arguments", args, "output", string(output[:]), "error", err)
		return err
	}

	glog.Infoln("formatted device", args)

	return nil
}

func (d *OpenManageVolumeDriver) isFormatted(source string) bool {
	//d.lsblk()

	var args []string
	args = append(args, "blkid", source)

	command := exec.Command(args[0], args[1:]...)
	output, err := command.CombinedOutput()
	if err != nil {
		glog.Infoln("volume is not formatted, arguments", args, "output", string(output[:]), "error", err)
		return false
	}

	glog.Infoln("volume is formatted, arguments", args, "output", string(output[:]))
	return true
}

func (d *OpenManageVolumeDriver) lsblk() {
	var args []string
	args = append(args, "lsblk")

	command := exec.Command(args[0], args[1:]...)
	output, err := command.CombinedOutput()
	glog.Infoln("arguments", args, "output", string(output[:]), "error", err)
}

func (d *OpenManageVolumeDriver) isDeviceMountToPath(mountPath string) bool {
	// check whether there is device mounted to the mountPath
	var args []string
	args = append(args, "lsblk")

	command := exec.Command(args[0], args[1:]...)
	output, err := command.CombinedOutput()
	if err != nil {
		ostr := ""
		if output != nil {
			ostr = string(output[:])
		}
		glog.Infoln("lsblk error", err, mountPath, "output", ostr)
		return false
	}

	if len(output) == 0 {
		glog.Errorln("lsblk, no output", mountPath)
		return false
	}

	str := string(output[:])
	glog.Infoln("lsblk output", str, mountPath)

	return strings.Contains(str, mountPath)
}
