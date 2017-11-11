package firecampdockervolume

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
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

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/server"
	"github.com/cloudstax/firecamp/utils"
)

const (
	defaultNetworkInterface = "eth0"
	defaultRoot             = "/mnt/" + common.SystemName
	defaultFSType           = "xfs"
	defaultMkfs             = "mkfs.xfs"
	tmpfileSuffix           = ".tmp"
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
	// the net interface to add/del ip
	ifname string

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
		ifname:          defaultNetworkInterface,
		containersvcIns: containersvcIns,
		containerInfo:   containerInfo,
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

	if utils.IsControlDBService(serviceUUID) {
		// controldb service volume, return success
		glog.Infoln("Create, controldb service volume", serviceUUID)
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

	mountPath = d.mountpoint(mountPath)

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
		memberIndex, err := utils.GetServiceMemberIndex(v.member.MemberName)
		if err != nil {
			errmsg := fmt.Sprintf("invalid service member name %s, error %s", v.member, err)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}

		name := containersvc.GenVolumeSourceName(svcuuid, memberIndex)
		vols = append(vols, &volume.Volume{Name: name, Mountpoint: d.mountpoint(svcuuid)})

		if len(v.member.Volumes.LogVolumeID) != 0 {
			logvolid := svcuuid + common.NameSeparator + common.LogDevicePathSuffix
			name = containersvc.GenVolumeSourceName(logvolid, memberIndex)
			vols = append(vols, &volume.Volume{Name: name, Mountpoint: d.mountpoint(logvolid)})
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

	serviceUUID, mountPath, memberIndex, err := d.parseRequestName(r.Name)
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
	// one service could have 2 volumes, one for data, the other for log.
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
	serviceAttr, member, errmsg := d.getServiceAttrAndMember(ctx, serviceUUID, memberIndex, requuid)
	if len(errmsg) != 0 {
		return volume.Response{Err: errmsg}
	}

	// update DNS, this MUST be done after volume is assigned to this node (updated in DB).
	// if the service requires the static ip, no need to update DNS as DNS always points to the static ip.
	if serviceAttr.RegisterDNS && !serviceAttr.RequireStaticIP {
		errmsg := d.updateDNS(ctx, serviceAttr, member, requuid)
		if len(errmsg) != 0 {
			return volume.Response{Err: errmsg}
		}
	}

	// updateStaticIP if the service requires the static ip.
	if serviceAttr.RequireStaticIP {
		errmsg = d.updateStaticIP(ctx, serviceAttr, member, requuid)
		if len(errmsg) != 0 {
			return volume.Response{Err: errmsg}
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
	if serviceAttr.RequireStaticIP {
		err = addIP(member.StaticIP, d.ifname)
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
		if serviceAttr.RequireStaticIP {
			err = delIP(member.StaticIP, d.ifname)
			glog.Errorln("delIP error", err, "member", member, "requuid", requuid)
		}

		return volume.Response{Err: errmsg}
	}

	// create the service config file if necessary
	err = d.createConfigFile(ctx, member, requuid)
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
			if serviceAttr.RequireStaticIP {
				err = delIP(member.StaticIP, d.ifname)
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

	glog.Infoln("Mount done, requuid", requuid, member.Volumes, member)
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

	if vol.member.StaticIP != common.DefaultHostIP {
		// delete the ip from network to make sure no one could access this node by member's ip.
		// no need to unassign from network interface. The new node that takes over the member
		// will do it.
		err = delIP(vol.member.StaticIP, d.ifname)
		if err != nil {
			errmsg := fmt.Sprintf("delete ip error %s, member %s, requuid %s", err, vol.member, requuid)
			glog.Errorln(errmsg)
			return volume.Response{Err: errmsg}
		}

		glog.Infoln("deleted member ip, requuid", requuid, vol.member)

		// static ip could not be unassigned from the network interface. If unassigned, AWS will
		// think the ip is available and may assign to the new instance.
	}

	// last container for the volume, umount fs
	glog.Infoln("last container for the volume, requuid", requuid, vol.member.Volumes, vol.member)
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
	glog.Infoln("detached volume", vol.member.Volumes, "serverID",
		d.serverInfo.GetLocalInstanceID(), "error", err, "requuid", requuid)

	// don't wait till detach complete, detach may take long time and cause container stop timeout.
	// let the new task attachment handles it. If detach times out at new task start phase,
	// ECS will simply reschedule the task.

	glog.Infoln("Unmount done", r, "requuid", requuid)
	return volume.Response{}
}

func (d *FireCampVolumeDriver) parseRequestName(name string) (serviceUUID string, mountPath string, memberIndex int64, err error) {
	strs := strings.Split(name, common.NameSeparator)

	if len(strs) == 1 {
		// serviceuuid
		return strs[0], strs[0], -1, nil
	}

	if len(strs) == 2 {
		if strs[1] == common.LogDevicePathSuffix {
			// serviceuuid-log
			return strs[0], name, -1, nil
		}

		// serviceuuid-1
		memberIndex, err = strconv.ParseInt(strs[1], 10, 64)
		// docker swarm task slot starts with 1, 2, ...
		memberIndex--
		return strs[0], strs[0], memberIndex, err
	}

	if len(strs) == 3 {
		if strs[1] == common.LogDevicePathSuffix {
			// serviceuuid-log-1
			memberIndex, err = strconv.ParseInt(strs[2], 10, 64)
			// docker swarm task slot starts with 1, 2, ...
			memberIndex--
			return strs[0], strs[0] + common.NameSeparator + common.LogDevicePathSuffix, memberIndex, err
		}
	}

	// invalid
	return "", "", -1, common.ErrInvalidArgs
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

func (d *FireCampVolumeDriver) updateStaticIP(ctx context.Context, serviceAttr *common.ServiceAttr, member *common.ServiceMember, requuid string) (errmsg string) {
	// get the static ip on the local server.
	netInterface, err := d.serverIns.GetInstanceNetworkInterface(ctx, d.serverInfo.GetLocalInstanceID())
	if err != nil {
		errmsg = fmt.Sprintf("GetInstanceNetworkInterface error %s, ServerInstanceID %s service %s member %s, requuid %s",
			err, d.serverInfo.GetLocalInstanceID(), serviceAttr, member, requuid)
		glog.Errorln(errmsg)
		return errmsg
	}

	// whether the member's static ip is owned locally.
	localOwned := false
	var memberStaticIP *common.ServiceStaticIP
	for _, ip := range netInterface.PrivateIPs {
		serviceip, err := d.dbIns.GetServiceStaticIP(ctx, ip)
		if err != nil {
			if err != db.ErrDBRecordNotFound {
				errmsg = fmt.Sprintf("GetServiceStaticIP error %s, ip %s service %s member %s, requuid %s",
					err, ip, serviceAttr, member, requuid)
				glog.Errorln(errmsg)
				return errmsg
			}

			// this is possible as ip is created at the network interface first, then put to db.
			glog.Infoln("ip", ip, "not found in db, network interface", netInterface.InterfaceID)
			continue
		}

		glog.Infoln("get service ip, requuid", requuid, serviceip, "member", member)

		// if ip does not belong to the service, skip it
		if serviceip.ServiceUUID != member.ServiceUUID {
			continue
		}

		// ip belongs to the service, check if ip is for the current member.
		if ip == member.StaticIP {
			// ip is for the current member
			glog.Infoln("current node has the member's static ip, requuid", requuid, serviceip, member)
			localOwned = true
			memberStaticIP = serviceip
		} else {
			// ip belongs to the service but not for the current member.
			// unassign it to make sure other members do NOT take us as the previous member.

			// this should actually not happen, as the static ip will be unassigned when umount the member's volume.
			// here is just a protection to clean up the possible dangling ip. Q: how could the dangling ip happen?
			glog.Errorln("unassign dangling ip from local network", netInterface.InterfaceID,
				"server", netInterface.ServerInstanceID, serviceip, "requuid", requuid)

			err = d.serverIns.UnassignStaticIP(ctx, netInterface.InterfaceID, ip)
			if err != nil {
				errmsg = fmt.Sprintf("UnassignStaticIP error %s, ip %s network interface %s member %s, requuid %s",
					err, serviceip, netInterface.InterfaceID, member, requuid)
				glog.Errorln(errmsg)
				return errmsg
			}
			// should not update db here, as another node may be in the process of taking over the static ip.

			// delete the possible ip from network
			err = delIP(ip, d.ifname)
			if err != nil {
				errmsg = fmt.Sprintf("delete ip error %s, ip %s, member %s, requuid %s", err, ip, member, requuid)
				glog.Errorln(errmsg)
				return errmsg
			}
		}
	}

	if memberStaticIP == nil {
		// member's static ip is not owned by the local node, load from db.
		memberStaticIP, err = d.dbIns.GetServiceStaticIP(ctx, member.StaticIP)
		if err != nil {
			errmsg = fmt.Sprintf("GetServiceStaticIP error %s, service %s member %s, requuid %s",
				err, serviceAttr, member, requuid)
			glog.Errorln(errmsg)
			return errmsg
		}
	}

	glog.Infoln("member's static ip", memberStaticIP, "requuid", requuid, "member", member)

	if localOwned {
		// the member's ip is owned by the local node, check whether need to update db.
		// The ServiceStaticIP in db may not be updated. For example, after ip is assigned to
		// the local node, node/plugin restarts before db is updated.
		if memberStaticIP.ServerInstanceID != d.serverInfo.GetLocalInstanceID() ||
			memberStaticIP.NetworkInterfaceID != netInterface.InterfaceID {
			newip := db.UpdateServiceStaticIP(memberStaticIP, d.serverInfo.GetLocalInstanceID(), netInterface.InterfaceID)
			err = d.dbIns.UpdateServiceStaticIP(ctx, memberStaticIP, newip)
			if err != nil {
				errmsg = fmt.Sprintf("UpdateServiceStaticIP error %s, ip %s service %s member %s, requuid %s",
					err, memberStaticIP, serviceAttr, member, requuid)
				glog.Errorln(errmsg)
				return errmsg
			}

			glog.Infoln("UpdateServiceStaticIP to local node", newip, "requuid", requuid)
		}
	} else {
		// the member's ip is not owned by the local node, unassign it from the old owner,
		// assign to the local node and update db.
		err = d.serverIns.UnassignStaticIP(ctx, memberStaticIP.NetworkInterfaceID, memberStaticIP.StaticIP)
		if err != nil {
			errmsg = fmt.Sprintf("UnassignStaticIP error %s, ip %s member %s, requuid %s",
				err, memberStaticIP, member, requuid)
			glog.Errorln(errmsg)
			return errmsg
		}

		glog.Infoln("UnassignStaticIP from the old owner", memberStaticIP, "requuid", requuid)

		err = d.serverIns.AssignStaticIP(ctx, netInterface.InterfaceID, memberStaticIP.StaticIP)
		if err != nil {
			errmsg = fmt.Sprintf("AssignStaticIP error %s, ip %s local network interface %s member %s, requuid %s",
				err, memberStaticIP, netInterface.InterfaceID, member, requuid)
			glog.Errorln(errmsg)
			return errmsg
		}

		glog.Infoln("assigned static ip", memberStaticIP.StaticIP, "to local network",
			netInterface.InterfaceID, "member", member, "requuid", requuid)

		newip := db.UpdateServiceStaticIP(memberStaticIP, d.serverInfo.GetLocalInstanceID(), netInterface.InterfaceID)
		err = d.dbIns.UpdateServiceStaticIP(ctx, memberStaticIP, newip)
		if err != nil {
			errmsg = fmt.Sprintf("UpdateServiceStaticIP error %s, ip %s service %s member %s, requuid %s",
				err, memberStaticIP, serviceAttr, member, requuid)
			glog.Errorln(errmsg)
			return errmsg
		}

		glog.Infoln("updated static ip to local node", newip, "member", member, "requuid", requuid)
	}
	return ""
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
	glog.Infoln("get service attr", serviceAttr, "requuid", requuid)

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

func (d *FireCampVolumeDriver) createConfigFile(ctx context.Context, member *common.ServiceMember, requuid string) error {
	// if the service does not need the config file, return
	if len(member.Configs) == 0 {
		glog.Infoln("no config file", member, "requuid", requuid)
		return nil
	}

	// create the conf dir
	mpath := d.mountpoint(member.ServiceUUID)
	configDirPath := filepath.Join(mpath, common.DefaultConfigDir)
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
	d.unmountFS(mountPath)
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
	if len(member.Volumes.LogVolumeID) != 0 {
		err = d.detachVolumeFromLastOwner(ctx, member.Volumes.LogVolumeID, member.ServerInstanceID, member.Volumes.LogDeviceName, requuid)
		if err != nil {
			glog.Errorln("detach log volume from last owner error", err, "requuid", requuid, member.Volumes, member)
			return nil, err
		}
	}
	err = d.detachVolumeFromLastOwner(ctx, member.Volumes.PrimaryVolumeID, member.ServerInstanceID, member.Volumes.PrimaryDeviceName, requuid)
	if err != nil {
		glog.Errorln("detach primary volume from last owner error", err, "requuid", requuid, member.Volumes, member)
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
	mvols := common.MemberVolumes{
		PrimaryVolumeID:   utils.GetControlDBVolumeID(serviceUUID),
		PrimaryDeviceName: d.serverIns.GetControlDBDeviceName(),
	}
	member := db.CreateServiceMember(serviceUUID,
		common.ControlDBServiceName,
		d.serverInfo.GetLocalAvailabilityZone(),
		taskID,
		d.containerInfo.GetLocalContainerInstanceID(),
		d.serverInfo.GetLocalInstanceID(),
		mtime,
		mvols,
		"", // static ip
		nil)

	// construct the serviceAttr for the controldb service
	// taskCounts and volSizeGB are useless for the controldb service
	taskCounts := int64(1)
	volSizeGB := int64(0)
	registerDNS := true
	requireStaticIP := false
	devNames := common.ServiceDeviceNames{
		PrimaryDeviceName: d.serverIns.GetControlDBDeviceName(),
	}
	attr := db.CreateServiceAttr(serviceUUID, common.ServiceStatusActive, mtime, taskCounts,
		volSizeGB, d.containerInfo.GetContainerClusterID(), common.ControlDBServiceName,
		devNames, registerDNS, domainName, hostedZoneID, requireStaticIP)

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

	var localIPs map[string]bool
	if serviceAttr.RequireStaticIP {
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
		if member.AvailableZone != zone {
			glog.V(1).Infoln("member is not in local zone", zone, member)
			continue
		}

		// check if the member is idle
		_, ok := taskIDs[member.TaskID]
		if !ok {
			glog.Infoln("find idle member", member, "service", serviceAttr, "requuid", requuid)

			// select the idle member that is previously owned by the local node
			if member.ContainerInstanceID == d.containerInfo.GetLocalContainerInstanceID() {
				glog.Infoln("idle member is previously owned by the local instance", member, "requuid", requuid)
				return member, nil
			}

			// select the member if the member's static ip is assigned to the local node
			if serviceAttr.RequireStaticIP {
				_, local := localIPs[member.StaticIP]
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
		if member.ContainerInstanceID == d.containerInfo.GetLocalContainerInstanceID() {
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
	vols := &(member.Volumes)
	// attach the primary volume to host
	err := d.serverIns.AttachVolume(ctx, vols.PrimaryVolumeID, member.ServerInstanceID, vols.PrimaryDeviceName)
	if err != nil {
		glog.Errorln("attach the primary volume error", err, "requuid", requuid, vols, member)
		return err
	}

	if len(vols.LogVolumeID) != 0 {
		err := d.serverIns.AttachVolume(ctx, vols.PrimaryVolumeID, member.ServerInstanceID, vols.PrimaryDeviceName)
		if err != nil {
			glog.Errorln("attach the log volume error", err, "requuid", requuid, member.Volumes, member)
			d.serverIns.DetachVolume(ctx, vols.PrimaryVolumeID, member.ServerInstanceID, vols.PrimaryDeviceName)
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

	if len(vols.LogVolumeID) != 0 {
		err = d.serverIns.WaitVolumeAttached(ctx, vols.LogVolumeID)
		if err != nil {
			glog.Errorln("the log volume is still not attached, requuid", requuid, vols, member)
			// send request to detach the volume
			d.detachVolumes(ctx, member, requuid)
			return err
		}
	}

	glog.Infoln("attached member volumes, requuid", requuid, vols, member)
	return nil
}

func (d *FireCampVolumeDriver) detachVolumes(ctx context.Context, member *common.ServiceMember, requuid string) {
	vols := &(member.Volumes)

	err := d.serverIns.DetachVolume(ctx, vols.PrimaryVolumeID, d.serverInfo.GetLocalInstanceID(), vols.PrimaryDeviceName)
	if err != nil {
		glog.Errorln("detach the primary volume error", err, "requuid", requuid, vols, member)
	}

	if len(member.Volumes.LogVolumeID) != 0 {
		err = d.serverIns.DetachVolume(ctx, vols.LogVolumeID, d.serverInfo.GetLocalInstanceID(), vols.LogDeviceName)
		if err != nil {
			glog.Errorln("detach the log volume error", err, "requuid", requuid, vols, member)
		}
	}
}

func (d *FireCampVolumeDriver) formatVolumes(ctx context.Context, member *common.ServiceMember, requuid string) error {
	// format dev if necessary
	formatted := d.isFormatted(member.Volumes.PrimaryDeviceName)
	if !formatted {
		err := d.formatFS(member.Volumes.PrimaryDeviceName)
		if err != nil {
			glog.Errorln("format the primary device error", err, "requuid", requuid, member.Volumes)
			return err
		}
	}

	if len(member.Volumes.LogVolumeID) != 0 {
		formatted := d.isFormatted(member.Volumes.LogDeviceName)
		if !formatted {
			err := d.formatFS(member.Volumes.LogDeviceName)
			if err != nil {
				glog.Errorln("format the log device error", err, "requuid", requuid, member.Volumes)
				return err
			}
		}
	}

	return nil
}

func (d *FireCampVolumeDriver) mountVolumes(ctx context.Context, member *common.ServiceMember, requuid string) error {
	// mount the primary volume
	primaryMountPath := d.mountpoint(member.ServiceUUID)
	err := d.mountFS(member.Volumes.PrimaryDeviceName, primaryMountPath)
	if err != nil {
		glog.Errorln("mount primary device error", err, "requuid", requuid, "mount path", primaryMountPath, member.Volumes)
		return err
	}
	glog.Infoln("mount primary device to", primaryMountPath, "requuid", requuid, member.Volumes)

	// mount the log volume
	if len(member.Volumes.LogVolumeID) != 0 {
		mpath := d.mountpoint(member.ServiceUUID + common.NameSeparator + common.LogDevicePathSuffix)
		err := d.mountFS(member.Volumes.LogDeviceName, mpath)
		if err != nil {
			glog.Errorln("mount log device error", err, "requuid", requuid, "mount path", mpath, member.Volumes)
			// unmount the primary device. TODO what if the unmount fails?
			d.unmountFS(primaryMountPath)
			return err
		}
		glog.Infoln("mount log device to", mpath, "requuid", requuid, member.Volumes)
	}

	return nil
}

func (d *FireCampVolumeDriver) umountVolumes(ctx context.Context, member *common.ServiceMember, requuid string) error {
	// umount the primary volume
	mpath := d.mountpoint(member.ServiceUUID)
	err := d.unmountFS(mpath)
	if err != nil {
		glog.Errorln("umount primary device error", err, "requuid", requuid, "mount path", mpath, member.Volumes)
	} else {
		glog.Infoln("umount primary device from", mpath, "requuid", requuid, member.Volumes)
	}

	// umount the log volume
	if len(member.Volumes.LogVolumeID) != 0 {
		mpath = d.mountpoint(member.ServiceUUID + common.NameSeparator + common.LogDevicePathSuffix)
		err1 := d.unmountFS(mpath)
		if err1 != nil {
			glog.Errorln("umount log device error", err1, "requuid", requuid, "mount path", mpath, member.Volumes)
			return err1
		}
		glog.Infoln("umount log device from", mpath, "requuid", requuid, member.Volumes)
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

	args := d.getMountArgs(source, mountPath, defaultFSType, defaultMountOptions)

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
