package utils

import (
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	"hash/fnv"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/nu7hatch/gouuid"
	"golang.org/x/net/context"

	"github.com/jazzl0ver/firecamp/api/common"
)

func SetLogToStd() {
	flag.Set("logtostderr", "true")
	flag.Set("stderrthreshold", "INFO")
}

func SetLogDir() {
	// glog has log_dir flag to specify the log dir
	f := flag.Lookup("log_dir")
	if f.Value.String() == "" {
		// set the default log dir
		flag.Set("log_dir", common.DefaultLogDir)
	}

	err := CreateDirIfNotExist(f.Value.String())
	if err != nil {
		glog.Fatalln("create/check log dir error", err, f.Value.String())
	}
}

func CheckResource(r *common.Resources) error {
	if r.MaxCPUUnits != common.DefaultMaxCPUUnits && r.MaxCPUUnits < r.ReserveCPUUnits {
		return fmt.Errorf("invalid resource - MaxCPUUnits %d less than ReserveCPUUnits %d", r.MaxCPUUnits, r.ReserveCPUUnits)
	}
	if r.MaxMemMB != common.DefaultMaxMemoryMB && r.MaxMemMB < r.ReserveMemMB {
		return fmt.Errorf("invalid resource - MaxMemMB %d less than ReserveMemMB %d", r.MaxMemMB, r.ReserveMemMB)
	}
	return nil
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

// GenUUID returns a 32 bytes uuid string
func GenUUID() string {
	u, err := uuid.NewV4()
	if err != nil {
		// uuid gen error, switch to use timestamp
		glog.Errorln("uuid.NewV4 error", err)
		tmstr := strconv.FormatInt(time.Now().UnixNano(), 10)
		return GenMD5(tmstr)
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

// Int32Ptr returns the pointer to int32 variable
func Int32Ptr(i int32) *int32 {
	return &i
}

// Int64Ptr returns the pointer to int64 variable
func Int64Ptr(i int64) *int64 {
	return &i
}

// BoolPtr returns the pointer to bool variable
func BoolPtr(bl bool) *bool {
	return &bl
}

func GenServiceMemberName(serviceName string, index int64) string {
	return serviceName + common.NameSeparator + strconv.FormatInt(index, 10)
}

func GenConfigFileID(prefix string, configFileName string, version int64) string {
	// configFileName may include the '_', such as pg_hba.conf for postgres.
	// The '_' is not supported as k8s configmap name. convert configFileName to md5.
	fileID := GenMD5(configFileName)
	return prefix + common.NameSeparator + fileID + common.NameSeparator + strconv.FormatInt(version, 10)
}

func GetConfigFileVersion(fileID string) (version int64, err error) {
	fields := strings.Split(fileID, common.NameSeparator)
	versionStr := fields[len(fields)-1]
	return strconv.ParseInt(versionStr, 10, 64)
}

// GetNextIP gets the next unused ip.
// aws reserves like 10.0.0.0, 10.0.0.1, 10.0.0.2, 10.0.0.3 and 10.0.0.255,
// http://docs.aws.amazon.com/AmazonVPC/latest/UserGuide/VPC_Subnets.html
func GetNextIP(usedIPs map[string]bool, ipnet *net.IPNet, lastIP net.IP) (nextIP net.IP, err error) {
	lastipstr := lastIP.String()
	ip := net.ParseIP(lastipstr)
	for ipnet.Contains(ip) {
		// increase ip
		for j := len(ip) - 1; j >= 0; j-- {
			// skip the reserved ips. this may skip some usable ip, but it is ok.
			if ip[j] < 3 {
				ip[j] = 3
			}
			if ip[j] == 254 {
				continue
			}

			ip[j]++
			if ip[j] > 0 {
				break
			}
		}

		ipstr := ip.String()
		_, ok := usedIPs[ipstr]
		if !ok {
			glog.Infoln("find unused ip", ipstr, "last ip", lastipstr)
			return ip, nil
		}
	}

	glog.Errorln("not find unused ip, used ips", len(usedIPs), "ipnet", ipnet, "lastIP", lastIP)
	return nil, common.ErrInternal
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
