package utils

import (
	"errors"
	"fmt"
	"os"

	"github.com/golang/glog"
)

const (
	DefaultDirMode  = 0755
	DefaultFileMode = 0600
)

func IsFileExist(fpath string) (bool, error) {
	_, err := os.Stat(fpath)
	if err == nil {
		// file exists
		glog.V(5).Infoln("file exists", fpath)
		return true, nil
	}

	// err != nil, check if not exist
	if os.IsNotExist(err) {
		// file not exists
		glog.V(5).Infoln("file not exists", fpath)
		return false, nil
	}

	glog.Errorln("Stat file", fpath, "error", err)
	return false, err
}

func IsDirExist(dirpath string) (bool, error) {
	f, err := os.Stat(dirpath)
	if err == nil {
		// path exists
		if !f.IsDir() {
			errmsg := fmt.Sprintf("path %s exists, but not a dir", dirpath)
			glog.Errorln(errmsg)
			return false, errors.New(errmsg)
		}
		return true, nil
	}

	// err != nil, check if not exist
	if os.IsNotExist(err) {
		// dir not exists
		return false, nil
	}

	glog.Errorln("Stat dir", dirpath, "error", err)
	return false, err
}

func CreateDirIfNotExist(dirpath string) error {
	exist, err := IsDirExist(dirpath)
	if err != nil {
		return err
	}

	if exist {
		return nil
	}

	// dir not exists, create it
	err = os.MkdirAll(dirpath, DefaultDirMode)
	if err != nil && !os.IsExist(err) {
		glog.Errorln("Mkdir", dirpath, "error", err)
		return err
	}
	glog.Infoln("Mkdir done", dirpath)
	return nil
}
