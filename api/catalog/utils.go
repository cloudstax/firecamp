package catalog

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cloudstax/firecamp/api/manage"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/utils"
)

// IsServiceConfigFile checks if the file is the service.conf file
func IsServiceConfigFile(filename string) bool {
	return filename == SERVICE_FILE_NAME
}

// IsMemberConfigFile checks if the file is the member.conf file
func IsMemberConfigFile(filename string) bool {
	return filename == MEMBER_FILE_NAME
}

// GenStatelessServiceReplicaConfigs generates the replica configs for the stateless service.
func GenStatelessServiceReplicaConfigs(cluster string, service string, replicas int) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)

	replicaCfgs := make([]*manage.ReplicaConfig, replicas)
	for i := 0; i < replicas; i++ {
		member := utils.GenServiceMemberName(service, int64(i))
		memberHost := dns.GenDNSName(member, domain)

		content := fmt.Sprintf("SERVICE_MEMBER=%s", memberHost)
		memberCfg := &manage.ConfigFileContent{
			FileName: MEMBER_FILE_NAME,
			FileMode: common.DefaultConfigFileMode,
			Content:  content,
		}

		configs := []*manage.ConfigFileContent{memberCfg}

		replicaCfgs[i] = &manage.ReplicaConfig{Zone: common.AnyAvailabilityZone, MemberName: member, Configs: configs}
	}
	return replicaCfgs
}

// GenServiceMemberURIs creates the list of URIs for all service members,
// example: http://myes-0.t1-firecamp.com:9200,http://myes-1.t1-firecamp.com:9200
func GenServiceMemberURIs(cluster string, service string, replicas int64, port int64) string {
	hosts := ""
	domain := dns.GenDefaultDomainName(cluster)
	for i := int64(0); i < replicas; i++ {
		member := utils.GenServiceMemberName(service, i)
		dnsname := dns.GenDNSName(member, domain)
		if len(hosts) == 0 {
			hosts = fmt.Sprintf("http://%s:%d", dnsname, port)
		} else {
			hosts += fmt.Sprintf(",http://%s:%d", dnsname, port)
		}
	}
	return hosts
}

// GenServiceMemberHosts creates the hostname list of all service members,
// example: myzoo-0.t1-firecamp.com,myzoo-1.t1-firecamp.com
// Note: this currently works for the service that all members have a single name format, such as ZooKeeper, Kafka, etc.
// For service such as ElasticSearch, MongoDB, that has different name formats, this is not suitable.
func GenServiceMemberHosts(cluster string, service string, replicas int64) string {
	hosts := ""
	domain := dns.GenDefaultDomainName(cluster)
	for i := int64(0); i < replicas; i++ {
		member := utils.GenServiceMemberName(service, i)
		dnsname := dns.GenDNSName(member, domain)
		if len(hosts) == 0 {
			hosts = dnsname
		} else {
			hosts += fmt.Sprintf(",%s", dnsname)
		}
	}
	return hosts
}

// GenServiceMemberHostsWithPort creates the hostname:port list of all service members,
// example: myzoo-0.t1-firecamp.com:2181,myzoo-1.t1-firecamp.com:2181
// Note: this currently works for the service that all members have a single name format, such as ZooKeeper, Kafka, etc.
// For service such as ElasticSearch, MongoDB, that has different name formats, this is not suitable.
func GenServiceMemberHostsWithPort(cluster string, service string, replicas int64, port int64) string {
	hosts := ""
	domain := dns.GenDefaultDomainName(cluster)
	for i := int64(0); i < replicas; i++ {
		member := utils.GenServiceMemberName(service, i)
		dnsname := dns.GenDNSName(member, domain)
		if len(hosts) == 0 {
			hosts = fmt.Sprintf("%s:%d", dnsname, port)
		} else {
			hosts += fmt.Sprintf(",%s:%d", dnsname, port)
		}
	}
	return hosts
}

// ValidateUpdateOptions checks if the update options are valid
func ValidateUpdateOptions(heapSizeMB int64, jmxUser string, jmxPasswd string) error {
	if heapSizeMB < 0 {
		return errors.New("heap size should not be less than 0")
	}
	if len(jmxUser) != 0 && len(jmxPasswd) == 0 {
		return errors.New("please set the new jmx remote password")
	}
	return nil
}

// UpdateServiceConfigHeapAndJMX updates the service.conf file content
func UpdateServiceConfigHeapAndJMX(oldContent string, heapSizeMB int64, jmxUser string, jmxPasswd string) string {
	content := oldContent
	lines := strings.Split(oldContent, "\n")
	for _, line := range lines {
		if heapSizeMB > 0 && strings.HasPrefix(line, "HEAP_SIZE_MB") {
			newHeap := fmt.Sprintf("HEAP_SIZE_MB=%d", heapSizeMB)
			content = strings.Replace(content, line, newHeap, 1)
		}
		if len(jmxUser) != 0 && len(jmxPasswd) != 0 {
			if strings.HasPrefix(line, "JMX_REMOTE_USER") {
				newUser := fmt.Sprintf("JMX_REMOTE_USER=%s", jmxUser)
				content = strings.Replace(content, line, newUser, 1)
			} else if strings.HasPrefix(line, "JMX_REMOTE_PASSWD") {
				newPasswd := fmt.Sprintf("JMX_REMOTE_PASSWD=%s", jmxPasswd)
				content = strings.Replace(content, line, newPasswd, 1)
			}
		}
	}
	return content
}
