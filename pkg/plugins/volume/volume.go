package dockervolume

import (
	"errors"
	"fmt"
	"math/rand"
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

	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/pkg/containersvc"
	"github.com/cloudstax/firecamp/pkg/db"
	"github.com/cloudstax/firecamp/pkg/dns"
	"github.com/cloudstax/firecamp/pkg/plugins"
	"github.com/cloudstax/firecamp/pkg/plugins/network"
	"github.com/cloudstax/firecamp/pkg/server"
	"github.com/cloudstax/firecamp/pkg/utils"
)

const (
	defaultRoot   = "/mnt/" + common.SystemName
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

	// network service
	netSvc *dockernetwork.ServiceNetwork
}

// NewVolumeDriver creates a new FireCampVolumeDriver instance
func NewVolumeDriver(dbIns db.DB, dnsIns dns.DNS, serverIns server.Server, serverInfo server.Info,
	containersvcIns containersvc.ContainerSvc, containerInfo containersvc.Info) *FireCampVolumeDriver {
	netSvc := dockernetwork.NewServiceNetwork(dbIns, dnsIns, serverIns, serverInfo)
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
		netSvc:          netSvc,
	}
	return d
}

// Create checks if volume is mounted or service exists in DB
func (d *FireCampVolumeDriver) Create(r volume.Request) volume.Response {
	glog.Infoln("Create volume", r)

	serviceUUID, _, _, err := d.parseRequestName(r.Name)
	if err != nil {
		errmsg := fmt.Sprintf("invalid volume name %s, error: %s", r, err)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	// check if volume is in cache
	member := d.getMountedVolume(serviceUUID)
	if member != nil {
		glog.Infoln("Create, volume in cache", member, "for serviceUUID", serviceUUID)
		return volume.Response{}
	}

	ctx := context.Background()

	// volume not in cache, check if service exists in DB
	_, err = d.dbIns.GetServiceAttr(ctx, serviceUUID)
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
	serviceUUID, _, _, err := d.parseRequestName(r.Name)
	if err != nil {
		errmsg := fmt.Sprintf("invalid volume name %s, error: %s", r, err)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	d.removeMountedVolume(serviceUUID)
	glog.Infoln("Remove request done", r)
	return volume.Response{}
}

// Get returns the mountPath if volume is mounted
func (d *FireCampVolumeDriver) Get(r volume.Request) volume.Response {
	glog.Infoln("Get volume", r)

	serviceUUID, mountPath, _, err := d.parseRequestName(r.Name)
	if err != nil {
		errmsg := fmt.Sprintf("invalid volume name %s, error: %s", r, err)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	ctx := context.Background()
	_, err = d.dbIns.GetServiceAttr(ctx, serviceUUID)
	if err != nil {
		if err == db.ErrDBRecordNotFound {
			errmsg := fmt.Sprintf("volume %s is not found", r.Name)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}
		errmsg := fmt.Sprintf("GetServiceAttr error %s volume %s", err, r.Name)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	glog.Infoln("volume exists", r.Name)

	mountPath = d.mountpoint(mountPath)

	member := d.getMountedVolume(serviceUUID)
	if member != nil {
		glog.Infoln("volume is mounted", r)
		return volume.Response{Volume: &volume.Volume{Name: r.Name, Mountpoint: mountPath}}
	}

	// volume is not mounted locally, return success. If the service doesn't exist,
	// the later mount will fail.
	glog.Infoln("volume is not mounted for service", serviceUUID)
	return volume.Response{Volume: &volume.Volume{Name: r.Name}}
}

// Path returns the volume mountPath
func (d *FireCampVolumeDriver) Path(r volume.Request) volume.Response {
	glog.Infoln("Path volume", r)

	_, mountPath, _, err := d.parseRequestName(r.Name)
	if err != nil {
		errmsg := fmt.Sprintf("invalid volume name %s, error: %s", r, err)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	mountPath = d.mountpoint(mountPath)

	return volume.Response{Mountpoint: mountPath}
}

// List lists all mounted volumes
func (d *FireCampVolumeDriver) List(r volume.Request) volume.Response {
	d.volumesLock.Lock()
	defer d.volumesLock.Unlock()

	var vols []*volume.Volume
	for _, v := range d.volumes {
		svcuuid := v.member.ServiceUUID
		name := containersvc.GenVolumeSourceName(svcuuid, v.member.MemberName)
		vols = append(vols, &volume.Volume{Name: name, Mountpoint: d.mountpoint(svcuuid)})

		if len(v.member.Spec.Volumes.JournalVolumeID) != 0 {
			logvolpath := containersvc.GetServiceJournalVolumeName(svcuuid)
			name = containersvc.GenVolumeSourceName(logvolpath, v.member.MemberName)
			vols = append(vols, &volume.Volume{Name: name, Mountpoint: d.mountpoint(logvolpath)})
		}
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

	serviceUUID, mountPath, memberName, err := d.parseRequestName(r.Name)
	if err != nil {
		errmsg := fmt.Sprintf("invalid volume name %s, error: %s", r, err)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	mountPath = d.mountpoint(mountPath)

	ctx := context.Background()
	requuid := d.genReqUUID(serviceUUID)
	ctx = utils.NewRequestContext(ctx, requuid)

	// simply hold lock to avoid the possible concurrent mount.
	// one service could have 2 volumes, one for data, the other for journal.
	// one mount will mount both volumes, the other mount will increase the ref and return success.
	d.volumesLock.Lock()
	defer d.volumesLock.Unlock()

	// if volume is already mounted, return
	member := d.getMountedVolumeAndIncRefNoLock(serviceUUID)
	if member != nil {
		glog.Infoln("Mount done, volume", member, "is already mounted")
		return volume.Response{Mountpoint: mountPath}
	}

	// get the service attr and the service member for the current container
	serviceAttr, member, errmsg := d.getServiceAttrAndMember(ctx, serviceUUID, memberName, requuid)
	if len(errmsg) != 0 {
		return volume.Response{Err: errmsg}
	}

	// update DNS, this MUST be done after volume is assigned to this node (updated in DB).
	// if the service requires the static ip, no need to update DNS as DNS always points to the static ip.
	if serviceAttr.Spec.RegisterDNS && !serviceAttr.Spec.RequireStaticIP {
		_, err = d.netSvc.UpdateDNS(ctx, serviceAttr.Spec.DomainName, serviceAttr.Spec.HostedZoneID, member.MemberName)
		if err != nil {
			return volume.Response{Err: err.Error()}
		}
	}

	// updateStaticIP if the service requires the static ip.
	if serviceAttr.Spec.RequireStaticIP {
		err = d.netSvc.UpdateStaticIP(ctx, serviceAttr.Spec.DomainName, member)
		if err != nil {
			return volume.Response{Err: err.Error()}
		}
	}

	// attach volume to host
	glog.Infoln("attach volume", member, "requuid", requuid)
	err = d.attachVolumes(ctx, member, requuid)
	if err != nil {
		errmsg := fmt.Sprintf("Mount failed, attach member volume error %s %s, requuid %s", err, member, requuid)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	// format dev if necessary
	err = d.formatVolumes(ctx, member, requuid)
	if err != nil {
		errmsg := fmt.Sprintf("Mount failed, format device error %s member %s, requuid %s", err, member, requuid)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	// add the ip to network
	if serviceAttr.Spec.RequireStaticIP {
		err = d.netSvc.AddIP(member.Spec.StaticIP)
		if err != nil {
			errmsg := fmt.Sprintf("add ip error %s, member %s, requuid %s", err, member, requuid)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}
	}

	// mount volumes
	err = d.mountVolumes(ctx, member, requuid)
	if err != nil {
		errmsg := fmt.Sprintf("Mount failed, mount fs error %s member %s, requuid %s", err, member, requuid)
		glog.Errorln(errmsg)

		// try to del the ip from network
		if serviceAttr.Spec.RequireStaticIP {
			err = d.netSvc.DeleteIP(member.Spec.StaticIP)
			glog.Errorln("delIP error", err, "member", member, "requuid", requuid)
		}

		return volume.Response{Err: errmsg}
	}

	// create the service config file if necessary
	mpath := d.mountpoint(member.ServiceUUID)
	configDirPath := filepath.Join(mpath, common.DefaultConfigDir)
	err = CreateConfigFile(ctx, configDirPath, serviceAttr, member, d.dbIns)
	if err == nil {
		// create the service member so the log driver could know the service member.
		err = firecampplugin.CreatePluginServiceMemberFile(member.ServiceUUID, member.MemberName, requuid)
		if err != nil {
			glog.Errorln("CreatePluginServiceMemberFile error", err, "requuid", requuid, member)
			// cleanup the possbile garbage
			firecampplugin.DeletePluginServiceMemberFile(member.ServiceUUID, requuid)
		}
	}
	if err != nil {
		errmsg := fmt.Sprintf("create the config file error %s, service %s, requuid %s", err, serviceAttr, requuid)
		glog.Errorln(errmsg)

		// umount volumes
		err = d.umountVolumes(ctx, member, requuid)
		if err == nil {
			// successfully umount, return error for the Mount()
			// detach volume, ignore the possible error.
			d.detachVolumes(ctx, member, requuid)
			glog.Errorln("detached member volumes", member, "error", err, "requuid", requuid)

			// try to del the ip from network
			if serviceAttr.Spec.RequireStaticIP {
				err = d.netSvc.DeleteIP(member.Spec.StaticIP)
				glog.Errorln("delIP error", err, "member", member, "requuid", requuid)
			}

			return volume.Response{Err: errmsg}
		}

		// umount failed, has to return successs. or else, volume will not be able to
		// be detached. Expect the container will check the config file and exits,
		// so Unmount() will be called and the task will be rescheduled.
		glog.Errorln("umount fs error", err, "requuid", requuid, serviceAttr)
	}

	// cache mounted volume
	d.addMountedVolumeNoLock(serviceUUID, member)

	glog.Infoln("Mount done, requuid", requuid, member.Spec.Volumes, member)
	return volume.Response{Mountpoint: mountPath}
}

// Unmount the volume.
func (d *FireCampVolumeDriver) Unmount(r volume.UnmountRequest) volume.Response {
	serviceUUID, mountPath, _, err := d.parseRequestName(r.Name)
	if err != nil {
		errmsg := fmt.Sprintf("invalid volume name %s, error: %s", r, err)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	mountPath = d.mountpoint(mountPath)

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

	if vol.member.Spec.StaticIP != common.DefaultHostIP {
		// delete the ip from network to make sure no one could access this node by member's ip.
		// no need to unassign from network interface. The new node that takes over the member
		// will do it.
		err = d.netSvc.DeleteIP(vol.member.Spec.StaticIP)
		if err != nil {
			errmsg := fmt.Sprintf("delete ip error %s, member %s, requuid %s", err, vol.member, requuid)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}

		glog.Infoln("deleted member ip, requuid", requuid, vol.member)

		// static ip could not be unassigned from the network interface. If unassigned, AWS will
		// think the ip is available and may assign to the new instance.
	}

	// cleanup the member file for the log driver
	err = firecampplugin.DeletePluginServiceMemberFile(vol.member.ServiceUUID, requuid)
	if err != nil {
		errmsg := fmt.Sprintf("DeletePluginServiceMemberFile error %s, requuid %s, member %s", err, requuid, vol.member)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	// last container for the volume, umount fs
	glog.Infoln("last container for the volume, requuid", requuid, vol.member.Spec.Volumes, vol.member)
	err = d.umountVolumes(ctx, vol.member, requuid)
	if err != nil {
		errmsg := fmt.Sprintf("Unmount failed, umount fs %s error %s requuid %s", mountPath, err, requuid)
		glog.Errorln(errmsg)
		return volume.Response{Err: errmsg}
	}

	// successfully umount fs, remove mounted volume
	delete(d.volumes, serviceUUID)

	// ignore the possible errors of below operations

	// detach volume, ignore the possible error.
	d.detachVolumes(ctx, vol.member, requuid)
	glog.Infoln("detached volume", vol.member.Spec.Volumes, "serverID",
		d.serverInfo.GetLocalInstanceID(), "error", err, "requuid", requuid)

	// don't wait till detach complete, detach may take long time and cause container stop timeout.
	// let the new task attachment handles it. If detach times out at new task start phase,
	// ECS will simply reschedule the task.

	glog.Infoln("Unmount done", r, "requuid", requuid)
	return volume.Response{}
}

func (d *FireCampVolumeDriver) parseRequestName(name string) (serviceUUID string, mountPath string, memberName string, err error) {
	strs := strings.Split(name, common.VolumeNameSeparator)

	if len(strs) == 1 {
		// serviceuuid
		return strs[0], strs[0], "", nil
	}

	if len(strs) == 2 {
		if strs[0] == containersvc.JournalVolumeNamePrefix {
			// journal_serviceuuid
			return strs[1], name, "", nil
		}

		// serviceuuid_memberName
		return strs[0], strs[0], strs[1], err
	}

	if len(strs) == 3 {
		if strs[0] == containersvc.JournalVolumeNamePrefix {
			// journal_serviceuuid_memberName
			return strs[1], containersvc.GetServiceJournalVolumeName(strs[1]), strs[2], err
		}
	}

	// invalid
	return "", "", "", common.ErrInvalidArgs
}

func (d *FireCampVolumeDriver) getServiceAttrAndMember(ctx context.Context, serviceUUID string,
	memberName string, requuid string) (serviceAttr *common.ServiceAttr, member *common.ServiceMember, errmsg string) {
	// get cluster and service names by serviceUUID
	serviceAttr, err := d.dbIns.GetServiceAttr(ctx, serviceUUID)
	if err != nil {
		errmsg := fmt.Sprintf("GetServiceAttr error %s serviceUUID %s requuid %s", err, serviceUUID, requuid)
		glog.Errorln(errmsg)
		return nil, nil, errmsg
	}
	glog.Infoln("get service attr", serviceAttr, "requuid", requuid)

	// get one member for this mount
	member, err = d.getMemberForTask(ctx, serviceAttr, memberName, requuid)
	if err != nil {
		errmsg := fmt.Sprintf("Mount failed, get service member error %s, serviceUUID %s, requuid %s", err, serviceUUID, requuid)
		glog.Errorln(errmsg)
		return nil, nil, errmsg
	}

	glog.Infoln("get service member", member, "requuid", requuid)
	return serviceAttr, member, ""
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
	d.unmountFS(mountPath)
}

// getMemberForTask will get one idle member, not used or owned by down task,
// detach it and update the new owner in DB.
func (d *FireCampVolumeDriver) getMemberForTask(ctx context.Context, serviceAttr *common.ServiceAttr,
	memberName string, requuid string) (member *common.ServiceMember, err error) {
	if memberName != "" {
		glog.Infoln("member is passed in", memberName, "requuid", requuid, serviceAttr)

		member, err = d.dbIns.GetServiceMember(ctx, serviceAttr.ServiceUUID, memberName)
		if err != nil {
			glog.Errorln("GetServiceMember error", err, "member", memberName, "requuid", requuid, "service", serviceAttr)
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
	if len(member.Spec.Volumes.JournalVolumeID) != 0 {
		err = d.detachVolumeFromLastOwner(ctx, member.Spec.Volumes.JournalVolumeID, member.Spec.ServerInstanceID, member.Spec.Volumes.JournalDeviceName, requuid)
		if err != nil {
			glog.Errorln("detach journal volume from last owner error", err, "requuid", requuid, member.Spec.Volumes, member)
			return nil, err
		}
	}
	err = d.detachVolumeFromLastOwner(ctx, member.Spec.Volumes.PrimaryVolumeID, member.Spec.ServerInstanceID, member.Spec.Volumes.PrimaryDeviceName, requuid)
	if err != nil {
		glog.Errorln("detach primary volume from last owner error", err, "requuid", requuid, member.Spec.Volumes, member)
		return nil, err
	}

	glog.Infoln("got and detached member volume", member, "requuid", requuid)

	localTaskID := ""
	if memberName != "" {
		// the container framework passes in the member index for the volume.
		// no need to get the real task id, simply set to the server instance id.
		localTaskID = d.serverInfo.GetLocalInstanceID()
	} else {
		// get taskID of the local task.
		// assume only one task on one node for one service. It does not make sense to run
		// like mongodb primary and standby on the same node.
		localTaskID, err = d.containersvcIns.GetServiceTask(ctx, serviceAttr.Meta.ClusterName,
			serviceAttr.Meta.ServiceName, d.containerInfo.GetLocalContainerInstanceID())
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
	taskIDs, err := d.containersvcIns.ListActiveServiceTasks(ctx, serviceAttr.Meta.ClusterName, serviceAttr.Meta.ServiceName)
	if err != nil {
		glog.Errorln("ListActiveServiceTasks error", err, "service attr", serviceAttr, "requuid", requuid)
		return nil, err
	}

	var localIPs map[string]bool
	if serviceAttr.Spec.RequireStaticIP {
		netif, err := d.serverIns.GetInstanceNetworkInterface(ctx, d.serverInfo.GetLocalInstanceID())
		if err != nil {
			glog.Errorln("GetInstanceNetworkInterface error", err, "requuid", requuid, serviceAttr)
			return nil, err
		}

		glog.Infoln("local instance has static ips", netif.PrivateIPs, "requuid", requuid, serviceAttr)

		localIPs = make(map[string]bool)
		for _, ip := range netif.PrivateIPs {
			localIPs[ip] = true
		}
	}

	// list all members from DB, e.dbIns.ListServiceMembers(service.ServiceUUID)
	members, err := d.dbIns.ListServiceMembers(ctx, serviceAttr.ServiceUUID)
	if err != nil {
		glog.Errorln("ListVolumes error", err, "service", serviceAttr, "requuid", requuid)
		return nil, err
	}

	// find one idle volume, the volume's taskID not in the task list
	var idleMember *common.ServiceMember
	randIdx := rand.Intn(len(members))
	minIdx := len(members)

	for i, member := range members {
		// TODO filter out non local zone member at ListServiceMembers
		zone := d.serverInfo.GetLocalAvailabilityZone()
		if member.Spec.AvailableZone != zone {
			glog.V(1).Infoln("member is not in local zone", zone, member)
			continue
		}

		// check if the member is idle
		_, ok := taskIDs[member.Spec.TaskID]
		if !ok {
			glog.Infoln("find idle member", member, "service", serviceAttr, "requuid", requuid)

			// select the idle member that is previously owned by the local node
			if member.Spec.ContainerInstanceID == d.containerInfo.GetLocalContainerInstanceID() {
				glog.Infoln("idle member is previously owned by the local instance", member, "requuid", requuid)
				return member, nil
			}

			// select the member if the member's static ip is assigned to the local node
			if serviceAttr.Spec.RequireStaticIP {
				_, local := localIPs[member.Spec.StaticIP]
				if local {
					glog.Infoln("member's static ip is assigned to the local node, requuid", requuid, member)
					return member, nil
				}
			}

			min := 0
			if i < randIdx {
				min = randIdx - i
			} else {
				min = i - randIdx
			}

			if idleMember == nil || min < minIdx {
				// randomly select an idle member
				idleMember = member
				minIdx = min
			}

			// check next member
			continue
		}

		// member's task is still running
		// It is weird, but when testing mongodb container with member, the same task
		// may mount and umount the volume, then try to mount again. Don't know why.
		if member.Spec.ContainerInstanceID == d.containerInfo.GetLocalContainerInstanceID() {
			glog.Infoln("member task is still running on local instance, reuse it", member, "requuid", requuid)
			return member, nil
		}

		glog.V(0).Infoln("member", member, "in use, service", serviceAttr, "requuid", requuid)
	}

	if idleMember != nil {
		glog.Infoln("select idle member, requuid", requuid, member)
		return idleMember, nil
	}

	glog.Errorln("service has no idle member", serviceAttr, "requuid", requuid)
	return nil, common.ErrInternal
}

func (d *FireCampVolumeDriver) mountpoint(subpath string) string {
	return filepath.Join(d.root, subpath)
}

func (d *FireCampVolumeDriver) attachVolumes(ctx context.Context, member *common.ServiceMember, requuid string) error {
	vols := &(member.Spec.Volumes)
	// attach the primary volume to host
	err := d.serverIns.AttachVolume(ctx, vols.PrimaryVolumeID, member.Spec.ServerInstanceID, vols.PrimaryDeviceName)
	if err != nil {
		glog.Errorln("attach the primary volume error", err, "requuid", requuid, vols, member)
		return err
	}

	if len(vols.JournalVolumeID) != 0 {
		err := d.serverIns.AttachVolume(ctx, vols.JournalVolumeID, member.Spec.ServerInstanceID, vols.JournalDeviceName)
		if err != nil {
			glog.Errorln("attach the journal volume error", err, "requuid", requuid, member.Spec.Volumes, member)
			d.serverIns.DetachVolume(ctx, vols.PrimaryVolumeID, member.Spec.ServerInstanceID, vols.PrimaryDeviceName)
			return err
		}
	}

	// wait till attach complete
	err = d.serverIns.WaitVolumeAttached(ctx, vols.PrimaryVolumeID)
	if err != nil {
		// TODO EBS volume attach may stuck. Is below ref still the case? Maybe not?
		// ref discussion: https://forums.aws.amazon.com/message.jspa?messageID=235023
		// caller should handle it. For example, get volume state, detach it if still
		// at attaching state, and try attach again.
		glog.Errorln("the primary volume is still not attached, requuid", requuid, vols, member)
		// send request to detach the volume
		d.detachVolumes(ctx, member, requuid)
		return err
	}

	if len(vols.JournalVolumeID) != 0 {
		err = d.serverIns.WaitVolumeAttached(ctx, vols.JournalVolumeID)
		if err != nil {
			glog.Errorln("the journal volume is still not attached, requuid", requuid, vols, member)
			// send request to detach the volume
			d.detachVolumes(ctx, member, requuid)
			return err
		}
	}

	glog.Infoln("attached member volumes, requuid", requuid, vols, member)
	return nil
}

func (d *FireCampVolumeDriver) detachVolumes(ctx context.Context, member *common.ServiceMember, requuid string) {
	vols := &(member.Spec.Volumes)

	err := d.serverIns.DetachVolume(ctx, vols.PrimaryVolumeID, d.serverInfo.GetLocalInstanceID(), vols.PrimaryDeviceName)
	if err != nil {
		glog.Errorln("detach the primary volume error", err, "requuid", requuid, vols, member)
	}

	if len(member.Spec.Volumes.JournalVolumeID) != 0 {
		err = d.serverIns.DetachVolume(ctx, vols.JournalVolumeID, d.serverInfo.GetLocalInstanceID(), vols.JournalDeviceName)
		if err != nil {
			glog.Errorln("detach the journal volume error", err, "requuid", requuid, vols, member)
		}
	}
}

func (d *FireCampVolumeDriver) formatVolumes(ctx context.Context, member *common.ServiceMember, requuid string) error {
	// format dev if necessary
	formatted := d.isFormatted(member.Spec.Volumes.PrimaryDeviceName)
	if !formatted {
		err := d.formatFS(member.Spec.Volumes.PrimaryDeviceName)
		if err != nil {
			glog.Errorln("format the primary device error", err, "requuid", requuid, member.Spec.Volumes)
			return err
		}
	}

	if len(member.Spec.Volumes.JournalVolumeID) != 0 {
		formatted := d.isFormatted(member.Spec.Volumes.JournalDeviceName)
		if !formatted {
			err := d.formatFS(member.Spec.Volumes.JournalDeviceName)
			if err != nil {
				glog.Errorln("format the journal device error", err, "requuid", requuid, member.Spec.Volumes)
				return err
			}
		}
	}

	return nil
}

func (d *FireCampVolumeDriver) mountVolumes(ctx context.Context, member *common.ServiceMember, requuid string) error {
	// mount the primary volume
	primaryMountPath := d.mountpoint(member.ServiceUUID)
	err := d.mountFS(member.Spec.Volumes.PrimaryDeviceName, primaryMountPath)
	if err != nil {
		glog.Errorln("mount primary device error", err, "requuid", requuid, "mount path", primaryMountPath, member.Spec.Volumes)
		return err
	}
	glog.Infoln("mount primary device to", primaryMountPath, "requuid", requuid, member.Spec.Volumes)

	// mount the journal volume
	if len(member.Spec.Volumes.JournalVolumeID) != 0 {
		mpath := d.mountpoint(containersvc.GetServiceJournalVolumeName(member.ServiceUUID))
		err := d.mountFS(member.Spec.Volumes.JournalDeviceName, mpath)
		if err != nil {
			glog.Errorln("mount journal device error", err, "requuid", requuid, "mount path", mpath, member.Spec.Volumes)
			// unmount the primary device. TODO if the unmount fails, manual retry may be required.
			d.unmountFS(primaryMountPath)
			return err
		}
		glog.Infoln("mount journal device to", mpath, "requuid", requuid, member.Spec.Volumes)
	}

	return nil
}

func (d *FireCampVolumeDriver) umountVolumes(ctx context.Context, member *common.ServiceMember, requuid string) error {
	// umount the primary volume
	mpath := d.mountpoint(member.ServiceUUID)
	err := d.unmountFS(mpath)
	if err != nil {
		glog.Errorln("umount primary device error", err, "requuid", requuid, "mount path", mpath, member.Spec.Volumes)
	} else {
		glog.Infoln("umount primary device from", mpath, "requuid", requuid, member.Spec.Volumes)
	}

	// umount the journal volume
	if len(member.Spec.Volumes.JournalVolumeID) != 0 {
		mpath = d.mountpoint(containersvc.GetServiceJournalVolumeName(member.ServiceUUID))
		err1 := d.unmountFS(mpath)
		if err1 != nil {
			glog.Errorln("umount journal device error", err1, "requuid", requuid, "mount path", mpath, member.Spec.Volumes)
			return err1
		}
		glog.Infoln("umount journal device from", mpath, "requuid", requuid, member.Spec.Volumes)
	}

	return err
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

func (d *FireCampVolumeDriver) getMountedVolumeAndIncRefNoLock(serviceUUID string) *common.ServiceMember {
	vol, ok := d.volumes[serviceUUID]
	if !ok {
		return nil
	}

	vol.connections++

	glog.Infoln("get mounted volume", vol.member, "connections", vol.connections)
	return vol.member
}

func (d *FireCampVolumeDriver) addMountedVolumeNoLock(serviceUUID string, member *common.ServiceMember) {
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

func (d *FireCampVolumeDriver) mountFS(source string, mountPath string) error {
	err := d.checkAndCreateMountPath(mountPath)
	if err != nil {
		glog.Errorln("check/create mount path", mountPath, "error", err)
		return err
	}

	args := d.getMountArgs(source, mountPath, common.DefaultFSType, defaultMountOptions)

	command := exec.Command(args[0], args[1:]...)
	output, err := command.CombinedOutput()
	if err != nil {
		glog.Errorln("mount failed, arguments", args, "output", string(output[:]), "error", err)
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

func (d *FireCampVolumeDriver) unmountFS(mountPath string) error {
	var args []string
	args = append(args, "umount", mountPath)

	command := exec.Command(args[0], args[1:]...)
	output, err := command.CombinedOutput()
	if err != nil {
		outputstr := string(output[:])
		if strings.Contains(outputstr, "No such file or directory") ||
			strings.Contains(outputstr, "not mounted") {
			glog.Errorln("umount arguments", args, "output", outputstr)
			return nil
		}

		glog.Errorln("umount failed, arguments", args, "output", outputstr, "error", err)
		return err
	}

	d.removeMountPath(mountPath)
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
