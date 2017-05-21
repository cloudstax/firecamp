package utils

import (
	"flag"
	"strconv"
	"testing"
	"time"
)

func TestFileUtil(t *testing.T) {
	flag.Parse()

	ti := time.Now().UnixNano()
	tmpdir := "/tmp/fileutil" + strconv.FormatInt(ti, 10)

	exist, err := IsDirExist(tmpdir)
	if err != nil {
		t.Fatalf("IsDirExist %s got error %s", tmpdir, err)
	}
	if exist {
		t.Fatalf("IsDirExist expect dir %s not exist", tmpdir)
	}

	err = CreateDirIfNotExist(tmpdir)
	if err != nil {
		t.Fatalf("create dir %s error %s", tmpdir, err)
	}

	exist, err = IsDirExist(tmpdir)
	if err != nil {
		t.Fatalf("IsDirExist %s got error %s", tmpdir, err)
	}
	if !exist {
		t.Fatalf("IsDirExist expect dir %s exist", tmpdir)
	}

	// create dir again, expect success
	err = CreateDirIfNotExist(tmpdir)
	if err != nil {
		t.Fatalf("create existing dir %s error %s", tmpdir, err)
	}
}
