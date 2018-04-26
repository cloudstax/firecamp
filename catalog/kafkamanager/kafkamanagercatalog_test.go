package kmcatalog

import (
	"testing"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/catalog/zookeeper"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/manage"
)

func TestKafkaManagerCatalog(t *testing.T) {
	platform := common.ContainerPlatformECS
	region := "region1"
	cluster := "c1"
	replicas := int64(3)
	maxMemMB := int64(128)
	kmservice := "kmservice"
	kmuser := "kmuser"
	kmpd := "kmpd"

	zkservice := "zk1"
	zkservers := catalog.GenServiceMemberHostsWithPort(cluster, zkservice, replicas, zkcatalog.ClientPort)
	expectZkServers := "zk1-0.c1-firecamp.com:2181,zk1-1.c1-firecamp.com:2181,zk1-2.c1-firecamp.com:2181"
	if zkservers != expectZkServers {
		t.Fatalf("expect zk servers %s, get %s", expectZkServers, zkservers)
	}

	res := &common.Resources{}
	opts := &manage.CatalogKafkaManagerOptions{
		HeapSizeMB:    maxMemMB,
		ZkServiceName: zkservice,
		User:          kmuser,
		Password:      kmpd,
	}
	req := GenDefaultCreateServiceRequest(platform, region, cluster, kmservice, zkservers, opts, res)
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
