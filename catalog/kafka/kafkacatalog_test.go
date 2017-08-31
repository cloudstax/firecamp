package kafkacatalog

import (
	"testing"
	"time"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/dns"
)

func TestKafkaCatalog(t *testing.T) {
	platform := common.ContainerPlatformECS
	cluster := "c1"
	kafkaservice := "k1"
	azs := []string{"az1"}
	replicas := int64(3)
	volSizeGB := int64(1)
	maxMemMB := int64(128)
	allowTopicDel := true
	retentionHours := int64(10)
	domain := dns.GenDefaultDomainName(cluster)

	zkservice := "zk1"
	zkattr := db.CreateServiceAttr("zkuuid", common.ServiceStatusActive, time.Now().UnixNano(),
		replicas, volSizeGB, cluster, zkservice, "/dev/xvdh", true, domain, "hostedzone")
	zkservers := genZkServerList(zkattr)
	expectZkServers := "zk1-0.c1-firecamp.com:2181,zk1-1.c1-firecamp.com:2181,zk1-2.c1-firecamp.com:2181"
	if zkservers != expectZkServers {
		t.Fatalf("expect zk servers %s, get %s", expectZkServers, zkservers)
	}

	cfgs := GenReplicaConfigs(platform, cluster, kafkaservice, azs, replicas, maxMemMB, allowTopicDel, retentionHours, zkservers)
	if len(cfgs) != int(replicas) {
		t.Fatalf("expect %d replica configs, get %d", replicas, len(cfgs))
	}
}
