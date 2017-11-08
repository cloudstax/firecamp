package catalog

import (
	"fmt"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/manage"
)

// CreateSysConfigFile creates the content for the sys.conf file.
// example:
//   PLATFORM=ecs
//   SERVICE_MEMBER=mycas-0.cluster-firecamp.com
func CreateSysConfigFile(platform string, memberDNSName string) *manage.ReplicaConfigFile {
	content := fmt.Sprintf(sysContent, platform, memberDNSName)
	return &manage.ReplicaConfigFile{
		FileName: SYS_FILE_NAME,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}
}

const (
	separator  = "="
	sysContent = `
PLATFORM=%s
SERVICE_MEMBER=%s
`
)

// MBToBytes converts MB to bytes
func MBToBytes(mb int64) int64 {
	return mb * 1024 * 1024
}
