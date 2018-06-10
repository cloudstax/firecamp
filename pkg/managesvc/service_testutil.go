package managesvc

import (
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/api/manage"
	"github.com/cloudstax/firecamp/pkg/db"
	"github.com/cloudstax/firecamp/pkg/dns"
	"github.com/cloudstax/firecamp/pkg/server"
	"github.com/cloudstax/firecamp/pkg/utils"
)

func TestUtil_ServiceCreation(t *testing.T, s *ManageService, dbIns db.DB, serverIns *server.LoopServer, requireStaticIP bool) {
	cluster := "cluster1"
	servicePrefix := "service-"
	service := "service-0"

	volSize := int64(1)
	servicevol := &common.ServiceVolume{
		VolumeType:   common.VolumeTypeGPSSD,
		VolumeSizeGB: volSize,
	}

	az := "az-west"

	registerDNS := true
	domain := "example.com"
	vpcID := "vpc-1"
	region := "us-west-1"

	max := 10
	idx := 0

	firstIP := 4

	serviceCfgs := []*manage.ConfigFileContent{
		&manage.ConfigFileContent{FileName: "fname", FileMode: common.DefaultConfigFileMode, Content: "content"},
	}

	ctx := context.Background()

	// service map, key: service name, value: ""
	svcMap := make(map[string]string)

	taskCount1 := 3
	for idx < 3 {
		service = servicePrefix + strconv.Itoa(idx)

		// create the config files for replicas
		replicaCfgs := make([]*manage.ReplicaConfig, taskCount1)
		for i := 0; i < taskCount1; i++ {
			memberName := utils.GenServiceMemberName(service, int64(i))
			cfg := &manage.ConfigFileContent{FileName: service, Content: service}
			configs := []*manage.ConfigFileContent{cfg}
			replicaCfg := &manage.ReplicaConfig{Zone: az, MemberName: memberName, Configs: configs}
			replicaCfgs[i] = replicaCfg
		}

		var journalVol *common.ServiceVolume
		if idx == 2 {
			journalVol = &common.ServiceVolume{
				VolumeType:   common.VolumeTypeGPSSD,
				VolumeSizeGB: 1,
				Iops:         100,
				Encrypted:    false,
			}
		}

		req := &manage.CreateServiceRequest{
			Service: &manage.ServiceCommonRequest{
				Region:             region,
				Cluster:            cluster,
				ServiceName:        service,
				CatalogServiceType: common.CatalogService_Kafka,
			},
			Resource: &common.Resources{
				MaxCPUUnits:     common.DefaultMaxCPUUnits,
				ReserveCPUUnits: common.DefaultReserveCPUUnits,
				MaxMemMB:        common.DefaultMaxMemoryMB,
				ReserveMemMB:    common.DefaultReserveMemoryMB,
			},
			ServiceType:     common.ServiceTypeStateful,
			Replicas:        int64(taskCount1),
			Volume:          servicevol,
			JournalVolume:   journalVol,
			RegisterDNS:     registerDNS,
			RequireStaticIP: requireStaticIP,
			ServiceConfigs:  serviceCfgs,
			ReplicaConfigs:  replicaCfgs,
		}

		svcUUID, err := s.CreateService(ctx, req, domain, vpcID)
		if err != nil {
			t.Fatalf("CreateService error %s, cluster %s, service %s taskCount1 %d",
				err, cluster, service, taskCount1)
		}

		glog.Infoln("created service", service, "uuid", svcUUID)

		err = s.SetServiceInitialized(ctx, cluster, service)
		if err != nil {
			t.Fatalf("SetServiceInitialized error %s, cluster %s, service %s, taskCount1 %d",
				err, cluster, service, taskCount1)
		}

		// get service attr to verify
		sattr, err := dbIns.GetServiceAttr(ctx, svcUUID)
		if err != nil {
			t.Fatalf("GetServiceAttr error %s, cluster %s, service %s", err, cluster, service)
		}
		if idx == 2 {
			if len(sattr.Spec.Volumes.PrimaryDeviceName) == 0 || len(sattr.Spec.Volumes.JournalDeviceName) == 0 {
				t.Fatalf("expect journal device for service %s, service volumes %s", service, sattr.Spec.Volumes)
			}
		} else {
			if len(sattr.Spec.Volumes.PrimaryDeviceName) == 0 || len(sattr.Spec.Volumes.JournalDeviceName) != 0 {
				t.Fatalf("not expect journal device for service %s, service volumes %s", service, sattr.Spec.Volumes)
			}
		}

		// list service members to verify
		memberItems, err := dbIns.ListServiceMembers(ctx, svcUUID)
		if err != nil || len(memberItems) != taskCount1 {
			t.Fatalf("got %d, expect %d serviceMembers for service %s", len(memberItems), taskCount1, service)
		}
		sort.Slice(memberItems, func(i, j int) bool {
			return memberItems[i].MemberName < memberItems[j].MemberName
		})
		for i, member := range memberItems {
			name := utils.GenServiceMemberName(service, int64(i))
			if member.MemberName != name {
				t.Fatalf("expect member name %s, get %s", name, member)
			}
			if requireStaticIP {
				ipPrefix, _, _, _ := serverIns.GetCidrBlock()
				ip := ipPrefix + strconv.Itoa(firstIP+i)
				if member.Spec.StaticIP != ip {
					t.Fatalf("expect ip %s for member %s", ip, member)
				}
			}
			if idx == 2 {
				if len(member.Spec.Volumes.PrimaryVolumeID) == 0 || len(member.Spec.Volumes.PrimaryDeviceName) == 0 ||
					len(member.Spec.Volumes.JournalVolumeID) == 0 || len(member.Spec.Volumes.JournalDeviceName) == 0 {
					t.Fatalf("expect journal device for service %s, service volumes %s", service, member.Spec.Volumes)
				}
			} else {
				if len(member.Spec.Volumes.PrimaryVolumeID) == 0 || len(member.Spec.Volumes.PrimaryDeviceName) == 0 ||
					len(member.Spec.Volumes.JournalVolumeID) != 0 || len(member.Spec.Volumes.JournalDeviceName) != 0 {
					t.Fatalf("not expect journal device for service %s, service volumes %s", service, member.Spec.Volumes)
				}
			}
		}
		// increase the firstIP for the next service
		firstIP = firstIP + taskCount1

		svcMap[service] = ""
		idx++
	}
	// verify the Device, Service, ServiceAttr and ServiceMember
	deviceItems, err := dbIns.ListDevices(ctx, cluster)
	if err != nil || len(deviceItems) != 4 {
		t.Fatalf("ListDevices error %s, deviceItems %s", err, deviceItems)
	}
	serviceItems, err := dbIns.ListServices(ctx, cluster)
	if err != nil || len(serviceItems) != 3 {
		t.Fatalf("ListServices error %s, serviceItems %s", err, serviceItems)
	}
	for _, svc := range serviceItems {
		_, ok := svcMap[svc.ServiceName]
		if !ok {
			t.Fatalf("unexpected service %s, items %d", svc, serviceItems)
		}
		attr, err := dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
		if err != nil || attr.Meta.ServiceName != svc.ServiceName {
			t.Fatalf("get service attr error %s service %s attr %s, expect ", err, svc, attr)
		}
		memberItems, err := dbIns.ListServiceMembers(ctx, svc.ServiceUUID)
		if err != nil || len(memberItems) != taskCount1 {
			t.Fatalf("got %d, expect %d serviceMembers for service %s attr %s", len(memberItems), taskCount1, svc, attr)
		}
	}

	// create 2 more services
	taskCount2 := 2
	for idx < 5 {
		service = servicePrefix + strconv.Itoa(idx)

		// create the config files for replicas
		replicaCfgs := make([]*manage.ReplicaConfig, taskCount2)
		for i := 0; i < taskCount2; i++ {
			memberName := utils.GenServiceMemberName(service, int64(i))
			cfg := &manage.ConfigFileContent{FileName: service, Content: service}
			configs := []*manage.ConfigFileContent{cfg}
			replicaCfg := &manage.ReplicaConfig{Zone: az, MemberName: memberName, Configs: configs}
			replicaCfgs[i] = replicaCfg
		}

		var journalVol *common.ServiceVolume
		if idx == 4 {
			journalVol = &common.ServiceVolume{
				VolumeType:   common.VolumeTypeGPSSD,
				VolumeSizeGB: 1,
				Iops:         100,
				Encrypted:    false,
			}
		}

		req := &manage.CreateServiceRequest{
			Service: &manage.ServiceCommonRequest{
				Region:             region,
				Cluster:            cluster,
				ServiceName:        service,
				CatalogServiceType: common.CatalogService_Kafka,
			},
			Resource: &common.Resources{
				MaxCPUUnits:     common.DefaultMaxCPUUnits,
				ReserveCPUUnits: common.DefaultReserveCPUUnits,
				MaxMemMB:        common.DefaultMaxMemoryMB,
				ReserveMemMB:    common.DefaultReserveMemoryMB,
			},
			ServiceType:     common.ServiceTypeStateful,
			Replicas:        int64(taskCount2),
			Volume:          servicevol,
			JournalVolume:   journalVol,
			RegisterDNS:     registerDNS,
			RequireStaticIP: requireStaticIP,
			ServiceConfigs:  serviceCfgs,
			ReplicaConfigs:  replicaCfgs,
		}

		svcUUID, err := s.CreateService(ctx, req, domain, vpcID)
		if err != nil {
			t.Fatalf("CreateService error %s, cluster %s, service %s taskCount2 %d",
				err, cluster, service, taskCount2)
		}

		glog.Infoln("created service", service, "uuid", svcUUID)

		err = s.SetServiceInitialized(ctx, cluster, service)
		if err != nil {
			t.Fatalf("SetServiceInitialized error %s, cluster %s, service %s, taskCount2 %d",
				err, cluster, service, taskCount2)
		}

		// get service attr to verify
		sattr, err := dbIns.GetServiceAttr(ctx, svcUUID)
		if err != nil {
			t.Fatalf("GetServiceAttr error %s, cluster %s, service %s", err, cluster, service)
		}
		if idx == 4 {
			if len(sattr.Spec.Volumes.PrimaryDeviceName) == 0 || len(sattr.Spec.Volumes.JournalDeviceName) == 0 {
				t.Fatalf("expect journal device for service %s, service volumes %s", service, sattr.Spec.Volumes)
			}
		} else {
			if len(sattr.Spec.Volumes.PrimaryDeviceName) == 0 || len(sattr.Spec.Volumes.JournalDeviceName) != 0 {
				t.Fatalf("not expect journal device for service %s, service volumes %s", service, sattr.Spec.Volumes)
			}
		}

		// list service members to verify
		memberItems, err := dbIns.ListServiceMembers(ctx, svcUUID)
		if err != nil || len(memberItems) != taskCount2 {
			t.Fatalf("got %d, expect %d serviceMembers for service %s", len(memberItems), taskCount2, service)
		}
		sort.Slice(memberItems, func(i, j int) bool {
			return memberItems[i].MemberName < memberItems[j].MemberName
		})
		for i, member := range memberItems {
			name := utils.GenServiceMemberName(service, int64(i))
			if member.MemberName != name {
				t.Fatalf("expect member name %s, get %s", name, member)
			}
			if requireStaticIP {
				ipPrefix, _, _, _ := serverIns.GetCidrBlock()
				ip := ipPrefix + strconv.Itoa(firstIP+i)
				if member.Spec.StaticIP != ip {
					t.Fatalf("expect ip %s for member %s", ip, member)
				}
			}
			if idx == 4 {
				if len(member.Spec.Volumes.PrimaryVolumeID) == 0 || len(member.Spec.Volumes.PrimaryDeviceName) == 0 ||
					len(member.Spec.Volumes.JournalVolumeID) == 0 || len(member.Spec.Volumes.JournalDeviceName) == 0 {
					t.Fatalf("expect journal device for service %s, service volumes %s", service, member.Spec.Volumes)
				}
			} else {
				if len(member.Spec.Volumes.PrimaryVolumeID) == 0 || len(member.Spec.Volumes.PrimaryDeviceName) == 0 ||
					len(member.Spec.Volumes.JournalVolumeID) != 0 || len(member.Spec.Volumes.JournalDeviceName) != 0 {
					t.Fatalf("not expect journal device for service %s, service volumes %s", service, member.Spec.Volumes)
				}
			}
		}
		// increase the firstIP for the next service
		firstIP = firstIP + taskCount2

		svcMap[service] = ""
		idx++
	}
	// verify the Device, Service and ServiceMember
	deviceItems, err = dbIns.ListDevices(ctx, cluster)
	if err != nil || len(deviceItems) != 7 {
		t.Fatalf("ListDevices error %s, deviceItems %s", err, deviceItems)
	}
	serviceItems, err = dbIns.ListServices(ctx, cluster)
	if err != nil || len(serviceItems) != 5 {
		t.Fatalf("ListServices error %s, serviceItems %s", err, serviceItems)
	}
	for _, svc := range serviceItems {
		_, ok := svcMap[svc.ServiceName]
		if !ok {
			t.Fatalf("unexpected service %s, items %d", svc, serviceItems)
		}
		attr, err := dbIns.GetServiceAttr(ctx, svc.ServiceUUID)
		if err != nil || attr.Meta.ServiceName != svc.ServiceName {
			t.Fatalf("get service attr error %s service %s attr %s, expect ", err, svc, attr)
		}
		memberItems, err := dbIns.ListServiceMembers(ctx, svc.ServiceUUID)
		if err != nil {
			t.Fatalf("ListServiceMembers error %s, serviceUUID %s", err, svc.ServiceUUID)
		}
		seqstr := strings.TrimPrefix(svc.ServiceName, servicePrefix)
		seq, err := strconv.Atoi(seqstr)
		if err != nil {
			t.Fatalf("Atoi seqstr %s error %s for service %s", seqstr, err, svc)
		}
		if seq < 3 {
			if len(memberItems) != taskCount1 {
				t.Fatalf("got %d, expect %d serviceMembers for service %s", len(memberItems), taskCount1, svc)
			}
		} else {
			if seq < 5 {
				if len(memberItems) != taskCount2 {
					t.Fatalf("got %d, expect %d serviceMembers for service %s", len(memberItems), taskCount2, svc)
				}
			} else {
				t.Fatalf("unexpect seq %d, seqstr %s, service %s", seq, seqstr, svc)
			}
		}
	}

	// create 5 more services
	taskCount3 := 4
	for idx < max {
		service = servicePrefix + strconv.Itoa(idx)

		// create the config files for replicas
		replicaCfgs := make([]*manage.ReplicaConfig, taskCount3)
		for i := 0; i < taskCount3; i++ {
			memberName := utils.GenServiceMemberName(service, int64(i))
			cfg := &manage.ConfigFileContent{FileName: service, Content: service}
			configs := []*manage.ConfigFileContent{cfg}
			replicaCfg := &manage.ReplicaConfig{Zone: az, MemberName: memberName, Configs: configs}
			replicaCfgs[i] = replicaCfg
		}

		req := &manage.CreateServiceRequest{
			Service: &manage.ServiceCommonRequest{
				Region:             region,
				Cluster:            cluster,
				ServiceName:        service,
				CatalogServiceType: common.CatalogService_Kafka,
			},
			Resource: &common.Resources{
				MaxCPUUnits:     common.DefaultMaxCPUUnits,
				ReserveCPUUnits: common.DefaultReserveCPUUnits,
				MaxMemMB:        common.DefaultMaxMemoryMB,
				ReserveMemMB:    common.DefaultReserveMemoryMB,
			},
			ServiceType:     common.ServiceTypeStateful,
			Replicas:        int64(taskCount3),
			Volume:          servicevol,
			RegisterDNS:     registerDNS,
			RequireStaticIP: requireStaticIP,
			ServiceConfigs:  serviceCfgs,
			ReplicaConfigs:  replicaCfgs,
		}

		svcUUID, err := s.CreateService(ctx, req, domain, vpcID)
		if err != nil {
			t.Fatalf("CreateService error %s, cluster %s, service %s taskCount3 %d",
				err, cluster, service, taskCount3)
		}

		err = s.SetServiceInitialized(ctx, cluster, service)
		if err != nil {
			t.Fatalf("SetServiceInitialized error %s, cluster %s, service %s, taskCount3 %d",
				err, cluster, service, taskCount3)
		}

		// list service members to verify
		memberItems, err := dbIns.ListServiceMembers(ctx, svcUUID)
		if err != nil || len(memberItems) != taskCount3 {
			t.Fatalf("got %d, expect %d serviceMembers for service %s", len(memberItems), taskCount3, service)
		}
		sort.Slice(memberItems, func(i, j int) bool {
			return memberItems[i].MemberName < memberItems[j].MemberName
		})
		for i, member := range memberItems {
			name := utils.GenServiceMemberName(service, int64(i))
			if member.MemberName != name {
				t.Fatalf("expect member name %s, get %s", name, member)
			}
			if requireStaticIP {
				ipPrefix, _, _, _ := serverIns.GetCidrBlock()
				ip := ipPrefix + strconv.Itoa(firstIP+i)
				if member.Spec.StaticIP != ip {
					t.Fatalf("expect ip %s for member %s", ip, member)
				}
			}
		}
		// increase the firstIP for the next service
		firstIP = firstIP + taskCount3

		idx++
	}

	// list volumes of the service
	service = servicePrefix + strconv.Itoa(idx-1)
	vols, err := s.ListServiceVolumes(ctx, cluster, service)
	if err != nil || len(vols) != taskCount3 {
		t.Fatalf("ListServiceVolumes %s error % vols %s", service, err, vols)
	}

	// test service deletion
	vols, err = s.DeleteService(ctx, cluster, service)
	if err != nil {
		t.Fatalf("DeleteService %s error % vols %s", service, err, vols)
	}
}

func TestUtil_ServiceCreationRetry(t *testing.T, s *ManageService, dbIns db.DB, dnsIns dns.DNS, serverIns *server.LoopServer, requireStaticIP bool, requireJournalVolume bool) {
	cluster := "cluster1"
	az := "az-west"
	servicePrefix := "service-"
	taskCount := 3

	volSize := int64(1)
	servicevol := &common.ServiceVolume{
		VolumeType:   common.VolumeTypeGPSSD,
		VolumeSizeGB: volSize,
	}

	idx := 0
	firstIP := 4
	ipPrefix, _, _, _ := serverIns.GetCidrBlock()

	registerDNS := true
	domain := "example.com"
	vpcID := "vpc-1"
	region := "us-west-1"

	serviceCfgs := []*manage.ConfigFileContent{
		&manage.ConfigFileContent{FileName: "fname", FileMode: common.DefaultConfigFileMode, Content: "content"},
	}

	ctx := context.Background()

	service := servicePrefix + strconv.Itoa(idx)

	// create the config files for replicas
	replicaCfgs := make([]*manage.ReplicaConfig, taskCount)
	for i := 0; i < taskCount; i++ {
		memberName := utils.GenServiceMemberName(service, int64(i))
		cfg := &manage.ConfigFileContent{FileName: service, Content: service}
		configs := []*manage.ConfigFileContent{cfg}
		replicaCfg := &manage.ReplicaConfig{Zone: az, MemberName: memberName, Configs: configs}
		replicaCfgs[i] = replicaCfg
	}

	req := &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:             region,
			Cluster:            cluster,
			ServiceName:        service,
			CatalogServiceType: common.CatalogService_Kafka,
		},
		Resource: &common.Resources{
			MaxCPUUnits:     common.DefaultMaxCPUUnits,
			ReserveCPUUnits: common.DefaultReserveCPUUnits,
			MaxMemMB:        common.DefaultMaxMemoryMB,
			ReserveMemMB:    common.DefaultReserveMemoryMB,
		},
		ServiceType:     common.ServiceTypeStateful,
		Replicas:        int64(taskCount),
		Volume:          servicevol,
		RegisterDNS:     registerDNS,
		RequireStaticIP: requireStaticIP,
		ServiceConfigs:  serviceCfgs,
		ReplicaConfigs:  replicaCfgs,
	}
	if requireJournalVolume {
		req.JournalVolume = &common.ServiceVolume{
			VolumeType:   common.VolumeTypeGPSSD,
			VolumeSizeGB: volSize,
			Iops:         1,
			Encrypted:    false,
		}
	}

	// TODO test more cases. for example, service at different status, config file exists.
	// 1. device item exist
	dev, err := s.createDevice(ctx, cluster, service, "", "requuid")
	if err != nil || dev != "/dev/loop0" {
		t.Fatalf("createDevice error %s, expectDev /dev/loop0, got %s", err, dev)
	}
	svcUUID, err := s.CreateService(ctx, req, domain, vpcID)
	if err != nil {
		t.Fatalf("CreateService error %s, cluster %s, service %s, taskCount %d",
			err, cluster, service, taskCount)
	}
	// get service attr to verify
	sattr, err := dbIns.GetServiceAttr(ctx, svcUUID)
	if err != nil {
		t.Fatalf("GetServiceAttr error %s, cluster %s, service %s", err, cluster, service)
	}
	if sattr.Spec.Volumes.PrimaryDeviceName != "/dev/loop0" {
		t.Fatalf("expect primary device for service %s, service volumes %s", service, sattr.Spec.Volumes)
	}
	if requireJournalVolume {
		if sattr.Spec.Volumes.JournalDeviceName != "/dev/loop1" {
			t.Fatalf("expect journal device for service %s, service volumes %s", service, sattr.Spec.Volumes)
		}
	} else {
		if len(sattr.Spec.Volumes.JournalDeviceName) != 0 {
			t.Fatalf("not expect journal device for service %s, service volumes %s", service, sattr.Spec.Volumes)
		}
	}
	// list service members to verify
	memberItems, err := dbIns.ListServiceMembers(ctx, svcUUID)
	if err != nil || len(memberItems) != taskCount {
		t.Fatalf("got %d, expect %d serviceMembers for service %s", len(memberItems), taskCount, service)
	}
	sort.Slice(memberItems, func(i, j int) bool {
		return memberItems[i].MemberName < memberItems[j].MemberName
	})
	for i, member := range memberItems {
		name := utils.GenServiceMemberName(service, int64(i))
		if member.MemberName != name {
			t.Fatalf("expect member name %s, get %s", name, member)
		}
		if requireStaticIP {
			ip := ipPrefix + strconv.Itoa(firstIP+i)
			if member.Spec.StaticIP != ip {
				t.Fatalf("expect ip %s for member %s", ip, member)
			}
		}
		if requireJournalVolume {
			if len(member.Spec.Volumes.PrimaryVolumeID) == 0 || len(member.Spec.Volumes.PrimaryDeviceName) == 0 ||
				len(member.Spec.Volumes.JournalVolumeID) == 0 || len(member.Spec.Volumes.JournalDeviceName) == 0 {
				t.Fatalf("expect journal device for service %s, service volumes %s", service, member.Spec.Volumes)
			}
		} else {
			if len(member.Spec.Volumes.PrimaryVolumeID) == 0 || len(member.Spec.Volumes.PrimaryDeviceName) == 0 ||
				len(member.Spec.Volumes.JournalVolumeID) != 0 || len(member.Spec.Volumes.JournalDeviceName) != 0 {
				t.Fatalf("not expect journal device for service %s, service volumes %s", service, member.Spec.Volumes)
			}
		}
	}
	// increase the firstIP for the next service
	firstIP = firstIP + taskCount

	// 2. device and service item exist
	idx++
	service = servicePrefix + strconv.Itoa(idx)
	// update the ServiceName in CreateServiceRequest
	req.Service.ServiceName = service
	for i, cfg := range replicaCfgs {
		memberName := utils.GenServiceMemberName(service, int64(i))
		cfg.MemberName = memberName
	}

	dev, err = s.createDevice(ctx, cluster, service, "", "requuid")
	expectDev := "/dev/loop1"
	if requireJournalVolume {
		expectDev = "/dev/loop2"
	}
	if err != nil || dev != expectDev {
		t.Fatalf("createDevice error %s, expectDev %s got %s", err, expectDev, dev)
	}

	serviceItem := db.CreateService(cluster, service, "uuid"+service)
	err = dbIns.CreateService(ctx, serviceItem)
	if err != nil {
		t.Fatalf("CreateService error %s, serviceItem %s", err, serviceItem)
	}

	svcUUID, err = s.CreateService(ctx, req, domain, vpcID)
	if err != nil || svcUUID != "uuid"+service {
		t.Fatalf("CreateService error %s, cluster %s, service %s, taskCount %d, uuid %s %s",
			err, cluster, service, taskCount, svcUUID, "uuid"+service)
	}
	// get service attr to verify
	sattr, err = dbIns.GetServiceAttr(ctx, svcUUID)
	if err != nil {
		t.Fatalf("GetServiceAttr error %s, cluster %s, service %s", err, cluster, service)
	}
	if sattr.Spec.Volumes.PrimaryDeviceName != expectDev {
		t.Fatalf("expect primary device %s for service %s, service volumes %s", expectDev, service, sattr.Spec.Volumes)
	}
	if requireJournalVolume {
		expectDev = "/dev/loop3"
		if sattr.Spec.Volumes.JournalDeviceName != expectDev {
			t.Fatalf("expect journal device %s for service %s, service volumes %s", expectDev, service, sattr.Spec.Volumes)
		}
	} else {
		if len(sattr.Spec.Volumes.JournalDeviceName) != 0 {
			t.Fatalf("not expect journal device for service %s, service volumes %s", service, sattr.Spec.Volumes)
		}
	}
	// list service members to verify
	memberItems, err = dbIns.ListServiceMembers(ctx, svcUUID)
	if err != nil || len(memberItems) != taskCount {
		t.Fatalf("got %d, expect %d serviceMembers for service %s", len(memberItems), taskCount, service)
	}
	sort.Slice(memberItems, func(i, j int) bool {
		return memberItems[i].MemberName < memberItems[j].MemberName
	})
	for i, member := range memberItems {
		name := utils.GenServiceMemberName(service, int64(i))
		if member.MemberName != name {
			t.Fatalf("expect member name %s, get %s", name, member)
		}
		if requireStaticIP {
			ip := ipPrefix + strconv.Itoa(firstIP+i)
			if member.Spec.StaticIP != ip {
				t.Fatalf("expect ip %s for member %s", ip, member)
			}
		}
		if requireJournalVolume {
			if len(member.Spec.Volumes.PrimaryVolumeID) == 0 || len(member.Spec.Volumes.PrimaryDeviceName) == 0 ||
				len(member.Spec.Volumes.JournalVolumeID) == 0 || len(member.Spec.Volumes.JournalDeviceName) == 0 {
				t.Fatalf("expect journal device for service %s, service volumes %s", service, member.Spec.Volumes)
			}
		} else {
			if len(member.Spec.Volumes.PrimaryVolumeID) == 0 || len(member.Spec.Volumes.PrimaryDeviceName) == 0 ||
				len(member.Spec.Volumes.JournalVolumeID) != 0 || len(member.Spec.Volumes.JournalDeviceName) != 0 {
				t.Fatalf("not expect journal device for service %s, service volumes %s", service, member.Spec.Volumes)
			}
		}
	}
	// increase the firstIP for the next service
	firstIP = firstIP + taskCount

	// 3. device and service item exist, the 3rd device, service attr is at creating
	idx++
	service = servicePrefix + strconv.Itoa(idx)
	// update the ServiceName in CreateServiceRequest
	req.Service.ServiceName = service
	for i, cfg := range replicaCfgs {
		memberName := utils.GenServiceMemberName(service, int64(i))
		cfg.MemberName = memberName
	}

	dev, err = s.createDevice(ctx, cluster, service, "", "requuid")
	expectDev = "/dev/loop2"
	if requireJournalVolume {
		expectDev = "/dev/loop4"
	}
	if err != nil || dev != expectDev {
		t.Fatalf("createDevice error %s, expectDev %s, got %s", err, expectDev, dev)
	}

	serviceItem = db.CreateService(cluster, service, "uuid"+service)
	err = dbIns.CreateService(ctx, serviceItem)
	if err != nil {
		t.Fatalf("CreateService error %s, serviceItem %s", err, serviceItem)
	}

	hostedZoneID, err := dnsIns.GetOrCreateHostedZoneIDByName(ctx, domain, vpcID, region, true)
	if err != nil {
		t.Fatalf("GetOrCreateHostedZoneIDByName error %s, domain %s, vpc %s %s", err, domain, vpcID, region)
	}

	svols, err := s.createServiceVolumes(ctx, req)
	if err != nil {
		t.Fatalf("createServiceVolumes error %s", err)
	}
	if svols.PrimaryDeviceName != expectDev {
		t.Fatalf("expect device %s got %s", expectDev, svols.PrimaryDeviceName)
	}
	if requireJournalVolume && svols.JournalDeviceName != "/dev/loop5" {
		t.Fatalf("expect journal device /dev/loop5, get %s", svols.JournalDeviceName)
	}
	res := common.Resources{
		MaxCPUUnits:     common.DefaultMaxCPUUnits,
		ReserveCPUUnits: common.DefaultReserveCPUUnits,
		MaxMemMB:        common.DefaultMaxMemoryMB,
		ReserveMemMB:    common.DefaultReserveMemoryMB,
	}
	cfgids, err := s.checkAndCreateConfigFile(ctx, "uuid"+service, req.Service.ServiceName, req.ServiceConfigs)
	if err != nil {
		t.Fatalf("checkAndCreateConfigFile error %s, service %s", err, req.Service.ServiceName)
	}

	mtime := time.Now().UnixNano()
	attrMeta := db.CreateServiceMeta(cluster, service, mtime, common.ServiceTypeStateful, common.ServiceStatusCreating)
	attrSpec := db.CreateServiceSpec(int64(taskCount), &res, registerDNS, domain, hostedZoneID, requireStaticIP, cfgids, common.CatalogService_Kafka, svols)
	serviceAttr := db.CreateServiceAttr("uuid"+service, 0, attrMeta, attrSpec)

	err = dbIns.CreateServiceAttr(ctx, serviceAttr)
	if err != nil {
		t.Fatalf("CreateServiceAttr error %s, serviceAttr %s", err, serviceAttr)
	}

	svcUUID, err = s.CreateService(ctx, req, domain, vpcID)
	if err != nil || svcUUID != "uuid"+service {
		t.Fatalf("CreateService error %s, cluster %s, service %s, taskCount %d, uuid %s %s",
			err, cluster, service, taskCount, svcUUID, "uuid"+service)
	}
	// list service members to verify
	memberItems, err = dbIns.ListServiceMembers(ctx, svcUUID)
	if err != nil || len(memberItems) != taskCount {
		t.Fatalf("got %d, expect %d serviceMembers for service %s", len(memberItems), taskCount, service)
	}
	sort.Slice(memberItems, func(i, j int) bool {
		return memberItems[i].MemberName < memberItems[j].MemberName
	})
	for i, member := range memberItems {
		name := utils.GenServiceMemberName(service, int64(i))
		if member.MemberName != name {
			t.Fatalf("expect member name %s, get %s", name, member)
		}
		if requireStaticIP {
			ip := ipPrefix + strconv.Itoa(firstIP+i)
			if member.Spec.StaticIP != ip {
				t.Fatalf("expect ip %s for member %s", ip, member)
			}
		}
	}
	// increase the firstIP for the next service
	firstIP = firstIP + taskCount

	// 4. device and service item exist, the 4th device, service attr is at creating, and one serviceMember created
	idx++
	service = servicePrefix + strconv.Itoa(idx)
	// update the ServiceName in CreateServiceRequest
	req.Service.ServiceName = service
	for i, cfg := range replicaCfgs {
		memberName := utils.GenServiceMemberName(service, int64(i))
		cfg.MemberName = memberName
	}

	dev, err = s.createDevice(ctx, cluster, service, "", "requuid")
	expectDev = "/dev/loop3"
	if requireJournalVolume {
		expectDev = "/dev/loop6"
	}
	if err != nil || dev != expectDev {
		t.Fatalf("createDevice error %s, expectDev %s, got %s", err, expectDev, dev)
	}

	serviceItem = db.CreateService(cluster, service, "uuid"+service)
	err = dbIns.CreateService(ctx, serviceItem)
	if err != nil {
		t.Fatalf("CreateService error %s, serviceItem %s", err, serviceItem)
	}

	vols := common.ServiceVolumes{
		PrimaryDeviceName: dev,
		PrimaryVolume: common.ServiceVolume{
			VolumeType:   common.VolumeTypeGPSSD,
			VolumeSizeGB: volSize,
		},
	}
	if requireJournalVolume {
		journalDev, err := s.createDevice(ctx, cluster, service, expectDev, "requuid")
		if err != nil || journalDev != "/dev/loop7" {
			t.Fatalf("create journal device error %s, expectDev /dev/loop7, got %s", err, journalDev)
		}
		vols.JournalDeviceName = journalDev
		vols.JournalVolume = common.ServiceVolume{
			VolumeType:   common.VolumeTypeGPSSD,
			VolumeSizeGB: volSize,
			Iops:         1,
			Encrypted:    false,
		}
	}
	cfgids, err = s.checkAndCreateConfigFile(ctx, "uuid"+service, req.Service.ServiceName, req.ServiceConfigs)
	if err != nil {
		t.Fatalf("checkAndCreateConfigFile error %s, service %s", err, req.Service.ServiceName)
	}

	mtime = time.Now().UnixNano()
	attrMeta = db.CreateServiceMeta(cluster, service, mtime, common.ServiceTypeStateful, common.ServiceStatusCreating)
	attrSpec = db.CreateServiceSpec(int64(taskCount), &res, registerDNS, domain, hostedZoneID, requireStaticIP, cfgids, common.CatalogService_Kafka, &vols)
	serviceAttr = db.CreateServiceAttr("uuid"+service, 0, attrMeta, attrSpec)

	err = dbIns.CreateServiceAttr(ctx, serviceAttr)
	if err != nil {
		t.Fatalf("CreateServiceAttr error %s, serviceAttr %s", err, serviceAttr)
	}

	cfgs, err := s.checkAndCreateConfigFile(ctx, "uuid"+service, replicaCfgs[0].MemberName, replicaCfgs[0].Configs)
	if err != nil {
		t.Fatalf("checkAndCreateConfigFile error %s, serviceItem %s", err, serviceItem)
	}

	ip := common.DefaultHostIP
	if requireStaticIP {
		// create the next ip and assign to member
		assignedIPs := make(map[string]string)
		serviceip, err := s.createStaticIPsForZone(ctx, serviceAttr, assignedIPs, 1, az)
		if err != nil {
			t.Fatalf("createStaticIPsForZone error %s", err)
		}
		if len(serviceip) != 1 {
			t.Fatalf("expect 1 serviceip, get %d", len(serviceip))
		}
		ip = ipPrefix + strconv.Itoa(firstIP)
		if serviceip[0].StaticIP != ip {
			t.Fatalf("expect ip %s, get %s", ip, serviceip[0])
		}
	}

	_, err = s.createServiceMember(ctx, serviceAttr, az, replicaCfgs[0].MemberName, ip, cfgs)
	if err != nil {
		t.Fatalf("createServiceServiceMember error %s, serviceItem %s", err, serviceItem)
	}

	if requireStaticIP {
		// create the next ip and not assign to member
		assignedIPs := make(map[string]string)
		assignedIPs[ip] = replicaCfgs[0].MemberName
		serviceip, err := s.createStaticIPsForZone(ctx, serviceAttr, assignedIPs, 1, az)
		if err != nil {
			t.Fatalf("createStaticIPsForZone error %s", err)
		}
		if len(serviceip) != 1 {
			t.Fatalf("expect 1 serviceip, get %d", len(serviceip))
		}
		ip = ipPrefix + strconv.Itoa(firstIP+1)
		if serviceip[0].StaticIP != ip {
			t.Fatalf("expect ip %s, get %s", ip, serviceip[0])
		}
	}

	svcUUID, err = s.CreateService(ctx, req, domain, vpcID)
	if err != nil || svcUUID != "uuid"+service {
		t.Fatalf("CreateService error %s, cluster %s, service %s, taskCount %d, uuid %s %s",
			err, cluster, service, taskCount, svcUUID, "uuid"+service)
	}
	// get service attr to verify
	sattr, err = dbIns.GetServiceAttr(ctx, svcUUID)
	if err != nil {
		t.Fatalf("GetServiceAttr error %s, cluster %s, service %s", err, cluster, service)
	}
	if sattr.Spec.Volumes.PrimaryDeviceName != expectDev {
		t.Fatalf("expect primary device %s for service %s, service volumes %s", expectDev, service, sattr.Spec.Volumes)
	}
	if requireJournalVolume {
		expectDev = "/dev/loop7"
		if sattr.Spec.Volumes.JournalDeviceName != expectDev {
			t.Fatalf("expect journal device %s for service %s, service volumes %s", expectDev, service, sattr.Spec.Volumes)
		}
	} else {
		if len(sattr.Spec.Volumes.JournalDeviceName) != 0 {
			t.Fatalf("not expect journal device for service %s, service volumes %s", service, sattr.Spec.Volumes)
		}
	}
	// list service members to verify
	memberItems, err = dbIns.ListServiceMembers(ctx, svcUUID)
	if err != nil || len(memberItems) != taskCount {
		t.Fatalf("got %d, expect %d serviceMembers for service %s", len(memberItems), taskCount, service)
	}
	sort.Slice(memberItems, func(i, j int) bool {
		return memberItems[i].MemberName < memberItems[j].MemberName
	})
	for i, member := range memberItems {
		name := utils.GenServiceMemberName(service, int64(i))
		if member.MemberName != name {
			t.Fatalf("expect member name %s, get %s", name, member)
		}
		if requireStaticIP {
			ip := ipPrefix + strconv.Itoa(firstIP+i)
			if member.Spec.StaticIP != ip {
				t.Fatalf("expect ip %s for member %s", ip, member)
			}
		}
		if requireJournalVolume {
			if len(member.Spec.Volumes.PrimaryVolumeID) == 0 || len(member.Spec.Volumes.PrimaryDeviceName) == 0 ||
				len(member.Spec.Volumes.JournalVolumeID) == 0 || len(member.Spec.Volumes.JournalDeviceName) == 0 {
				t.Fatalf("expect journal device for service %s, service volumes %s", service, member.Spec.Volumes)
			}
		} else {
			if len(member.Spec.Volumes.PrimaryVolumeID) == 0 || len(member.Spec.Volumes.PrimaryDeviceName) == 0 ||
				len(member.Spec.Volumes.JournalVolumeID) != 0 || len(member.Spec.Volumes.JournalDeviceName) != 0 {
				t.Fatalf("not expect journal device for service %s, service volumes %s", service, member.Spec.Volumes)
			}
		}
	}
	// increase the firstIP for the next service
	firstIP = firstIP + taskCount

}
