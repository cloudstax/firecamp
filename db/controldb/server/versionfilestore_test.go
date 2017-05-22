package controldbserver

import (
	"flag"
	"strconv"
	"testing"
	"time"
)

func createNewVersion(t *testing.T, vf *versionfilestore, dataPrefix string, i int, checkFirstVersion bool) {
	requuid := "requuid"
	str := dataPrefix + strconv.Itoa(i)
	data := []byte(str)
	err := vf.CreateNewVersionFile(data, "create", requuid)
	if err != nil {
		t.Fatalf("create new version error %s num %d", err, i)
	}

	rdata, err := vf.ReadCurrentVersion("read", requuid)
	if err != nil {
		t.Fatalf("ReadCurrentVersion error %s num %d", err, i)
	}
	rstr := string(rdata)
	if str != rstr {
		t.Fatalf("data mismatch, expect %s, got %s", str, rstr)
	}

	// the first version is expected to be 0, only when currentVersion < maxVersions.
	// after that, cleanup job will run in the background, no fixed first version.
	if checkFirstVersion && vf.firstVersion != 0 {
		t.Fatalf("expect firstVersion %d, got %d", 0, vf.firstVersion)
	}

	if vf.currentVersion != int64(i) {
		t.Fatalf("expect currentVersion %d, got %d", i, vf.currentVersion)
	}
}

func TestDeviceVersionFile(t *testing.T) {
	flag.Parse()

	ti := time.Now().UnixNano()
	rootdir := "/tmp/fileutil" + strconv.FormatInt(ti, 10)
	requuid := "requuid"
	var rdata []byte
	var str string
	var rstr string

	vf, err := newVersionFileStore(fileTypeDevice, rootdir)
	if err != nil {
		t.Fatalf("newVersionFileStore error %s rootdir %s", err, rootdir)
	}

	dataPrefix := "testdata"
	fileNum := maxVersions - 1
	for i := 0; i < fileNum; i++ {
		createNewVersion(t, vf, dataPrefix, i, true)
	}

	// test newVersionFileStore with files exist
	vf2, err := newVersionFileStore(fileTypeDevice, rootdir)
	if err != nil {
		t.Fatalf("newVersionFileStore error %s rootdir %s", err, rootdir)
	}

	if !vf2.HasCurrentVersion() {
		t.Fatalf("expect HasCurrentVersion, %d %d", vf2.firstVersion, vf2.currentVersion)
	}
	if vf2.firstVersion != 0 {
		t.Fatalf("expect firstVersion %d, got %d", 0, vf2.firstVersion)
	}
	if vf2.currentVersion != int64(fileNum-1) {
		t.Fatalf("expect currentVersion %d, got %d", fileNum-1, vf2.currentVersion)
	}
	str = dataPrefix + strconv.Itoa(fileNum-1)
	rdata, err = vf2.ReadCurrentVersion("read", requuid)
	if err != nil {
		t.Fatalf("ReadCurrentVersion error %s num %d", err, fileNum-1)
	}
	rstr = string(rdata)
	if str != rstr {
		t.Fatalf("data mismatch, expect %s, got %s", str, rstr)
	}

	fileNum2 := fileNum + maxVersions + 3
	for i := fileNum; i < fileNum2; i++ {
		createNewVersion(t, vf2, dataPrefix, i, false)
	}

	// wait till the background cleanup job is done
	i := 0
	for vf2.cleanupRunning {
		time.Sleep(1 * time.Second)
		i++
		if i > 2 {
			t.Fatalf("cleanup job is still running, %d %d", vf2.firstVersion, vf2.currentVersion)
		}
	}

	// create a new version again to run the last cleanup
	createNewVersion(t, vf2, dataPrefix, fileNum2, false)

	// wait till the background cleanup job is done
	i = 0
	for vf2.cleanupRunning {
		time.Sleep(1 * time.Second)
		i++
		if i > 2 {
			t.Fatalf("cleanup job is still running, %d %d", vf2.firstVersion, vf2.currentVersion)
		}
	}

	// verify the first and current version
	if vf2.currentVersion != int64(fileNum2) {
		t.Fatalf("expect currentVersion %d, got %d", fileNum2, vf2.currentVersion)
	}
	if vf2.firstVersion != int64(fileNum2-maxVersions) {
		t.Fatalf("expect firstVersion %d, got %d", 0, vf2.firstVersion)
	}

	vf2.DeleteAll()
}
