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

// CreateJmxRemotePasswdConfFile creates the jmx remote password file.
func CreateJmxRemotePasswdConfFile(jmxUser string, jmxPasswd string) *manage.ReplicaConfigFile {
	return &manage.ReplicaConfigFile{
		FileName: JmxRemotePasswdConfFileName,
		FileMode: JmxRemotePasswdConfFileMode,
		Content:  CreateJmxConfFileContent(jmxUser, jmxPasswd),
	}
}

// IsJmxConfFile checks if the file is jmx conf file
func IsJmxConfFile(filename string) bool {
	return filename == JmxRemotePasswdConfFileName
}

// CreateJmxConfFileContent returns the new jmxremote.password file content
func CreateJmxConfFileContent(jmxUser string, jmxPasswd string) string {
	return fmt.Sprintf(jmxFileContent, jmxUser, jmxPasswd)
}

// MBToBytes converts MB to bytes
func MBToBytes(mb int64) int64 {
	return mb * 1024 * 1024
}

const (
	separator  = "="
	sysContent = `
PLATFORM=%s
SERVICE_MEMBER=%s
`

	// jmx file content format: "user passwd"
	jmxFileContent = "%s %s\n"
)
