package catalog

const (
	// The system variables in the sys.conf file
	SYS_FILE_NAME = "sys.conf"

	BindAllIP = "0.0.0.0"

	JmxRemotePasswdConfFileName = "jmxremote.password"
	JmxRemotePasswdConfFileMode = 0400
	// the default password is uuid for better security
	// every service will define its own jmx port
	JmxDefaultRemoteUser = "jmxuser"
	// jmx file content format: "user passwd"
	JmxFileContent = "%s %s\n"
)
