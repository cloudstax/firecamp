package controldbserver

import "os"

const (
	maxServiceAttrCacheEntry = 100
	maxVolumeCacheEntry      = 20
	requestTimeOutSecs       = 10

	maxVersions   = 10
	checksumBytes = 16

	fileTypeDevice      = 0
	fileTypeService     = 1
	fileTypeServiceAttr = 2
	fileTypeVolume      = 3

	tmpFileOpenFlag   = os.O_WRONLY | os.O_CREATE | os.O_TRUNC | os.O_APPEND
	filepathSeparator = "."
	serviceDirName    = "servicedir"
	volumeDirName     = "volumedir"
	configDirName     = "configdir"
	tmpFileSuffix     = ".tmp"

	deviceTmpFilePrefix          = "device.tmp"
	deviceVersionFilePrefix      = "device.v"
	serviceTmpFilePrefix         = "service.tmp"
	serviceVersionFilePrefix     = "service.v"
	serviceAttrTmpFilePrefix     = "attr.tmp"
	serviceAttrVersionFilePrefix = "attr.v"
)
