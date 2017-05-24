package manageservice

import (
	"strconv"
	"strings"
	"testing"

	"github.com/golang/glog"

	"github.com/openconnectio/openmanage/db"
	"github.com/openconnectio/openmanage/dns"
	"github.com/openconnectio/openmanage/manage"
	"github.com/openconnectio/openmanage/utils"
)

func TestUtil_ServiceCreateion(t *testing.T, s *SCService, dbIns db.DB) {
	cluster := "cluster1"
	servicePrefix := "service-"
	service := "service-0"

	var volSize int64
	volSize = 1
	az := "az-west"

	hasMembership := true
	domain := "example.com"
	vpcID := "vpc-1"
	region := "us-west-1"

	max := 10
	idx := 0

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
			replicaCfg := &manage.ReplicaConfig{Configs: configs}
			replicaCfgs[i] = replicaCfg
		}

		req := &manage.CreateServiceRequest{
			Service: &manage.ServiceCommonRequest{
				Region:      region,
				Zone:        az,
				Cluster:     cluster,
				ServiceName: service,
			},
			Replicas:       int64(taskCount1),
			VolumeSizeGB:   volSize,
			HasMembership:  hasMembership,
			ReplicaConfigs: replicaCfgs,
		}

		svcUUID, err := s.CreateService(req, domain, vpcID)
		if err != nil {
			t.Fatalf("CreateService error %s, cluster %s, service %s taskCount1 %d",
				err, cluster, service, taskCount1)
		}

		glog.Infoln("created service", service, "uuid", svcUUID)

		err = s.SetServiceInitialized(cluster, service)
		if err != nil {
			t.Fatalf("SetServiceInitialized error %s, cluster %s, service %s, taskCount1 %d",
				err, cluster, service, taskCount1)
		}

		svcMap[service] = ""
		idx++
	}
	// verify the Device, Service, ServiceAttr and Volume
	deviceItems, err := dbIns.ListDevices(cluster)
	if err != nil || len(deviceItems) != 3 {
		t.Fatalf("ListDevices error %s, deviceItems %s", err, deviceItems)
	}
	serviceItems, err := dbIns.ListServices(cluster)
	if err != nil || len(serviceItems) != 3 {
		t.Fatalf("ListServices error %s, serviceItems %s", err, serviceItems)
	}
	for _, svc := range serviceItems {
		_, ok := svcMap[svc.ServiceName]
		if !ok {
			t.Fatalf("unexpected service %s, items %d", svc, serviceItems)
		}
		attr, err := dbIns.GetServiceAttr(svc.ServiceUUID)
		if err != nil || attr.ServiceName != svc.ServiceName {
			t.Fatalf("get service attr error %s service %s attr %s, expect ", err, svc, attr)
		}
		volItems, err := dbIns.ListVolumes(svc.ServiceUUID)
		if err != nil || len(volItems) != taskCount1 {
			t.Fatalf("got %d, expect %d volumes for service %s attr %s", len(volItems), taskCount1, svc, attr)
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
				Zone:        az,
				Cluster:     cluster,
				ServiceName: service,
			},
			Replicas:       int64(taskCount2),
			VolumeSizeGB:   volSize,
			HasMembership:  hasMembership,
			ReplicaConfigs: replicaCfgs,
		}

		svcUUID, err := s.CreateService(req, domain, vpcID)
		if err != nil {
			t.Fatalf("CreateService error %s, cluster %s, service %s taskCount2 %d",
				err, cluster, service, taskCount2)
		}

		glog.Infoln("created service", service, "uuid", svcUUID)

		err = s.SetServiceInitialized(cluster, service)
		if err != nil {
			t.Fatalf("SetServiceInitialized error %s, cluster %s, service %s, taskCount2 %d",
				err, cluster, service, taskCount2)
		}

		svcMap[service] = ""
		idx++
	}
	// verify the Device, Service and Volume
	deviceItems, err = dbIns.ListDevices(cluster)
	if err != nil || len(deviceItems) != 5 {
		t.Fatalf("ListDevices error %s, deviceItems %s", err, deviceItems)
	}
	serviceItems, err = dbIns.ListServices(cluster)
	if err != nil || len(serviceItems) != 5 {
		t.Fatalf("ListServices error %s, serviceItems %s", err, serviceItems)
	}
	for _, svc := range serviceItems {
		_, ok := svcMap[svc.ServiceName]
		if !ok {
			t.Fatalf("unexpected service %s, items %d", svc, serviceItems)
		}
		attr, err := dbIns.GetServiceAttr(svc.ServiceUUID)
		if err != nil || attr.ServiceName != svc.ServiceName {
			t.Fatalf("get service attr error %s service %s attr %s, expect ", err, svc, attr)
		}
		volItems, err := dbIns.ListVolumes(svc.ServiceUUID)
		if err != nil {
			t.Fatalf("ListVolumes error %s, serviceUUID %s", err, svc.ServiceUUID)
		}
		seqstr := strings.TrimPrefix(svc.ServiceName, servicePrefix)
		seq, err := strconv.Atoi(seqstr)
		if err != nil {
			t.Fatalf("Atoi seqstr %s error %s for service %s", seqstr, err, svc)
		}
		if seq < 3 {
			if len(volItems) != taskCount1 {
				t.Fatalf("got %d, expect %d volumes for service %s", len(volItems), taskCount1, svc)
			}
		} else {
			if seq < 5 {
				if len(volItems) != taskCount2 {
					t.Fatalf("got %d, expect %d volumes for service %s", len(volItems), taskCount2, svc)
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
				Zone:        az,
				Cluster:     cluster,
				ServiceName: service,
			},
			Replicas:       int64(taskCount3),
			VolumeSizeGB:   volSize,
			HasMembership:  hasMembership,
			ReplicaConfigs: replicaCfgs,
		}

		_, err := s.CreateService(req, domain, vpcID)
		if err != nil {
			t.Fatalf("CreateService error %s, cluster %s, service %s taskCount3 %d",
				err, cluster, service, taskCount3)
		}

		err = s.SetServiceInitialized(cluster, service)
		if err != nil {
			t.Fatalf("SetServiceInitialized error %s, cluster %s, service %s, taskCount3 %d",
				err, cluster, service, taskCount3)
		}

		idx++
	}

	// list volumes of the service
	service = servicePrefix + strconv.Itoa(idx-1)
	vols, err := s.ListVolumes(cluster, service)
	if err != nil || len(vols) != taskCount3 {
		t.Fatalf("ListVolumes %s error % vols %s", service, err, vols)
	}

	// test service deletion
	err = s.DeleteService(cluster, service)
	if err != nil {
		t.Fatalf("DeleteService %s error % vols %s", service, err, vols)
	}
}

func TestUtil_ServiceCreationRetry(t *testing.T, s *SCService, dbIns db.DB, dnsIns dns.DNS) {
	cluster := "cluster1"
	az := "az-west"
	servicePrefix := "service-"
	taskCount := 3

	var volSize int64
	volSize = 1
	idx := 0

	hasMembership := true
	domain := "example.com"
	vpcID := "vpc-1"
	region := "us-west-1"

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
			Zone:        az,
			Cluster:     cluster,
			ServiceName: service,
		},
		Replicas:       int64(taskCount),
		VolumeSizeGB:   volSize,
		HasMembership:  hasMembership,
		ReplicaConfigs: replicaCfgs,
	}

	// TODO test more cases. for example, service at different status, config file exists.
	// 1. device item exist
	dev, err := s.createDevice(cluster, service)
	if err != nil || dev != "/dev/loop1" {
		t.Fatalf("createDevice error %s, expectDev /dev/loop1, got %s", err, dev)
	}
	svcUUID, err := s.CreateService(req, domain, vpcID)
	if err != nil {
		t.Fatalf("CreateService error %s, cluster %s, service %s, taskCount %d",
			err, cluster, service, taskCount)
	}

	// 2. device and service item exist
	idx++
	service = servicePrefix + strconv.Itoa(idx)
	dev, err = s.createDevice(cluster, service)
	if err != nil || dev != "/dev/loop2" {
		t.Fatalf("createDevice error %s, expectDev /dev/loop2, got %s", err, dev)
	}

	serviceItem := db.CreateService(cluster, service, "uuid"+service)
	err = dbIns.CreateService(serviceItem)
	if err != nil {
		t.Fatalf("CreateService error %s, serviceItem %s", err, serviceItem)
	}

	// update the ServiceName in CreateServiceRequest
	req.Service.ServiceName = service

	svcUUID, err = s.CreateService(req, domain, vpcID)
	if err != nil || svcUUID != "uuid"+service {
		t.Fatalf("CreateService error %s, cluster %s, service %s, taskCount %d, uuid %s %s",
			err, cluster, service, taskCount, svcUUID, "uuid"+service)
	}

	// 3. device and service item exist, the 3rd device, service attr is at creating
	idx++
	service = servicePrefix + strconv.Itoa(idx)
	dev, err = s.createDevice(cluster, service)
	if err != nil || dev != "/dev/loop3" {
		t.Fatalf("createDevice error %s, expectDev /dev/loop3, got %s", err, dev)
	}

	serviceItem = db.CreateService(cluster, service, "uuid"+service)
	err = dbIns.CreateService(serviceItem)
	if err != nil {
		t.Fatalf("CreateService error %s, serviceItem %s", err, serviceItem)
	}

	hostedZoneID, err := dnsIns.GetOrCreateHostedZoneIDByName(domain, vpcID, region, true)
	if err != nil {
		t.Fatalf("GetOrCreateHostedZoneIDByName error %s, domain %s, vpc %s %s", err, domain, vpcID, region)
	}

	serviceAttr := db.CreateInitialServiceAttr("uuid"+service, int64(taskCount), volSize,
		cluster, service, dev, hasMembership, domain, hostedZoneID)
	err = dbIns.CreateServiceAttr(serviceAttr)
	if err != nil {
		t.Fatalf("CreateServiceAttr error %s, serviceAttr %s", err, serviceAttr)
	}

	// update the ServiceName in CreateServiceRequest
	req.Service.ServiceName = service

	svcUUID, err = s.CreateService(req, domain, vpcID)
	if err != nil || svcUUID != "uuid"+service {
		t.Fatalf("CreateService error %s, cluster %s, service %s, taskCount %d, uuid %s %s",
			err, cluster, service, taskCount, svcUUID, "uuid"+service)
	}

	// 4. device and service item exist, the 4th device, service attr is at creating, and one volume created
	idx++
	service = servicePrefix + strconv.Itoa(idx)
	dev, err = s.createDevice(cluster, service)
	if err != nil || dev != "/dev/loop4" {
		t.Fatalf("createDevice error %s, expectDev /dev/loop4, got %s", err, dev)
	}

	serviceItem = db.CreateService(cluster, service, "uuid"+service)
	err = dbIns.CreateService(serviceItem)
	if err != nil {
		t.Fatalf("CreateService error %s, serviceItem %s", err, serviceItem)
	}

	serviceAttr = db.CreateInitialServiceAttr("uuid"+service, int64(taskCount), volSize,
		cluster, service, dev, hasMembership, domain, hostedZoneID)
	err = dbIns.CreateServiceAttr(serviceAttr)
	if err != nil {
		t.Fatalf("CreateServiceAttr error %s, serviceAttr %s", err, serviceAttr)
	}

	memberName := utils.GenServiceMemberName(service, 0)
	cfgs, err := s.checkAndCreateConfigFile("uuid"+service, memberName, replicaCfgs[0])
	if err != nil {
		t.Fatalf("checkAndCreateConfigFile error %s, serviceItem %s", err, serviceItem)
	}

	_, err = s.createServiceVolume("uuid"+service, volSize, az, dev, memberName, cfgs)
	if err != nil {
		t.Fatalf("createServiceVolume error %s, serviceItem %s", err, serviceItem)
	}

	// update the ServiceName in CreateServiceRequest
	req.Service.ServiceName = service

	svcUUID, err = s.CreateService(req, domain, vpcID)
	if err != nil || svcUUID != "uuid"+service {
		t.Fatalf("CreateService error %s, cluster %s, service %s, taskCount %d, uuid %s %s",
			err, cluster, service, taskCount, svcUUID, "uuid"+service)
	}

}
