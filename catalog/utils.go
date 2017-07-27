package catalog

import (
	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/manage"
)

const separator = "="

// CreateSysConfigFile creates the content for the sys.conf file.
// example: SERVICE_MEMBER=mycas-0.cluster-openmanage.com
func CreateSysConfigFile(memberDNSName string) *manage.ReplicaConfigFile {
	content := SYS_SERVICE_MEMBER + separator + memberDNSName
	return &manage.ReplicaConfigFile{
		FileName: SYS_FILE_NAME,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}
}
