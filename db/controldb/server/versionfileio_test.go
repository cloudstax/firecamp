package controldbserver

import (
	"flag"
	"os"
	"path"
	"strconv"
	"testing"
	"time"

	"github.com/openconnectio/openmanage/utils"
)

func TestVersionFileIO(t *testing.T) {
	flag.Parse()

	ti := time.Now().UnixNano()
	rootdir := "/tmp/fileutil" + strconv.FormatInt(ti, 10)
	err := utils.CreateDirIfNotExist(rootdir)
	if err != nil {
		t.Fatalf("create dir %s error %s", rootdir, err)
	}

	// create dir again, expect success
	err = utils.CreateDirIfNotExist(rootdir)
	if err != nil {
		t.Fatalf("create existing dir %s error %s", rootdir, err)
	}

	filepath := path.Join(rootdir, "testfile")
	data := []byte("testdata aaaaa")
	err = createFile(filepath, data, "create", "requuid")
	if err != nil {
		t.Fatalf("create file %s error %s", filepath, err)
	}

	rdata, err := readFile(filepath, "read", "requuid")
	if err != nil {
		t.Fatalf("read file %s error %s", filepath, err)
	}

	str1 := string(data[:])
	str2 := string(rdata[:])
	if str1 != str2 {
		t.Fatalf("expect data %s, got %s", str1, str2)
	}

	// overwrite the file
	err = createFile(filepath, data, "create", "requuid")
	if err != nil {
		t.Fatalf("overwrite file %s error %s", filepath, err)
	}

	rdata, err = readFile(filepath, "read", "requuid")
	if err != nil {
		t.Fatalf("read overwritten file %s error %s", filepath, err)
	}

	str1 = string(data[:])
	str2 = string(rdata[:])
	if str1 != str2 {
		t.Fatalf("expect data %s, got %s", str1, str2)
	}

	os.RemoveAll(rootdir)
}
