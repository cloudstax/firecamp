package catalog

import (
	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/manage"
)

const separator = "="

// CreateSysConfigFile creates the content for the sys.conf file.
// like:
// SERVICE_MEMBER=memberName
func CreateSysConfigFile(memberName string) *manage.ReplicaConfigFile {
	content := SYS_SERVICE_MEMBER + separator + memberName
	return &manage.ReplicaConfigFile{
		FileName: SYS_FILE_NAME,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}
}
