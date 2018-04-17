package catalog

const (
	// The service configurable variables and some service specific variables
	SERVICE_FILE_NAME = "service.conf"
	// The specific variables for one service member
	MEMBER_FILE_NAME = "member.conf"

	BindAllIP = "0.0.0.0"

	JmxRemotePasswdConfFileName = "jmxremote.password"
	JmxRemoteAccessConfFileName = "jmxremote.access"
	JmxConfFileMode             = 0400
	// the default password is uuid for better security
	// every service will define its own jmx port
	JmxDefaultRemoteUser = "jmxuser"
	JmxReadOnlyAccess    = "readonly"
	JmxReadWriteAccess   = "readwrite"
)
