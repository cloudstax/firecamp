package utils

import (
	"crypto/md5"
	"encoding/hex"
	"flag"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/nu7hatch/gouuid"
	"golang.org/x/net/context"

	"github.com/openconnectio/openmanage/common"
)

func SetLogDir() {
	// glog has log_dir flag to specify the log dir
	f := flag.Lookup("log_dir")
	if f.Value.String() == "" {
		// set the default log dir
		flag.Set("log_dir", common.DefaultLogDir)
	}

	err := CreateDirIfNotExist(f.Value.String())
	if err != nil {
		glog.Fatalln("create/check controldb log dir error", err, f.Value.String())
	}
}

func Hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

func GenMD5(content string) string {
	data := md5.Sum([]byte(content))
	return hex.EncodeToString(data[:])
}

func GenUUID() string {
	u, err := uuid.NewV4()
	if err != nil {
		// uuid gen error, switch to use timestamp
		glog.Errorln("uuid.NewV4 error", err)
		return "time-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(u[:])
}

func GenRequestUUID() string {
	return "req-" + GenUUID()
}

// timestamp + uuid
func GenCallerID() string {
	sep := "-"
	t := time.Now().Unix()
	uuid := strconv.FormatInt(t, 10) + sep + GenUUID()
	return uuid
}

func GenServiceMemberName(serviceName string, index int64) string {
	return serviceName + common.NameSeparator + strconv.FormatInt(index, 10)
}

func GenServiceMemberConfigID(memberName string, configFileName string, version int64) string {
	return memberName + common.NameSeparator + configFileName + common.NameSeparator + strconv.FormatInt(version, 10)
}

func GenControlDBServiceUUID(volID string) string {
	return common.ControlDBUUIDPrefix + volID
}

func IsControlDBService(serviceUUID string) bool {
	return strings.HasPrefix(serviceUUID, common.ControlDBUUIDPrefix)
}

// GetControlDBVolumeID extracts the volume id from the controldb serviceUUID
func GetControlDBVolumeID(controldbServiceUUID string) string {
	return strings.TrimPrefix(controldbServiceUUID, common.ControlDBUUIDPrefix)
}

// The key type is unexported to prevent collisions with context keys defined in
// other packages.
type key int

// reqIDKey is the context key for the requuid.  Its value of zero is
// arbitrary.  If this package defined other context keys, they would have
// different integer values.
const reqIDKey key = 0

// NewRequestContext returns a new Context carrying requuid.
func NewRequestContext(ctx context.Context, requuid string) context.Context {
	return context.WithValue(ctx, reqIDKey, requuid)
}

// GetReqIDFromContext gets the requuid from ctx.
func GetReqIDFromContext(ctx context.Context) string {
	// ctx.Value returns nil if ctx has no value for the key;
	requuid, _ := ctx.Value(reqIDKey).(string)
	return requuid
}
