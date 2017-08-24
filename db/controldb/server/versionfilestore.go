package controldbserver

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/utils"
)

// versionfilestore implements the file version feature. Every write will generate a new version.
// Read will always read the latest version. The older versions will be cleaned up in the background.
// The implementation is not thread-safe. The caller MUST serialize the accesses, to ensure the
// correct version sequence.
type versionfilestore struct {
	fileType int

	rootDir           string
	versionFilePrefix string
	tmpFilePrefix     string

	// the first and current device file version
	firstVersion   int64
	currentVersion int64
	// whether the cleanup job is running
	cleanupRunning bool
}

func newVersionFileStore(fileType int, rootDir string) (*versionfilestore, error) {
	f := &versionfilestore{
		fileType:       fileType,
		rootDir:        rootDir,
		firstVersion:   -1,
		currentVersion: -1,
		cleanupRunning: false,
	}

	switch fileType {
	case fileTypeDevice:
		f.versionFilePrefix = deviceVersionFilePrefix
		f.tmpFilePrefix = deviceTmpFilePrefix
	case fileTypeService:
		f.versionFilePrefix = serviceVersionFilePrefix
		f.tmpFilePrefix = serviceTmpFilePrefix
	case fileTypeServiceAttr:
		f.versionFilePrefix = serviceAttrVersionFilePrefix
		f.tmpFilePrefix = serviceAttrTmpFilePrefix
	default:
		glog.Errorln("versionfilestore doesn't support file type", fileType)
		return nil, db.ErrDBInternal
	}

	// check and create the rootDir
	err := utils.CreateDirIfNotExist(rootDir)
	if err != nil {
		glog.Errorln("create rootDir error", err, rootDir)
		return nil, db.ErrDBInternal
	}

	err = f.getFirstAndLastVersions()
	if err != nil {
		glog.Errorln("getFirstAndLastVersions error", err, rootDir)
		return nil, db.ErrDBInternal
	}

	return f, nil
}

func (f *versionfilestore) ReadCurrentVersion(op string, requuid string) (data []byte, err error) {
	filepath := f.constructVersionFilePath(f.currentVersion)
	return readFile(filepath, op, requuid)
}

func (f *versionfilestore) HasCurrentVersion() bool {
	return f.currentVersion != -1
}

func (f *versionfilestore) genTempFilePath() string {
	// simply use current time. Even if conflict happens, it is ok.
	t := time.Now().UnixNano()
	return path.Join(f.rootDir, f.tmpFilePrefix+strconv.FormatInt(t, 10))
}

func (f *versionfilestore) extractVersion(filename string) (int64, error) {
	if strings.HasPrefix(filename, f.versionFilePrefix) {
		vs := strings.TrimPrefix(filename, f.versionFilePrefix)
		return strconv.ParseInt(vs, 16, 64)
	}
	return -1, errors.New("not a version file")
}

func (f *versionfilestore) isVersionFile(filename string) bool {
	return strings.HasPrefix(filename, f.versionFilePrefix)
}

func (f *versionfilestore) constructVersionFilePath(version int64) string {
	return path.Join(f.rootDir, f.versionFilePrefix+fmt.Sprintf("%016x", version))
}

func (f *versionfilestore) getFirstAndLastVersions() error {
	// list all version files with prefix
	dir, err := os.Open(f.rootDir)
	if err != nil {
		glog.Errorln("open rootDir", f.rootDir, "error", err)
		return err
	}
	defer dir.Close()

	files, err := dir.Readdirnames(-1)
	if err != nil {
		glog.Errorln("Readdirnames error", err, "rootDir", f.rootDir)
		return err
	}

	for _, file := range files {
		if f.isVersionFile(file) {
			version, err := f.extractVersion(file)
			if err != nil {
				glog.Errorln("extractVersion error", err, "file", file, "rootDir", f.rootDir)
				return err
			}

			if f.firstVersion == -1 || f.firstVersion > version {
				f.firstVersion = version
			}
			if f.currentVersion == -1 || f.currentVersion < version {
				f.currentVersion = version
			}
		}
	}

	glog.Infoln("get first version", f.firstVersion, "last version", f.currentVersion, "rootDir", f.rootDir)

	// clean up the old versions
	if f.currentVersion > f.firstVersion+maxVersions && !f.cleanupRunning {
		f.cleanupRunning = true
		go f.cleanupOldVersions(f.firstVersion, f.currentVersion, maxVersions)
	}

	return nil
}

func (f *versionfilestore) renameTmpFileToNewVersion(tmpfilepath string, requuid string) (newVersion int64, err error) {
	newVersion = f.currentVersion + 1
	filepath := f.constructVersionFilePath(newVersion)

	// sanity check to make sure the new version file not exists
	_, err = os.Stat(filepath)
	if err == nil {
		// file exists, this should not happen
		glog.Errorln("new version file", filepath, "exists, tmpfile", tmpfilepath, "requuid", requuid)
		return -1, db.ErrDBInternal
	}

	// err != nil, check if not exist
	if !os.IsNotExist(err) {
		glog.Errorln("stat new version file", filepath, "error", err,
			"tmpfile", tmpfilepath, "requuid", requuid)
		return -1, err
	}

	// file not exist, rename tmpfile to the new version file
	err = os.Rename(tmpfilepath, filepath)
	if err != nil {
		glog.Errorln("rename tmpfile", tmpfilepath, "to", filepath, "error", err, "requuid", requuid)
		return -1, err
	}

	glog.Infoln("created new version file", filepath, "from tmpfile", tmpfilepath, "requuid", requuid)
	return newVersion, nil
}

func (f *versionfilestore) CreateNewVersionFile(data []byte, op string, requuid string) error {
	// generate the temp file name
	tmpfilepath := f.genTempFilePath()

	// write data and checksum to file
	err := createFile(tmpfilepath, data, op, requuid)
	if err != nil {
		glog.Errorln(op, "create temp file", tmpfilepath, "error", err, "requuid", requuid)
		os.Remove(tmpfilepath)
		return err
	}

	// create a new file version
	newVersion, err := f.renameTmpFileToNewVersion(tmpfilepath, requuid)
	if err != nil {
		glog.Errorln(op, "create new version from tmpfile", tmpfilepath, "error", err, "requuid", requuid)
		os.Remove(tmpfilepath)
		return err
	}

	f.currentVersion = newVersion
	if f.firstVersion == -1 {
		// the very first version, newVersion must be 0
		if newVersion != 0 {
			glog.Errorln("firstVersion is -1, but newVersion is not 0", f.rootDir)
		}
		f.firstVersion = 0
	}

	// clean up the old versions
	if f.currentVersion > f.firstVersion+maxVersions && !f.cleanupRunning {
		f.cleanupRunning = true
		go f.cleanupOldVersions(f.firstVersion, f.currentVersion, maxVersions)
	}

	glog.Infoln(op, "create new version file", newVersion, "requuid", requuid)
	return nil
}

func (f *versionfilestore) resetCleanupRunningFlag() {
	f.cleanupRunning = false
}

// delete the older version files and return the new firstVersion
func (f *versionfilestore) cleanupOldVersions(firstVersion int64, lastVersion int64, maxVersions int64) {
	defer f.resetCleanupRunningFlag()

	if lastVersion < (firstVersion + maxVersions) {
		// nothing to do
		return
	}

	for i := firstVersion; i < lastVersion-maxVersions; i++ {
		filepath := f.constructVersionFilePath(i)

		err := os.Remove(filepath)
		if err != nil {
			if os.IsNotExist(err) {
				glog.Infoln(filepath, "not exist, remove the next file")
				f.firstVersion = i + 1
				continue
			}
			glog.Errorln("remove", filepath, "error", err)
			return
		}

		glog.Infoln("removed", filepath)
		f.firstVersion = i + 1
	}
}

func (f *versionfilestore) DeleteAll() error {
	return os.RemoveAll(f.rootDir)
}
