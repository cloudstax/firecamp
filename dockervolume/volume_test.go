package openmanagedockervolume

import (
	"flag"
	"os/exec"
	"testing"

	"github.com/docker/go-plugins-helpers/volume"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/openconnectio/openmanage/common"
	"github.com/openconnectio/openmanage/containersvc"
	"github.com/openconnectio/openmanage/db"
	"github.com/openconnectio/openmanage/dns"
	"github.com/openconnectio/openmanage/manage"
	"github.com/openconnectio/openmanage/manage/service"
	"github.com/openconnectio/openmanage/server"
	"github.com/openconnectio/openmanage/utils"
)

func TestVolumeDriver(t *testing.T) {
	flag.Parse()
	//flag.Set("stderrthreshold", "INFO")

	// create OpenManageVolumeDriver
	dbIns := db.NewMemDB()
	mockDNS := dns.NewMockDNS()
	serverIns := server.NewLoopServer()
	mockServerInfo := server.NewMockServerInfo()
	contSvcIns := containersvc.NewMemContainerSvc()
	mockContInfo := containersvc.NewMockContainerSvcInfo()

	requuid := utils.GenRequestUUID()
	ctx := context.Background()
	ctx = utils.NewRequestContext(ctx, requuid)

	mgsvc := manageservice.NewManageService(dbIns, serverIns, mockDNS)

	driver := NewVolumeDriver(dbIns, mockDNS, serverIns, mockServerInfo, contSvcIns, mockContInfo)

	cluster := "cluster1"
	taskCounts := 1
	region := "us-west-1"
	az := "az-west"
	domain := "test.com"
	vpcID := "vpc1"

	// create the 1st service
	service1 := "service1"

	// create the config files for replicas
	replicaCfgs := make([]*manage.ReplicaConfig, taskCounts)
	for i := 0; i < taskCounts; i++ {
		cfg := &manage.ReplicaConfigFile{FileName: "configfile-name", Content: "configfile-content"}
		configs := []*manage.ReplicaConfigFile{cfg}
		replicaCfg := &manage.ReplicaConfig{Configs: configs}
		replicaCfgs[i] = replicaCfg
	}

	req := &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Zone:        az,
			Cluster:     cluster,
			ServiceName: service1,
		},
		Replicas:       int64(taskCounts),
		VolumeSizeGB:   int64(taskCounts + 1),
		HasMembership:  true,
		ReplicaConfigs: replicaCfgs,
	}

	uuid1, err := mgsvc.CreateService(ctx, req, domain, vpcID)
	if err != nil {
		t.Fatalf("CreateService error", err)
	}

	// create one task on the container instance
	task1 := "task1"
	err = contSvcIns.AddServiceTask(ctx, cluster, service1, task1, mockContInfo.GetLocalContainerInstanceID())
	if err != nil {
		t.Fatalf("AddServiceTask error", err)
	}

	volumeMountTest(t, driver, uuid1)

	// check the device is umounted.
	mountpath := driver.mountpoint(uuid1)
	if driver.isDeviceMountToPath(mountpath) {
		t.Fatalf("device is still mounted to %s", mountpath)
		runlsblk()
		rundf()
	}

	// create the 2nd service
	service2 := "service2"
	req.Service.ServiceName = service2
	uuid2, err := mgsvc.CreateService(ctx, req, domain, vpcID)
	if err != nil {
		t.Fatalf("CreateService error", err)
	}

	// create one task on the container instance
	task2 := "task2"
	err = contSvcIns.AddServiceTask(ctx, cluster, service2, task2, mockContInfo.GetLocalContainerInstanceID())
	if err != nil {
		t.Fatalf("AddServiceTask error", err)
	}

	vols, err := dbIns.ListVolumes(ctx, uuid2)
	if err != nil || len(vols) != 1 {
		t.Fatalf("ListVolumes error", err, "vols", vols)
	}
	vol := vols[0]
	vol.ContainerInstanceID = mockContInfo.GetLocalContainerInstanceID()

	driver2 := NewVolumeDriver(dbIns, mockDNS, serverIns, mockServerInfo, contSvcIns, mockContInfo)
	volumeMountTestWithDriverRestart(ctx, t, driver, driver2, uuid2, serverIns, vol)

	// check the device is umounted.
	mountpath = driver.mountpoint(uuid2)
	if driver.isDeviceMountToPath(mountpath) {
		t.Fatalf("device is still mounted to %s", mountpath)
		runlsblk()
		rundf()
	}
}

func volumeMountTest(t *testing.T, driver *OpenManageVolumeDriver, svcUUID string) {
	// mount the volume
	mreq := volume.MountRequest{Name: svcUUID}
	mresp := driver.Mount(mreq)
	if len(mresp.Err) != 0 {
		t.Fatalf("failed to mount volume", svcUUID, "error", mresp.Err)
	}

	expecterr := false
	// volume mounted, unmount before exit
	defer unmount(svcUUID, driver, t, expecterr)

	// mount again to test the multiple mounts on the same volume
	mresp = driver.Mount(mreq)
	if len(mresp.Err) != 0 {
		t.Fatalf("failed to mount volume", svcUUID, "error", mresp.Err)
	}

	// volume mounted, unmount before exit
	defer unmount(svcUUID, driver, t, expecterr)

	// get volume
	req := volume.Request{Name: svcUUID}
	resp := driver.Get(req)
	if len(resp.Err) != 0 {
		t.Fatalf("failed to get volume", svcUUID, "error", resp.Err)
	}
	glog.Infoln("get volume", svcUUID, "resp", resp)

	// list volume
	resp = driver.List(req)
	if len(resp.Err) != 0 {
		t.Fatalf("failed to list volumes, error", resp.Err)
	}
	glog.Infoln("list volumes", resp)

	// show misc info
	//runblkid("/dev/loop0")
	//rundf()
	//runlsblk()
}

func volumeMountTestWithDriverRestart(ctx context.Context, t *testing.T, driver *OpenManageVolumeDriver,
	driver2 *OpenManageVolumeDriver, svcUUID string, serverIns server.Server, vol *common.Volume) {
	// mount the volume
	mreq := volume.MountRequest{Name: svcUUID}
	mresp := driver.Mount(mreq)
	if len(mresp.Err) != 0 {
		t.Fatalf("failed to mount volume", svcUUID, "error", mresp.Err)
	}

	expecterr := true
	// volume mounted, unmount before exit
	defer unmount(svcUUID, driver2, t, expecterr)

	// mount again to test the multiple mounts on the same volume
	mresp = driver.Mount(mreq)
	if len(mresp.Err) != 0 {
		t.Fatalf("failed to mount volume", svcUUID, "error", mresp.Err)
	}

	// volume mounted, unmount before exit
	defer unmount(svcUUID, driver2, t, expecterr)
	defer serverIns.DetachVolume(ctx, vol.VolumeID, vol.ContainerInstanceID, vol.DeviceName)

	// get volume
	req := volume.Request{Name: svcUUID}
	resp := driver.Get(req)
	if len(resp.Err) != 0 {
		t.Fatalf("failed to get volume", svcUUID, "error", resp.Err)
	}
	glog.Infoln("get volume", svcUUID, "resp", resp)

	// list volume
	resp = driver.List(req)
	if len(resp.Err) != 0 {
		t.Fatalf("failed to list volumes, error", resp.Err)
	}
	glog.Infoln("list volumes", resp)

	// show misc info
	//runblkid("/dev/loop0")
	//rundf()
	//runlsblk()
}

func unmount(svcUUID string, driver *OpenManageVolumeDriver, t *testing.T, expecterr bool) {
	ureq := volume.UnmountRequest{Name: svcUUID}
	uresp := driver.Unmount(ureq)
	if expecterr {
		if len(uresp.Err) == 0 {
			t.Fatalf("failed to unmount volume", svcUUID, "error", uresp.Err)
		}
	} else {
		if len(uresp.Err) != 0 {
			t.Fatalf("failed to unmount volume", svcUUID, "error", uresp.Err)
		}
	}
}

func runblkid(dev string) {
	var args []string
	args = append(args, "blkid", dev)

	command := exec.Command(args[0], args[1:]...)

	output, err := command.CombinedOutput()
	glog.Infoln(args, "output", string(output[:]), "error", err)
}

func rundf() {
	var args []string
	args = append(args, "df", "-h")

	command := exec.Command(args[0], args[1:]...)

	output, err := command.CombinedOutput()
	glog.Errorln(args, "output", string(output[:]), "error", err)
}

func runlsblk() {
	var args []string
	args = append(args, "lsblk")

	command := exec.Command(args[0], args[1:]...)

	output, err := command.CombinedOutput()
	glog.Errorln(args, "output", string(output[:]), "error", err)
}
