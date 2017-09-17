package manageservice

import (
	"strconv"
	"strings"
	"testing"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

func TestUtil_ServiceCreateion(t *testing.T, s *ManageService, dbIns db.DB) {
	cluster := "cluster1"
	servicePrefix := "service-"
	service := "service-0"

	var volSize int64
	volSize = 1
	az := "az-west"

	registerDNS := true
	domain := "example.com"
	vpcID := "vpc-1"
	region := "us-west-1"

	max := 10
	idx := 0

	ctx := context.Background()

	// service map, key: service name, value: ""
	svcMap := make(map[string]string)

	taskCount1 := 3
	for idx < 3 {
		service = servicePrefix + strconv.Itoa(idx)

		// create the config files for replicas
		replicaCfgs := make([]*manage.ReplicaConfig, taskCount1)
		for i := 0; i < taskCount1; i++ {
			cfg := &manage.ReplicaConfigFile{FileName: service, Content: service}
			configs := []*manage.ReplicaConfigFile{cfg}
			replicaCfg := &manage.ReplicaConfig{Zone: az, Configs: configs}
			replicaCfgs[i] = replicaCfg
		}

		req := &manage.CreateServiceRequest{
			Service: &manage.ServiceCommonRequest{
				Region:      region,
				Cluster:     cluster,
				ServiceName: service,
			},
			Replicas:       int64(taskCount1),
			VolumeSizeGB:   volSize,
			RegisterDNS:    registerDNS,
			ReplicaConfigs: replicaCfgs,
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

		svcMap[service] = ""
		idx++
	}
	// verify the Device, Service, ServiceAttr and ServiceMember
	deviceItems, err := dbIns.ListDevices(ctx, cluster)
	if err != nil || len(deviceItems) != 3 {
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
		if err != nil || attr.ServiceName != svc.ServiceName {
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
			cfg := &manage.ReplicaConfigFile{FileName: service, Content: service}
			configs := []*manage.ReplicaConfigFile{cfg}
			replicaCfg := &manage.ReplicaConfig{Configs: configs}
			replicaCfgs[i] = replicaCfg
		}

		req := &manage.CreateServiceRequest{
			Service: &manage.ServiceCommonRequest{
				Region:      region,
				Cluster:     cluster,
				ServiceName: service,
			},
			Replicas:       int64(taskCount2),
			VolumeSizeGB:   volSize,
			RegisterDNS:    registerDNS,
			ReplicaConfigs: replicaCfgs,
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

		svcMap[service] = ""
		idx++
	}
	// verify the Device, Service and ServiceMember
	deviceItems, err = dbIns.ListDevices(ctx, cluster)
	if err != nil || len(deviceItems) != 5 {
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
		if err != nil || attr.ServiceName != svc.ServiceName {
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
			cfg := &manage.ReplicaConfigFile{FileName: service, Content: service}
			configs := []*manage.ReplicaConfigFile{cfg}
			replicaCfg := &manage.ReplicaConfig{Configs: configs}
			replicaCfgs[i] = replicaCfg
		}

		req := &manage.CreateServiceRequest{
			Service: &manage.ServiceCommonRequest{
				Region:      region,
				Cluster:     cluster,
				ServiceName: service,
			},
			Replicas:       int64(taskCount3),
			VolumeSizeGB:   volSize,
			RegisterDNS:    registerDNS,
			ReplicaConfigs: replicaCfgs,
		}

		_, err := s.CreateService(ctx, req, domain, vpcID)
		if err != nil {
			t.Fatalf("CreateService error %s, cluster %s, service %s taskCount3 %d",
				err, cluster, service, taskCount3)
		}

		err = s.SetServiceInitialized(ctx, cluster, service)
		if err != nil {
			t.Fatalf("SetServiceInitialized error %s, cluster %s, service %s, taskCount3 %d",
				err, cluster, service, taskCount3)
		}

		idx++
	}

	// list volumes of the service
	service = servicePrefix + strconv.Itoa(idx-1)
	vols, err := s.ListServiceVolumes(ctx, cluster, service)
	if err != nil || len(vols) != taskCount3 {
		t.Fatalf("ListServiceVolumes %s error % vols %s", service, err, vols)
	}

	// test service deletion
	err = s.DeleteService(ctx, cluster, service)
	if err != nil {
		t.Fatalf("DeleteService %s error % vols %s", service, err, vols)
	}
}

func TestUtil_ServiceCreationRetry(t *testing.T, s *ManageService, dbIns db.DB, dnsIns dns.DNS) {
	cluster := "cluster1"
	az := "az-west"
	servicePrefix := "service-"
	taskCount := 3

	var volSize int64
	volSize = 1
	idx := 0

	registerDNS := true
	domain := "example.com"
	vpcID := "vpc-1"
	region := "us-west-1"
	requireStaticIP := false

	ctx := context.Background()

	service := servicePrefix + strconv.Itoa(idx)

	// create the config files for replicas
	replicaCfgs := make([]*manage.ReplicaConfig, taskCount)
	for i := 0; i < taskCount; i++ {
		cfg := &manage.ReplicaConfigFile{FileName: service, Content: service}
		configs := []*manage.ReplicaConfigFile{cfg}
		replicaCfg := &manage.ReplicaConfig{Configs: configs}
		replicaCfgs[i] = replicaCfg
	}

	req := &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     cluster,
			ServiceName: service,
		},
		Replicas:       int64(taskCount),
		VolumeSizeGB:   volSize,
		RegisterDNS:    registerDNS,
		ReplicaConfigs: replicaCfgs,
	}

	// TODO test more cases. for example, service at different status, config file exists.
	// 1. device item exist
	dev, err := s.createDevice(ctx, cluster, service)
	if err != nil || dev != "/dev/loop1" {
		t.Fatalf("createDevice error %s, expectDev /dev/loop1, got %s", err, dev)
	}
	svcUUID, err := s.CreateService(ctx, req, domain, vpcID)
	if err != nil {
		t.Fatalf("CreateService error %s, cluster %s, service %s, taskCount %d",
			err, cluster, service, taskCount)
	}

	// 2. device and service item exist
	idx++
	service = servicePrefix + strconv.Itoa(idx)
	dev, err = s.createDevice(ctx, cluster, service)
	if err != nil || dev != "/dev/loop2" {
		t.Fatalf("createDevice error %s, expectDev /dev/loop2, got %s", err, dev)
	}

	serviceItem := db.CreateService(cluster, service, "uuid"+service)
	err = dbIns.CreateService(ctx, serviceItem)
	if err != nil {
		t.Fatalf("CreateService error %s, serviceItem %s", err, serviceItem)
	}

	// update the ServiceName in CreateServiceRequest
	req.Service.ServiceName = service

	svcUUID, err = s.CreateService(ctx, req, domain, vpcID)
	if err != nil || svcUUID != "uuid"+service {
		t.Fatalf("CreateService error %s, cluster %s, service %s, taskCount %d, uuid %s %s",
			err, cluster, service, taskCount, svcUUID, "uuid"+service)
	}

	// 3. device and service item exist, the 3rd device, service attr is at creating
	idx++
	service = servicePrefix + strconv.Itoa(idx)
	dev, err = s.createDevice(ctx, cluster, service)
	if err != nil || dev != "/dev/loop3" {
		t.Fatalf("createDevice error %s, expectDev /dev/loop3, got %s", err, dev)
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

	serviceAttr := db.CreateInitialServiceAttr("uuid"+service, int64(taskCount), volSize,
		cluster, service, dev, registerDNS, domain, hostedZoneID, requireStaticIP)
	err = dbIns.CreateServiceAttr(ctx, serviceAttr)
	if err != nil {
		t.Fatalf("CreateServiceAttr error %s, serviceAttr %s", err, serviceAttr)
	}

	// update the ServiceName in CreateServiceRequest
	req.Service.ServiceName = service

	svcUUID, err = s.CreateService(ctx, req, domain, vpcID)
	if err != nil || svcUUID != "uuid"+service {
		t.Fatalf("CreateService error %s, cluster %s, service %s, taskCount %d, uuid %s %s",
			err, cluster, service, taskCount, svcUUID, "uuid"+service)
	}

	// 4. device and service item exist, the 4th device, service attr is at creating, and one serviceMember created
	idx++
	service = servicePrefix + strconv.Itoa(idx)
	dev, err = s.createDevice(ctx, cluster, service)
	if err != nil || dev != "/dev/loop4" {
		t.Fatalf("createDevice error %s, expectDev /dev/loop4, got %s", err, dev)
	}

	serviceItem = db.CreateService(cluster, service, "uuid"+service)
	err = dbIns.CreateService(ctx, serviceItem)
	if err != nil {
		t.Fatalf("CreateService error %s, serviceItem %s", err, serviceItem)
	}

	serviceAttr = db.CreateInitialServiceAttr("uuid"+service, int64(taskCount), volSize,
		cluster, service, dev, registerDNS, domain, hostedZoneID, requireStaticIP)
	err = dbIns.CreateServiceAttr(ctx, serviceAttr)
	if err != nil {
		t.Fatalf("CreateServiceAttr error %s, serviceAttr %s", err, serviceAttr)
	}

	memberName := utils.GenServiceMemberName(service, 0)
	cfgs, err := s.checkAndCreateConfigFile(ctx, "uuid"+service, memberName, replicaCfgs[0])
	if err != nil {
		t.Fatalf("checkAndCreateConfigFile error %s, serviceItem %s", err, serviceItem)
	}

	_, err = s.createServiceMember(ctx, "uuid"+service, volSize, az, dev, memberName, "", cfgs)
	if err != nil {
		t.Fatalf("createServiceServiceMember error %s, serviceItem %s", err, serviceItem)
	}

	// update the ServiceName in CreateServiceRequest
	req.Service.ServiceName = service

	svcUUID, err = s.CreateService(ctx, req, domain, vpcID)
	if err != nil || svcUUID != "uuid"+service {
		t.Fatalf("CreateService error %s, cluster %s, service %s, taskCount %d, uuid %s %s",
			err, cluster, service, taskCount, svcUUID, "uuid"+service)
	}

}
