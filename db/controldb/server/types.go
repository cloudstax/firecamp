package controldbserver

import "os"

const (
	maxServiceAttrCacheEntry = 100
	maxServiceMemberCacheEntry      = 20
	requestTimeOutSecs       = 10

	maxVersions   = 10
	checksumBytes = 16

	fileTypeDevice      = 0
	fileTypeService     = 1
	fileTypeServiceAttr = 2
	fileTypeServiceMember      = 3

	tmpFileOpenFlag   = os.O_WRONLY | os.O_CREATE | os.O_TRUNC | os.O_APPEND
	filepathSeparator = "."
	serviceDirName    = "servicedir"
	serviceMemberDirName     = "serviceMemberdir"
	configDirName     = "configdir"
	tmpFileSuffix     = ".tmp"

	deviceTmpFilePrefix          = "device.tmp"
	deviceVersionFilePrefix      = "device.v"
	serviceTmpFilePrefix         = "service.tmp"
	serviceVersionFilePrefix     = "service.v"
	serviceAttrTmpFilePrefix     = "attr.tmp"
	serviceAttrVersionFilePrefix = "attr.v"
)
