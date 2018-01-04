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

func CreateConfigFile(ctx context.Context, configDirPath string, member *common.ServiceMember, dbIns db.DB) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// if the service does not need the config file, return
	if len(member.Configs) == 0 {
		glog.Infoln("no config file", member, "requuid", requuid)
		return nil
	}

	// create the conf dir if not exists
	err := utils.CreateDirIfNotExist(configDirPath)
	if err != nil {
		glog.Errorln("create the config dir error", err, configDirPath, "requuid", requuid, member)
		return err
	}

	// check the md5 first.
	// The config file content may not be small. For example, the default cassandra.yaml is 45K.
	// The config files are rarely updated after creation. No need to read the content over.
	for _, cfg := range member.Configs {
		// check and create the config file if necessary
		fpath := filepath.Join(configDirPath, cfg.FileName)
		exist, err := utils.IsFileExist(fpath)
		if err != nil {
			glog.Errorln("check the config file error", err, fpath, "requuid", requuid, member)
			return err
		}

		if exist {
			// the config file exists, check whether it is the same with the config in DB.
			fdata, err := ioutil.ReadFile(fpath)
			if err != nil {
				glog.Errorln("read the config file error", err, fpath, "requuid", requuid, member)
				return err
			}

			fmd5 := md5.Sum(fdata)
			fmd5str := hex.EncodeToString(fmd5[:])
			if fmd5str == cfg.FileMD5 {
				glog.Infoln("the config file has the latest content", fpath, "requuid", requuid, member)
				// check next config file
				continue
			}
			// the config content is changed, update the config file
			glog.Infoln("the config file changed, update it", fpath, "requuid", requuid, member)
		}

		// the config file not exists or content is changed
		// read the content
		cfgFile, err := dbIns.GetConfigFile(ctx, member.ServiceUUID, cfg.FileID)
		if err != nil {
			glog.Errorln("GetConfigFile error", err, fpath, "requuid", requuid, member)
			return err
		}

		data := []byte(cfgFile.Content)
		err = utils.CreateOrOverwriteFile(fpath, data, os.FileMode(cfgFile.FileMode))
		if err != nil {
			glog.Errorln("write the config file error", err, fpath, "requuid", requuid, member)
			return err
		}
		glog.Infoln("write the config file done for service", fpath, "requuid", requuid, member)
	}

	return nil
}
