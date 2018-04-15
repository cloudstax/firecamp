package dockervolume

import (
	"crypto/md5"
	"encoding/hex"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/utils"
)

func CreateConfigFile(ctx context.Context, configDirPath string, attr *common.ServiceAttr, member *common.ServiceMember, dbIns db.DB) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// if the service does not need the config file, return
	if len(member.Spec.Configs) == 0 {
		glog.Infoln("no config file", member, "requuid", requuid)
		return nil
	}

	// create the conf dir if not exists
	err := utils.CreateDirIfNotExist(configDirPath)
	if err != nil {
		glog.Errorln("create the config dir error", err, configDirPath, "requuid", requuid, member)
		return err
	}

	// create the service common configs
	for _, cfg := range attr.Spec.ServiceConfigs {
		err = checkAndCreateConfigFile(ctx, attr.ServiceUUID, configDirPath, &cfg, dbIns, requuid)
		if err != nil {
			return err
		}
	}

	// create the member configs
	for _, cfg := range member.Spec.Configs {
		err = checkAndCreateConfigFile(ctx, member.ServiceUUID, configDirPath, &cfg, dbIns, requuid)
		if err != nil {
			return err
		}
	}

	return nil
}

// check the md5 first.
// The config files are rarely updated after creation. No need to read the content over.
func checkAndCreateConfigFile(ctx context.Context, serviceUUID string, configDirPath string, cfg *common.ConfigID, dbIns db.DB, requuid string) error {
	// check and create the config file if necessary
	fpath := filepath.Join(configDirPath, cfg.FileName)
	exist, err := utils.IsFileExist(fpath)
	if err != nil {
		glog.Errorln("check the config file error", err, fpath, "requuid", requuid)
		return err
	}

	if exist {
		// the config file exists, check whether it is the same with the config in DB.
		fdata, err := ioutil.ReadFile(fpath)
		if err != nil {
			glog.Errorln("read the config file error", err, fpath, "requuid", requuid)
			return err
		}

		fmd5 := md5.Sum(fdata)
		fmd5str := hex.EncodeToString(fmd5[:])
		if fmd5str == cfg.FileMD5 {
			glog.Infoln("the config file has the latest content", fpath, "requuid", requuid)
			return nil
		}
		// the config content is changed, update the config file
		glog.Infoln("the config file changed, update it", fpath, "requuid", requuid)
	}

	// the config file not exists or content is changed
	// read the content
	cfgFile, err := dbIns.GetConfigFile(ctx, serviceUUID, cfg.FileID)
	if err != nil {
		glog.Errorln("GetConfigFile error", err, fpath, "requuid", requuid)
		return err
	}

	data := []byte(cfgFile.Content)
	err = utils.CreateOrOverwriteFile(fpath, data, os.FileMode(cfgFile.FileMode))
	if err != nil {
		glog.Errorln("write the config file error", err, fpath, "requuid", requuid)
		return err
	}

	glog.Infoln("write the config file done", fpath, "requuid", requuid)
	return nil
}
