package utils

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/golang/glog"
)

const (
	DefaultDirMode  = 0755
	DefaultFileMode = 0600
	tmpfileSuffix   = ".tmp"
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

func CreateOrOverwriteFile(fpath string, data []byte, fileMode os.FileMode) error {
	// create the tmp file
	tmpfpath := fpath + tmpfileSuffix
	err := writeFile(tmpfpath, data, DefaultFileMode)
	if err != nil {
		glog.Errorln("write the tmp file error", err, tmpfpath)
		os.Remove(tmpfpath)
		return err
	}

	// chmod the file mode if necessary
	if fileMode != DefaultFileMode {
		err = os.Chmod(tmpfpath, fileMode)
		if err != nil {
			glog.Errorln("Chmod file error", err, tmpfpath)
			os.Remove(tmpfpath)
			return err
		}
	}

	// rename the tmp file to the target file
	err = os.Rename(tmpfpath, fpath)
	if err != nil {
		glog.Errorln("rename tmp file", tmpfpath, "to", fpath, "error", err)
		os.Remove(tmpfpath)
		return err
	}

	glog.Infoln("created the file", fpath)
	return nil
}

func writeFile(fpath string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_SYNC, perm)
	if err != nil {
		return err
	}
	n, err := f.Write(data)
	if err == nil && n < len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	err1 := f.Close()
	if err == nil {
		err = err1
	}
	return err
}
