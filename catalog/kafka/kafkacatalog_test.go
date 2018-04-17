package kafkacatalog

import (
	"testing"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/catalog/zookeeper"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/manage"
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

	zkservice := "zk1"
	vols := common.ServiceVolumes{
		PrimaryDeviceName: "/dev/xvdh",
		PrimaryVolume: common.ServiceVolume{
			VolumeType:   common.VolumeTypeGPSSD,
			VolumeSizeGB: volSizeGB,
		},
	}
	zkservers := catalog.GenServiceMemberHostsWithPort(cluster, zkservice, replicas, zkcatalog.ClientPort)
	expectZkServers := "zk1-0.c1-firecamp.com:2181,zk1-1.c1-firecamp.com:2181,zk1-2.c1-firecamp.com:2181"
	if zkservers != expectZkServers {
		t.Fatalf("expect zk servers %s, get %s", expectZkServers, zkservers)
	}

	opts := &manage.CatalogKafkaOptions{
		Replicas:        replicas,
		Volume:          &vols.PrimaryVolume,
		HeapSizeMB:      maxMemMB,
		AllowTopicDel:   allowTopicDel,
		RetentionHours:  retentionHours,
		ZkServiceName:   zkservice,
		JmxRemoteUser:   "u1",
		JmxRemotePasswd: "p1",
	}
	cfgs := genMemberConfigs(platform, cluster, kafkaservice, azs, opts)
	if len(cfgs) != int(replicas) {
		t.Fatalf("expect %d replica configs, get %d", replicas, len(cfgs))
	}
}
