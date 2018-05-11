package consulcatalog

import (
	"testing"

	"github.com/cloudstax/firecamp/api/catalog"
	"github.com/cloudstax/firecamp/api/common"
)

func TestConsulCatalog(t *testing.T) {
	platform := "ecs"
	cluster := "t1"
	service := "consul1"
	azs := []string{"us-east-1a", "us-east-1b", "us-east-1c"}
	opts := &catalog.CatalogConsulOptions{
		Replicas: 1,
		Volume: &common.ServiceVolume{
			VolumeType:   common.VolumeTypeGPSSD,
			VolumeSizeGB: 1,
		},
	}

	replCfgs := genReplicaConfigs(platform, cluster, service, azs, opts)
	if len(replCfgs) != 1 {
		t.Fatalf("expect 1 replica config, get %d", len(replCfgs))
	}
	if len(replCfgs[0].Configs) != 1 {
		t.Fatalf("expect 1 configs, get %d", len(replCfgs[0].Configs))
	}
	if replCfgs[0].Zone != azs[0] {
		t.Fatalf("expect zone %s, get %s", azs[0], replCfgs[0].Zone)
	}
}
