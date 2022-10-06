package firecampplugin

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang/glog"

	"github.com/jazzl0ver/firecamp/pkg/utils"
)

const (
	pluginShareDataDir          = "/var/lib/firecamp"
	pluginServiceMemberFileName = "member"
	memberKey                   = "SERVICE_MEMBER"
)

// CreatePluginServiceMemberFile creates the service member file for the log plugin.
// This func is only called by the volume plugin after the volume is mounted.
func CreatePluginServiceMemberFile(serviceUUID string, memberName string, requuid string) error {
	fdir := filepath.Join(pluginShareDataDir, serviceUUID)
	fpath := filepath.Join(fdir, pluginServiceMemberFileName)

	_, err := os.Stat(fpath)
	if err != nil {
		if !os.IsNotExist(err) {
			glog.Errorln("check plugin file existence error", err, fpath, "service member", memberName, "requuid", requuid)
			return err
		}
		// The plugin file exists. This is possible, as the volume plugin and log plugin are independent.
		// They may crash and restart at any time. For example, has to manually patch the volume/log plugin,
		// the plugin will be restarted. While, the service container will keep running.
		// Assume docker will call StartLogging again for the container.
		glog.Errorln("the plugin file exist", fpath, "service member", memberName, "requuid", requuid)
	}

	// the service member file not exist or exist.

	// create the dir. The dir may exist in the corner case that file creation failed and
	// os.RemoveAll also failed.
	err = utils.CreateDirIfNotExist(fdir)
	if err != nil {
		glog.Errorln("create dir error", err, fdir, "service member", memberName, "requuid", requuid)
		return err
	}

	content := fmt.Sprintf("%s=%s", memberKey, memberName)
	err = utils.CreateOrOverwriteFile(fpath, []byte(content), utils.DefaultFileMode)
	if err != nil {
		glog.Errorln("create member file error", err, fpath, "service member", memberName, "requuid", requuid)
		// delete the dir
		os.RemoveAll(fdir)
		return err
	}

	glog.Infoln("created member file", fpath, memberName, "requuid", requuid)
	return nil
}

// GetPluginServiceMember parses the service member from the plugin member file.
func GetPluginServiceMember(serviceUUID string, maxWaitSeconds int64, retryWaitSeconds int64) (memberName string, err error) {
	fpath := filepath.Join(pluginShareDataDir, serviceUUID, pluginServiceMemberFileName)
	content := ""
	for i := int64(0); i < maxWaitSeconds; i += retryWaitSeconds {
		b, err := ioutil.ReadFile(fpath)
		if err == nil {
			content = string(b)
			break
		}

		if !os.IsNotExist(err) {
			glog.Errorln("read member file error", err, fpath)
			return "", err
		}

		glog.Errorln("member file not exist, wait", retryWaitSeconds, "seconds", fpath)
		time.Sleep(time.Duration(retryWaitSeconds) * time.Second)
	}

	if len(content) == 0 {
		errmsg := fmt.Sprintf("member file not exist after %d seconds for service %s", maxWaitSeconds, serviceUUID)
		glog.Errorln(errmsg)
		return "", errors.New(errmsg)
	}

	glog.Infoln("read from member file", content, "service", serviceUUID)

	strs := strings.Split(content, "=")
	if len(strs) != 2 || strs[0] != memberKey {
		errmsg := fmt.Sprintf("invalid member file %s, content: %s", fpath, content)
		glog.Errorln(errmsg)
		return "", errors.New(errmsg)
	}

	memberName = strs[1]
	glog.Infoln("get service member", memberName, "from member file", fpath)
	return memberName, nil
}

// DeletePluginServiceMemberFile deletes the plugin service member file
func DeletePluginServiceMemberFile(serviceUUID string, requuid string) error {
	fdir := filepath.Join(pluginShareDataDir, serviceUUID)
	err := os.RemoveAll(fdir)
	if err != nil && !os.IsNotExist(err) {
		glog.Errorln("delete member dir error", err, fdir, "requuid", requuid)
		return err
	}
	glog.Infoln("member dir deleted", fdir, "requuid", requuid)
	return nil
}
