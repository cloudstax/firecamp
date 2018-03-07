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
		FileMode: JmxConfFileMode,
		Content:  CreateJmxPasswdConfFileContent(jmxUser, jmxPasswd),
	}
}

// IsJmxPasswdConfFile checks if the file is jmx password conf file
func IsJmxPasswdConfFile(filename string) bool {
	return filename == JmxRemotePasswdConfFileName
}

// CreateJmxPasswdConfFileContent returns the jmxremote.password file content
func CreateJmxPasswdConfFileContent(jmxUser string, jmxPasswd string) string {
	return fmt.Sprintf(jmxPasswdFileContent, jmxUser, jmxPasswd)
}

// CreateJmxRemoteAccessConfFile creates the jmx remote access file.
func CreateJmxRemoteAccessConfFile(jmxUser string, accessPerm string) *manage.ReplicaConfigFile {
	return &manage.ReplicaConfigFile{
		FileName: JmxRemoteAccessConfFileName,
		FileMode: JmxConfFileMode,
		Content:  CreateJmxAccessConfFileContent(jmxUser, accessPerm),
	}
}

// IsJmxAccessConfFile checks if the file is jmx access conf file
func IsJmxAccessConfFile(filename string) bool {
	return filename == JmxRemoteAccessConfFileName
}

// CreateJmxAccessConfFileContent returns the jmxremote.access file content
func CreateJmxAccessConfFileContent(jmxUser string, accessPerm string) string {
	return fmt.Sprintf(jmxAccessFileContent, jmxUser, accessPerm)
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

	// jmx passwd file content format: "user passwd"
	jmxPasswdFileContent = "%s %s\n"

	// jmx access file content format: "user accessPermission"
	jmxAccessFileContent = "%s %s\n"
)
