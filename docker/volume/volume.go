package firecampdockervolume

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
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

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/server"
	"github.com/cloudstax/firecamp/utils"
)

const (
	defaultRoot   = "/mnt/" + common.SystemName
	defaultFSType = "xfs"
	defaultMkfs   = "mkfs.xfs"
	tmpfileSuffix = ".tmp"
)

var defaultMountOptions = []string{"discard", "defaults"}

type driverVolume struct {
	member *common.ServiceMember
	// the number of connections to this volume. This is used for mountFrom.
	// for example, app binds 2 containers, one as storage container, the other
	// mounts the volume from the stroage container.
	// docker will call mount/unmount for the same volume when start/stop both containers.
	connections int
}

// FireCampVolumeDriver is the docker volume plugin.
type FireCampVolumeDriver struct {
	// the default root dir, volume will be mounted to /mnt/firecamp/serviceUUID
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

// NewVolumeDriver creates a new FireCampVolumeDriver instance
func NewVolumeDriver(dbIns db.DB, dnsIns dns.DNS, serverIns server.Server, serverInfo server.Info,
	containersvcIns containersvc.ContainerSvc, containerInfo containersvc.Info) *FireCampVolumeDriver {
	d := &FireCampVolumeDriver{
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
func (d *FireCampVolumeDriver) Create(r volume.Request) volume.Response {
	glog.Infoln("Create volume", r)

	serviceUUID := r.Name

	// check if volume is in cache
	member := d.getMountedVolume(serviceUUID)
	if member != nil {
		glog.Infoln("Create, volume in cache", member, "for serviceUUID", serviceUUID)
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
func (d *FireCampVolumeDriver) Remove(r volume.Request) volume.Response {
	d.removeMountedVolume(r.Name)
	glog.Infoln("Remove request done", r)
	return volume.Response{}
}

// Get returns the mountPath if volume is mounted
func (d *FireCampVolumeDriver) Get(r volume.Request) volume.Response {
	glog.Infoln("Get volume", r)

	var err error
	serviceUUID := r.Name
	if containersvc.VolumeSourceHasTaskSlot(r.Name) {
		serviceUUID, _, err = containersvc.ParseVolumeSource(r.Name)
		if err != nil {
			errmsg := fmt.Sprintf("invalid volume name %s, error: %s", r, err)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}
	}

	mountPath := d.mountpoint(serviceUUID)

	member := d.getMountedVolume(serviceUUID)
	if member != nil {
		glog.Infoln("volume is mounted", r)
		return volume.Response{Volume: &volume.Volume{Name: r.Name, Mountpoint: mountPath}}
	}

	// volume is not mounted locally, return success. If the service doesn't exist,
	// the later mount will fail.
	// could not return error here, or else, amazon-ecs-agent may not start the container.
	glog.Infoln("volume is not mounted for service", serviceUUID)
	return volume.Response{Volume: &volume.Volume{Name: r.Name}}
}

// Path returns the volume mountPath
func (d *FireCampVolumeDriver) Path(r volume.Request) volume.Response {
	glog.Infoln("Path volume", r)

	var err error
	serviceUUID := r.Name
	if containersvc.VolumeSourceHasTaskSlot(r.Name) {
		serviceUUID, _, err = containersvc.ParseVolumeSource(r.Name)
		if err != nil {
			errmsg := fmt.Sprintf("invalid volume name %s, error: %s", r, err)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}
	}

	mountPath := d.mountpoint(serviceUUID)
	return volume.Response{Mountpoint: mountPath}
}

// List lists all mounted volumes
func (d *FireCampVolumeDriver) List(r volume.Request) volume.Response {
	d.volumesLock.Lock()
	defer d.volumesLock.Unlock()

	var vols []*volume.Volume
	for _, v := range d.volumes {
		svcuuid := v.member.ServiceUUID
		memberIndex, err := utils.GetServiceMemberIndex(v.member.MemberName)
		if err != nil {
			errmsg := fmt.Sprintf("invalid service member name %s, error %s", v.member, err)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}

		name := containersvc.GenVolumeSourceName(svcuuid, memberIndex)
		vols = append(vols, &volume.Volume{Name: name, Mountpoint: d.mountpoint(svcuuid)})
	}

	glog.Infoln("list", len(d.volumes), "volumes")
	return volume.Response{Volumes: vols}
}

// Capabilities is always local
func (d *FireCampVolumeDriver) Capabilities(volume.Request) volume.Response {
	return volume.Response{
		Capabilities: volume.Capability{Scope: "local"},
	}
}

func (d *FireCampVolumeDriver) genReqUUID(serviceUUID string) string {
	t := time.Now().Unix()
	requuid := d.serverInfo.GetPrivateIP() + common.NameSeparator +
		serviceUUID + common.NameSeparator + strconv.FormatInt(t, 10)
	return requuid
}

// Mount mounts the volume to the host.
func (d *FireCampVolumeDriver) Mount(r volume.MountRequest) volume.Response {
	glog.Infoln("handle Mount ", r)

	var err error
	serviceUUID := r.Name
	memberIndex := int64(-1)
	if containersvc.VolumeSourceHasTaskSlot(r.Name) {
		serviceUUID, memberIndex, err = containersvc.ParseVolumeSource(r.Name)
		if err != nil {
			errmsg := fmt.Sprintf("invalid volume name %s, error: %s", r, err)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}
		// docker swarm task slot starts with 1, 2, ...
		memberIndex--
	}

	mountPath := d.mountpoint(serviceUUID)

	ctx := context.Background()
	requuid := d.genReqUUID(serviceUUID)
	ctx = utils.NewRequestContext(ctx, requuid)

	// if volume is already mounted, return
	member := d.getMountedVolumeAndIncRef(serviceUUID)
	if member != nil {
		glog.Infoln("Mount done, volume", member, "is already mounted")
		return volume.Response{Mountpoint: mountPath}
	}

	// get the service attr and the service member for the current container
	serviceAttr, member, errmsg := d.getServiceAttrAndMember(ctx, serviceUUID, memberIndex, requuid)
	if errmsg != "" {
		return volume.Response{Err: errmsg}
	}

	// update DNS, this MUST be done after volume is assigned to this node (updated in DB).
	if serviceAttr.RegisterDNS {
		errmsg := d.updateDNS(ctx, serviceAttr, member, requuid)
		if errmsg != "" {
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
	glog.Infoln("attach volume", member, "requuid", requuid)
	err = d.attachVolume(ctx, *member, requuid)
	if err != nil {
		errmsg := fmt.Sprintf("Mount failed, attach member volume error %s %s, requuid %s", err, member, requuid)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	// format dev if necessary
	formatted := d.isFormatted(member.DeviceName)
	if !formatted {
		err = d.formatFS(member.DeviceName)
		if err != nil {
			errmsg := fmt.Sprintf("Mount failed, format device error %s member %s, requuid %s", err, member, requuid)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}
	}

	// mount
	err = d.mountFS(member.DeviceName, mountPath)
	if err != nil {
		errmsg := fmt.Sprintf("Mount failed, mount fs error %s member %s, requuid %s", err, member, requuid)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	// create the service config file if necessary
	err = d.createConfigFile(ctx, mountPath, member, requuid)
	if err != nil {
		errmsg := fmt.Sprintf("create the config file error %s, service %s, requuid %s", err, serviceAttr, requuid)
		glog.Errorln(errmsg)

		// umount fs
		err = d.unmountFS(mountPath)
		if err == nil {
			// successfully umount, return error for the Mount()
			d.removeMountPath(mountPath)
			// detach volume, ignore the possible error.
			err = d.serverIns.DetachVolume(ctx, member.VolumeID, d.serverInfo.GetLocalInstanceID(), member.DeviceName)
			glog.Errorln("detached volume", member, "error", err, "requuid", requuid)
			return volume.Response{Err: errmsg}
		}

		// umount failed, has to return successs. or else, volume will not be able to
		// be detached. Expect the container will check the config file and exits,
		// so Unmount() will be called and the task will be rescheduled.
		glog.Errorln("umount fs error", err, mountPath, serviceAttr, "requuid", requuid)
	}

	// cache mounted volume
	d.addMountedVolume(serviceUUID, member)

	glog.Infoln("Mount done, member volume", member, "to", mountPath, "requuid", requuid)
	return volume.Response{Mountpoint: mountPath}
}

// Unmount the volume.
func (d *FireCampVolumeDriver) Unmount(r volume.UnmountRequest) volume.Response {
	var err error
	serviceUUID := r.Name
	if containersvc.VolumeSourceHasTaskSlot(r.Name) {
		serviceUUID, _, err = containersvc.ParseVolumeSource(r.Name)
		if err != nil {
			errmsg := fmt.Sprintf("invalid volume name %s, error: %s", r, err)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}
	}

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
	glog.Infoln("volume", vol.member, "connections", vol.connections, "requuid", requuid)
	if (vol.connections - 1) > 0 {
		// update the volumes
		vol.connections--
		glog.Infoln("Unmount done, some container still uses the volume",
			vol.member, "connections", vol.connections, "requuid", requuid)
		return volume.Response{}
	}

	// last container for the volume, umount fs
	glog.Infoln("last container for the volume", r, "unmount fs", mountPath, "requuid", requuid)
	err = d.unmountFS(mountPath)
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
	err = d.serverIns.DetachVolume(ctx, vol.member.VolumeID,
		d.serverInfo.GetLocalInstanceID(), vol.member.DeviceName)
	glog.Infoln("detached volume", vol.member, "serverID",
		d.serverInfo.GetLocalInstanceID(), "error", err, "requuid", requuid)

	// don't wait till detach complete, detach may take long time and cause container stop timeout.
	// let the new task attachment handles it. If detach times out at new task start phase,
	// ECS will simply reschedule the task.

	glog.Infoln("Unmount done", r, "requuid", requuid)
	return volume.Response{}
}

func (d *FireCampVolumeDriver) updateDNS(ctx context.Context, serviceAttr *common.ServiceAttr, member *common.ServiceMember, requuid string) (errmsg string) {
	dnsName := dns.GenDNSName(member.MemberName, serviceAttr.DomainName)
	privateIP := d.serverInfo.GetPrivateIP()
	err := d.dnsIns.UpdateDNSRecord(ctx, dnsName, privateIP, serviceAttr.HostedZoneID)
	if err != nil {
		errmsg = fmt.Sprintf("UpdateDNSRecord error %s service %s member %s, requuid %s",
			err, serviceAttr, member, requuid)
		glog.Errorln(errmsg)
		return errmsg
	}

	// make sure DNS returns the updated record
	dnsIP, err := d.dnsIns.WaitDNSRecordUpdated(ctx, dnsName, privateIP, serviceAttr.HostedZoneID)
	if err != nil {
		errmsg = fmt.Sprintf("WaitDNSRecordUpdated error %s, expect privateIP %s got %s, service %s member %s, requuid %s",
			err, privateIP, dnsIP, serviceAttr, member, requuid)
		glog.Errorln(errmsg)
		return errmsg
	}

	// wait till the DNS record lookup on the node returns the updated ip.
	// This is to make sure DB doesn't get the invalid old host at the replication initialization.
	//
	// TODO After service is created, the first time DNS lookup from AWS Route53 takes around 60s.
	// Any way to enhance it? Also if container fails over to another node, the DNS lookup takes
	// around 16s to get the new ip, even if the default TTL is set to 3s.
	glog.Infoln("DNS record updated", dnsName, "wait till the local host refreshes it, requuid", requuid)
	maxWaitSeconds := time.Duration(90) * time.Second
	sleepSeconds := time.Duration(3) * time.Second
	for t := time.Duration(0); t < maxWaitSeconds; t += sleepSeconds {
		addr, err := d.dnsIns.LookupLocalDNS(ctx, dnsName)
		if err == nil && addr == privateIP {
			glog.Infoln("LookupLocalDNS", dnsName, "gets the local privateIP", privateIP, "requuid", requuid)
			return ""
		}
		glog.Infoln("LookupLocalDNS", dnsName, "error", err, "get ip", addr, "requuid", requuid)
		time.Sleep(sleepSeconds)
	}

	errmsg = fmt.Sprintf("local host waits the dns refreshes timed out, dnsname %s privateIP %s, requuid %s, service %s member %s",
		dnsName, privateIP, requuid, serviceAttr, member)
	glog.Errorln(errmsg)
	return errmsg
}

func (d *FireCampVolumeDriver) getServiceAttrAndMember(ctx context.Context, serviceUUID string,
	memberIndex int64, requuid string) (serviceAttr *common.ServiceAttr, member *common.ServiceMember, errmsg string) {
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
			errmsg = fmt.Sprintf("GetOrCreateHostedZoneIDByName error %s domain %s vpc %s %s, service %s, requuid %s",
				err, domainName, vpcID, vpcRegion, serviceUUID, requuid)
			glog.Errorln(errmsg)
			return nil, nil, errmsg
		}
		glog.Infoln("get hostedZoneID", hostedZoneID, "for service", serviceUUID,
			"domain", domainName, "vpc", vpcID, vpcRegion, "requuid", requuid)

		// detach the volume from the last owner
		err = d.detachControlDBVolume(ctx, utils.GetControlDBVolumeID(serviceUUID), requuid)
		if err != nil {
			errmsg = fmt.Sprintf("detachControlDBVolume error %s service %s, requuid %s", err, serviceUUID, requuid)
			glog.Errorln(errmsg)
			return nil, nil, errmsg
		}

		// get the volume and serviceAttr for the controldb service
		member, serviceAttr = d.getControlDBVolumeAndServiceAttr(serviceUUID, domainName, hostedZoneID)

		glog.Infoln("get controldb service volume", member, "serviceAttr", serviceAttr, "requuid", requuid)
		return serviceAttr, member, ""
	}

	// the application service, talks with the controldb service for volume
	// get cluster and service names by serviceUUID
	serviceAttr, err = d.dbIns.GetServiceAttr(ctx, serviceUUID)
	if err != nil {
		errmsg := fmt.Sprintf("GetServiceAttr error %s serviceUUID %s requuid %s", err, serviceUUID, requuid)
		glog.Errorln(errmsg)
		return nil, nil, errmsg
	}
	glog.Infoln("get application service attr", serviceAttr, "requuid", requuid)

	// get one member for this mount
	member, err = d.getMemberForTask(ctx, serviceAttr, memberIndex, requuid)
	if err != nil {
		errmsg := fmt.Sprintf("Mount failed, get service member error %s, serviceUUID %s, requuid %s", err, serviceUUID, requuid)
		glog.Errorln(errmsg)
		return nil, nil, errmsg
	}

	glog.Infoln("get service member", member, "requuid", requuid)
	return serviceAttr, member, ""
}

func (d *FireCampVolumeDriver) createConfigFile(ctx context.Context, mountPath string, member *common.ServiceMember, requuid string) error {
	// if the service does not need the config file, return
	if len(member.Configs) == 0 {
		glog.Infoln("no config file", member, "requuid", requuid)
		return nil
	}

	// create the conf dir
	configDirPath := filepath.Join(mountPath, common.DefaultConfigDir)
	err := utils.CreateDirIfNotExist(configDirPath)
	if err != nil {
		glog.Errorln("create the config dir error", err, configDirPath, "requuid", requuid, member)
		return err
	}

	// check the md5 first.
	// The config file content may not be small. For example, the default cassandra.yaml is 45K.
	// The config files are rarely updated after creation. No need to read the content over.
	for _, cfg := range member.Configs {
		// check and create the config file if necessary
		fpath := filepath.Join(configDirPath, cfg.FileName)
		exist, err := utils.IsFileExist(fpath)
		if err != nil {
			glog.Errorln("check the config file error", err, fpath, "requuid", requuid, member)
			return err
		}

		if exist {
			// the config file exists, check whether it is the same with the config in DB.
			fdata, err := ioutil.ReadFile(fpath)
			if err != nil {
				glog.Errorln("read the config file error", err, fpath, "requuid", requuid, member)
				return err
			}

			fmd5 := md5.Sum(fdata)
			fmd5str := hex.EncodeToString(fmd5[:])
			if fmd5str == cfg.FileMD5 {
				glog.Infoln("the config file has the latest content", fpath, "requuid", requuid, member)
				// check next config file
				continue
			}
			// the config content is changed, update the config file
			glog.Infoln("the config file changed, update it", fpath, "requuid", requuid, member)
		}

		// the config file not exists or content is changed
		// read the content
		cfgFile, err := d.dbIns.GetConfigFile(ctx, member.ServiceUUID, cfg.FileID)
		if err != nil {
			glog.Errorln("GetConfigFile error", err, fpath, "requuid", requuid, member)
			return err
		}

		data := []byte(cfgFile.Content)
		err = utils.CreateOrOverwriteFile(fpath, data, os.FileMode(cfgFile.FileMode))
		if err != nil {
			glog.Errorln("write the config file error", err, fpath, "requuid", requuid, member)
			return err
		}
		glog.Infoln("write the config file done for service", fpath, "requuid", requuid, member)
	}

	return nil
}

func (d *FireCampVolumeDriver) checkAndUnmountNotCachedVolume(mountPath string) {
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

// getMemberForTask will get one idle member, not used or owned by down task,
// detach it and update the new owner in DB.
func (d *FireCampVolumeDriver) getMemberForTask(ctx context.Context, serviceAttr *common.ServiceAttr,
	memberIndex int64, requuid string) (member *common.ServiceMember, err error) {
	if memberIndex != int64(-1) {
		glog.Infoln("member index is passed in", memberIndex, "requuid", requuid, serviceAttr)

		memberName := utils.GenServiceMemberName(serviceAttr.ServiceName, memberIndex)
		member, err = d.dbIns.GetServiceMember(ctx, serviceAttr.ServiceUUID, memberName)
		if err != nil {
			glog.Errorln("GetServiceMember error", err, "memberName", memberName, "requuid", requuid, "service", serviceAttr)
			return nil, err
		}
	} else {
		// find one idle member
		member, err = d.findIdleMember(ctx, serviceAttr, requuid)
		if err != nil {
			glog.Errorln("findIdleMember error", err, "requuid", requuid, "service", serviceAttr)
			return nil, err
		}
	}

	// detach volume from the last owner
	err = d.detachVolumeFromLastOwner(ctx, member.VolumeID, member.ServerInstanceID, member.DeviceName, requuid)
	if err != nil {
		glog.Errorln("detachVolumeFromLastOwner error", err, "requuid", requuid, "member", member)
		return nil, err
	}

	glog.Infoln("got and detached member volume", member, "requuid", requuid)

	localTaskID := ""
	if memberIndex != int64(-1) {
		// the container framework passes in the member index for the volume.
		// no need to get the real task id, simply set to the server instance id.
		localTaskID = d.serverInfo.GetLocalInstanceID()
	} else {
		// get taskID of the local task.
		// assume only one task on one node for one service. It does not make sense to run
		// like mongodb primary and standby on the same node.
		localTaskID, err = d.containersvcIns.GetServiceTask(ctx, serviceAttr.ClusterName,
			serviceAttr.ServiceName, d.containerInfo.GetLocalContainerInstanceID())
		if err != nil {
			glog.Errorln("get local task id error", err, "requuid", requuid,
				"containerInsID", d.containerInfo.GetLocalContainerInstanceID(), "service attr", serviceAttr)
			return nil, err
		}
	}

	// update service member owner in db.
	// TODO if there are concurrent failures, multiple tasks may select the same idle member.
	// now, the task will fail and be scheduled again. better have some retries here.
	newMember := db.UpdateServiceMemberOwner(member, localTaskID,
		d.containerInfo.GetLocalContainerInstanceID(), d.serverInfo.GetLocalInstanceID())
	err = d.dbIns.UpdateServiceMember(ctx, member, newMember)
	if err != nil {
		glog.Errorln("UpdateServiceMember error", err, "requuid", requuid, "new", newMember, "old", member)
		return nil, err
	}

	glog.Infoln("updated member", member, "to", localTaskID, "requuid", requuid,
		d.containerInfo.GetLocalContainerInstanceID(), d.serverInfo.GetLocalInstanceID())

	return newMember, nil
}

func (d *FireCampVolumeDriver) detachControlDBVolume(ctx context.Context, volID string, requuid string) error {
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

func (d *FireCampVolumeDriver) getControlDBVolumeAndServiceAttr(serviceUUID string, domainName string, hostedZoneID string) (*common.ServiceMember, *common.ServiceAttr) {
	mtime := time.Now().UnixNano()
	// construct the vol for the controldb service
	// could actually get the taksID, as only one controldb task is running at any time.
	// but taskID is useless for the controldb service, so simply use empty taskID.
	taskID := ""
	member := db.CreateServiceMember(serviceUUID,
		common.ControlDBServiceName,
		d.serverInfo.GetLocalAvailabilityZone(),
		taskID,
		d.containerInfo.GetLocalContainerInstanceID(),
		d.serverInfo.GetLocalInstanceID(),
		mtime,
		utils.GetControlDBVolumeID(serviceUUID),
		d.serverIns.GetControlDBDeviceName(),
		nil)

	// construct the serviceAttr for the controldb service
	// taskCounts and volSizeGB are useless for the controldb service
	taskCounts := int64(1)
	volSizeGB := int64(0)
	registerDNS := true
	attr := db.CreateServiceAttr(serviceUUID, common.ServiceStatusActive, mtime, taskCounts,
		volSizeGB, d.containerInfo.GetContainerClusterID(), common.ControlDBServiceName,
		d.serverIns.GetControlDBDeviceName(), registerDNS, domainName, hostedZoneID)

	return member, attr
}

func (d *FireCampVolumeDriver) findIdleMember(ctx context.Context,
	serviceAttr *common.ServiceAttr, requuid string) (member *common.ServiceMember, err error) {
	// list all tasks of the service
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

	// list all members from DB, e.dbIns.ListServiceMembers(service.ServiceUUID)
	members, err := d.dbIns.ListServiceMembers(ctx, serviceAttr.ServiceUUID)
	if err != nil {
		glog.Errorln("ListVolumes error", err, "service", serviceAttr, "requuid", requuid)
		return nil, err
	}

	// find one idle volume, the volume's taskID not in the task list
	for _, member := range members {
		// TODO filter out non local zone member at ListServiceMembers
		zone := d.serverInfo.GetLocalAvailabilityZone()
		if member.AvailableZone != zone {
			glog.V(1).Infoln("member is not in local zone", zone, member)
			continue
		}

		// TODO selects the idle member on the same node, the container platform may reschedule the task on the same node.

		// TODO better to get all idle members and randomly select one, as the concurrent
		//			tasks may find the same idle member around the same time.
		_, ok := taskIDs[member.TaskID]
		if !ok {
			glog.Infoln("find idle member", member, "service", serviceAttr, "requuid", requuid)
			return member, nil
		}

		// member's task is still running
		// It is weird, but when testing mongodb container with member, the same task
		// may mount and umount the volume, then try to mount again. Don't know why.
		if member.ContainerInstanceID == d.containerInfo.GetLocalContainerInstanceID() {
			glog.Infoln("member task is still running on local instance, reuse it", member, "requuid", requuid)
			return member, nil
		}

		glog.V(0).Infoln("member", member, "in use, service", serviceAttr, "requuid", requuid)
	}

	glog.Errorln("service has no idle member", serviceAttr, "requuid", requuid)
	return nil, common.ErrInternal
}

func (d *FireCampVolumeDriver) mountpoint(serviceUUID string) string {
	return filepath.Join(d.root, serviceUUID)
}

func (d *FireCampVolumeDriver) attachVolume(ctx context.Context, member common.ServiceMember, requuid string) error {
	// attach volume to host
	err := d.serverIns.AttachVolume(ctx, member.VolumeID, member.ServerInstanceID, member.DeviceName)
	if err != nil {
		glog.Errorln("failed to AttachVolume", member, "error", err, "requuid", requuid)
		return err
	}

	// wait till attach complete
	err = d.serverIns.WaitVolumeAttached(ctx, member.VolumeID)
	if err != nil {
		// TODO EBS volume attach may stuck. Is below ref still the case? Maybe not?
		// ref discussion: https://forums.aws.amazon.com/message.jspa?messageID=235023
		// caller should handle it. For example, get volume state, detach it if still
		// at attaching state, and try attach again.
		glog.Errorln("member volume", member, "is still not attached, requuid", requuid)
		return err
	}

	glog.Infoln("attached member volume", member, "requuid", requuid)
	return nil
}

func (d *FireCampVolumeDriver) detachVolumeFromLastOwner(ctx context.Context, volID string,
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

func (d *FireCampVolumeDriver) getMountedVolume(serviceUUID string) *common.ServiceMember {
	d.volumesLock.Lock()
	defer d.volumesLock.Unlock()

	vol, ok := d.volumes[serviceUUID]
	if !ok {
		return nil
	}
	return vol.member
}

func (d *FireCampVolumeDriver) getMountedVolumeAndIncRef(serviceUUID string) *common.ServiceMember {
	d.volumesLock.Lock()
	defer d.volumesLock.Unlock()

	vol, ok := d.volumes[serviceUUID]
	if !ok {
		return nil
	}

	vol.connections++

	glog.Infoln("get mounted volume", vol.member, "connections", vol.connections)
	return vol.member
}

func (d *FireCampVolumeDriver) addMountedVolume(serviceUUID string, member *common.ServiceMember) {
	d.volumesLock.Lock()
	defer d.volumesLock.Unlock()

	d.volumes[serviceUUID] = &driverVolume{member: member, connections: 1}
}

func (d *FireCampVolumeDriver) removeMountedVolume(serviceUUID string) {
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
	glog.Infoln("volume is still in use, not remove", vol.member, "connections", vol.connections)
}

func (d *FireCampVolumeDriver) checkAndCreateMountPath(mountPath string) error {
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

func (d *FireCampVolumeDriver) removeMountPath(mountPath string) error {
	err := os.Remove(mountPath)
	if err != nil {
		glog.Errorln("failed to remove mountPath", mountPath, "error", err)
		return err
	}
	return nil
}

func (d *FireCampVolumeDriver) mountFS(source string, target string) error {
	args := d.getMountArgs(source, target, defaultFSType, defaultMountOptions)

	command := exec.Command(args[0], args[1:]...)
	output, err := command.CombinedOutput()
	if err != nil {
		glog.Errorln("mount failed, arguments", args,
			"output", string(output[:]), "error", err)
		return err
	}

	return err
}

func (d *FireCampVolumeDriver) getMountArgs(source, target, fstype string, options []string) []string {
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

func (d *FireCampVolumeDriver) unmountFS(target string) error {
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

// formats the source device
func (d *FireCampVolumeDriver) formatFS(source string) error {
	var args []string
	args = append(args, defaultMkfs, source)
	command := exec.Command(args[0], args[1:]...)
	output, err := command.CombinedOutput()
	if err != nil {
		glog.Errorln(defaultMkfs, "failed, arguments", args, "output", string(output[:]), "error", err)
		return err
	}

	glog.Infoln("formatted device", args)

	return nil
}

func (d *FireCampVolumeDriver) isFormatted(source string) bool {
	//d.lsblk()

	var args []string
	args = append(args, "blkid", "-p", "-u", "filesystem", source)

	command := exec.Command(args[0], args[1:]...)
	output, err := command.CombinedOutput()
	ostr := string(output[:])
	if err != nil {
		// the caller ensure the volume is attached, here return false.
		// if the volume is not attached, the later format will fail and return error.
		glog.Errorln("check device error", err, "arguments", args, "output", ostr)
		return false
	}

	if len(output) != 0 {
		glog.Infoln("volume is formatted, arguments", args, "output", ostr)
		return true
	}

	glog.Infoln("volume is not formatted, arguments", args, "output", ostr)
	return false
}

func (d *FireCampVolumeDriver) lsblk() {
	var args []string
	args = append(args, "lsblk")

	command := exec.Command(args[0], args[1:]...)
	output, err := command.CombinedOutput()
	glog.Infoln("arguments", args, "output", string(output[:]), "error", err)
}

func (d *FireCampVolumeDriver) isDeviceMountToPath(mountPath string) bool {
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
