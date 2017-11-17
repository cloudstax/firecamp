package consulcatalog

import (
	"strings"
	"testing"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

func TestConsulCatalog(t *testing.T) {
	platform := "ecs"
	region := "us-east-1"
	cluster := "t1"
	service := "consul1"
	azs := []string{"us-east-1a", "us-east-1b", "us-east-1c"}
	opts := &manage.CatalogConsulOptions{
		Replicas: 1,
		Volume: &common.ServiceVolume{
			VolumeType:   common.VolumeTypeGPSSD,
			VolumeSizeGB: 1,
		},
	}

	replCfgs := GenReplicaConfigs(platform, region, cluster, service, azs, opts)
	if len(replCfgs) != 1 {
		t.Fatalf("expect 1 replica config, get %d", len(replCfgs))
	}
	if len(replCfgs[0].Configs) != 2 {
		t.Fatalf("expect 2 configs, get %d", len(replCfgs[0].Configs))
	}
	if replCfgs[0].Zone != azs[0] {
		t.Fatalf("expect zone %s, get %s", azs[0], replCfgs[0].Zone)
	}

	if !IsBasicConfigFile(replCfgs[0].Configs[1].FileName) {
		t.Fatalf("expect consul basic config file, %s", replCfgs[0].Configs[1].FileName)
	}

	content := replCfgs[0].Configs[1].Content
	domain := dns.GenDefaultDomainName(cluster)
	member := utils.GenServiceMemberName(service, 0)
	memberHost := dns.GenDNSName(member, domain)
	idx := strings.Index(content, "\"advertise_addr\": \""+memberHost+"\"")
	if idx == -1 {
		t.Fatalf("expect advertise addr as member dns name %s, %s", memberHost, content)
	}
	idx = strings.Index(content, "\"bind_addr\": \""+memberHost+"\"")
	if idx == -1 {
		t.Fatalf("expect bind addr as member dns name %s, %s", memberHost, content)
	}
	strs := strings.Split(content, memberHost)
	if len(strs) != 4 {
		t.Fatalf("expect 4 substrings, get %d", len(strs))
	}

	ip := "127.0.0.1"
	memberips := make(map[string]string)
	memberips[memberHost] = ip
	newContent := ReplaceMemberName(content, memberips)
	idx = strings.Index(newContent, "\"advertise_addr\": \""+ip+"\"")
	if idx == -1 {
		t.Fatalf("expect advertise addr as ip %s, %s", ip, newContent)
	}
	idx = strings.Index(newContent, "\"bind_addr\": \""+ip+"\"")
	if idx == -1 {
		t.Fatalf("expect bind addr as ip %s, %s", ip, newContent)
	}
	strs = strings.Split(newContent, ip)
	if len(strs) != 4 {
		t.Fatalf("expect 4 substrings, get %d, %s", len(strs), newContent)
	}
}
