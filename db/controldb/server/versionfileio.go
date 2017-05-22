package controldbserver

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"io/ioutil"
	"os"

	"github.com/golang/glog"

	"github.com/openconnectio/openmanage/db"
	"github.com/openconnectio/openmanage/utils"
)

// create the file, append checksum and then data to the file
func createFile(filepath string, data []byte, op string, requuid string) error {
	checksum := md5.Sum(data)

	// open the tmp file
	file, err := os.OpenFile(filepath, tmpFileOpenFlag, utils.DefaultFileMode)
	if err != nil {
		glog.Errorln(op, "open file", filepath, "error", err, "requuid", requuid)
		return err
	}
	defer file.Close()

	// write checksum to the head of the file
	n, err := file.Write(checksum[:])
	if err != nil {
		glog.Errorln(op, "write checksum to file", filepath, "error", err, "requuid", requuid)
		return err
	}
	if n != len(checksum) {
		glog.Errorln(op, "write checksum to file", filepath, "error short write, requuid", requuid)
		return io.ErrShortWrite
	}

	// append the device data to the file
	n, err = file.Write(data)
	if err != nil {
		glog.Errorln(op, "write device data to file", filepath, "error", err, "requuid", requuid)
		return err
	}
	if n != len(data) {
		glog.Errorln(op, "write device data to file", filepath, "error short write, requuid", requuid)
		return io.ErrShortWrite
	}

	err = file.Sync()
	if err != nil {
		glog.Errorln(op, "sync data to file", filepath, "error", err, "requuid", requuid)
		return err
	}

	glog.Infoln(op, "create file done", filepath, "requuid", requuid)
	return nil
}

// read the file, verify the checksum and return data
func readFile(filepath string, op string, requuid string) (data []byte, err error) {
	buf, err := ioutil.ReadFile(filepath)
	if err != nil {
		glog.Errorln(op, "read file", filepath, "error", err, "requuid", requuid)
		return nil, err
	}

	if len(buf) <= checksumBytes {
		glog.Errorln(op, "file data", len(buf), buf, "less than checksumBytes",
			checksumBytes, "file", filepath, "requuid", requuid)
		return nil, db.ErrDBInternal
	}

	ck1 := hex.EncodeToString(buf[0:checksumBytes])
	data = buf[checksumBytes:]
	sum := md5.Sum(data)
	ck2 := hex.EncodeToString(sum[:])
	if ck1 != ck2 {
		glog.Errorln(op, "data corrupted, stored checksum", ck1,
			"computed checksum", ck2, "file", filepath, "requuid", requuid)
		return nil, db.ErrDBInternal
	}

	return data, nil
}
