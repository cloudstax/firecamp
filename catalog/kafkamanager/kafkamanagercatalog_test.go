package kafkamanagercatalog

import (
	"testing"
	"time"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
)

func TestKafkaManagerCatalog(t *testing.T) {
	platform := common.ContainerPlatformECS
	region := "region1"
	cluster := "c1"
	replicas := int64(3)
	volSizeGB := int64(1)
	maxMemMB := int64(128)
	domain := dns.GenDefaultDomainName(cluster)
	kmservice := "kmservice"
	kmuser := "kmuser"
	kmpd := "kmpd"

	zkservice := "zk1"
	vols := common.ServiceVolumes{
		PrimaryDeviceName: "/dev/xvdh",
		PrimaryVolume: common.ServiceVolume{
			VolumeType:   common.VolumeTypeGPSSD,
			VolumeSizeGB: volSizeGB,
		},
	}
	zkattr := db.CreateServiceAttr("zkuuid", common.ServiceStatusActive, time.Now().UnixNano(),
		replicas, cluster, zkservice, vols, true, domain, "hostedzone", false, nil, common.Resources{})
	zkservers := genZkServerList(zkattr)
	expectZkServers := "zk1-0.c1-firecamp.com:2181,zk1-1.c1-firecamp.com:2181,zk1-2.c1-firecamp.com:2181"
	if zkservers != expectZkServers {
		t.Fatalf("expect zk servers %s, get %s", expectZkServers, zkservers)
	}

	opts := &manage.CatalogKafkaManagerOptions{
		HeapSizeMB:    maxMemMB,
		ZkServiceName: zkservice,
		User:          kmuser,
		Password:      kmpd,
	}
	req := GenDefaultCreateServiceRequest(platform, region, cluster, kmservice, opts, &common.Resources{}, zkattr)
	if req.Replicas != 1 {
		t.Fatalf("expect 1 replica, get %s", req)
	}
	if req.Resource.ReserveMemMB != maxMemMB {
		t.Fatalf("expect reserved mem %d, get %d", maxMemMB, req.Resource.ReserveMemMB)
	}
	if req.Service.ServiceType != common.ServiceTypeStateless {
		t.Fatalf("expect stateless service, get %s", req)
	}
}
