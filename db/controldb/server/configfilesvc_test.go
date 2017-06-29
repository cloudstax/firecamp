package controldbserver

import (
	"flag"
	"os"
	"strconv"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/db"
	"github.com/cloudstax/openmanage/db/controldb"
	pb "github.com/cloudstax/openmanage/db/controldb/protocols"
	"github.com/cloudstax/openmanage/utils"
)

func TestConfigFileReadWriter(t *testing.T) {
	flag.Parse()
	ctx := context.Background()
	ti := time.Now().UnixNano()
	rootdir := "/tmp/fileutil" + strconv.FormatInt(ti, 10)
	serviceUUID := "serviceuuid-1"
	fileName := "cfgfile.name"

	fileIDPrefix := "fileid-"
	fileContentPrefix := "filecontent-"
	skipContent := false

	s := newConfigFileReadWriter(0, rootdir)

	maxNum := 17
	for i := 0; i < maxNum; i++ {
		str := strconv.Itoa(i)
		fileID := fileIDPrefix + str
		fileContent := fileContentPrefix + str
		fileMD5 := utils.GenMD5(fileContent)
		mtime := time.Now().Unix()

		cfg := &pb.ConfigFile{
			ServiceUUID:  serviceUUID,
			FileID:       fileID,
			FileMD5:      fileMD5,
			FileName:     fileName,
			LastModified: mtime,
			Content:      fileContent,
		}

		err := s.createConfigFile(ctx, cfg)
		if err != nil {
			t.Fatalf("createConfigFile error %s rootdir %s", err, rootdir)
		}

		// test the request retry
		err = s.createConfigFile(ctx, cfg)
		if err != nil {
			t.Fatalf("createConfigFile error %s rootdir %s", err, rootdir)
		}

		// create the existing config file with different mtime
		cfg1 := controldb.CopyConfigFile(cfg)
		cfg1.LastModified = time.Now().UnixNano()
		err = s.createConfigFile(ctx, cfg1)
		if err != nil {
			t.Fatalf("create the existing config file with different mtime, expect success got %s, rootdir %s", err, rootdir)
		}

		// negative case: create the existing config file with different fields
		cfg1.FileName = "newname"
		err = s.createConfigFile(ctx, cfg1)
		if err != db.ErrDBConditionalCheckFailed {
			t.Fatalf("create the existing config file, expect error db.ErrDBConditionalCheckFailed, got %s, rootdir %s", err, rootdir)
		}

		// negative: get non-exist cfg
		key := &pb.ConfigFileKey{
			ServiceUUID: serviceUUID,
			FileID:      fileID + "xxx",
		}
		cfg1, err = s.getConfigFile(ctx, key)
		if err != db.ErrDBRecordNotFound {
			t.Fatalf("get non-exist config file, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
		}
		// get cfg
		key = &pb.ConfigFileKey{
			ServiceUUID: serviceUUID,
			FileID:      fileID,
		}
		cfg1, err = s.getConfigFile(ctx, key)
		if err != nil {
			t.Fatalf("getConfigFile error %s key %s rootdir %s", err, key, rootdir)
		}
		if !controldb.EqualConfigFile(cfg, cfg1, false, skipContent) {
			t.Fatalf("getConfigFile returns %s, expect %s rootdir %s", cfg1, cfg, rootdir)
		}
	}

	// delete 6 config files
	idx := 0
	for i := 0; i < 6; i++ {
		idx += 2
		str := strconv.Itoa(idx)
		fileID := fileIDPrefix + str

		// negative case: delete non-exist config file
		key := &pb.ConfigFileKey{
			ServiceUUID: serviceUUID,
			FileID:      fileID + "xxx",
		}
		err := s.deleteConfigFile(ctx, key)
		if err != db.ErrDBRecordNotFound {
			t.Fatalf("delete non-exist config file, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
		}
		// delete config file
		key = &pb.ConfigFileKey{
			ServiceUUID: serviceUUID,
			FileID:      fileID,
		}
		err = s.deleteConfigFile(ctx, key)
		if err != nil {
			t.Fatalf("deleteConfigFile error %s rootdir %s", err, rootdir)
		}
	}

	// cleanup
	os.Remove(rootdir)
}

func testConfigFileOp(t *testing.T, s *configFileSvc, serviceUUID string, fileID string, str string, rootdir string) {
	fileName := "cfgfile.name"
	updateSuffix := "-update"
	fileContentPrefix := "filecontent-"
	skipContent := false

	ctx := context.Background()

	fileContent := fileContentPrefix + str
	mtime := time.Now().Unix()

	cfg := &pb.ConfigFile{
		ServiceUUID:  serviceUUID,
		FileID:       fileID,
		FileMD5:      utils.GenMD5(fileContent),
		FileName:     fileName,
		LastModified: mtime,
		Content:      fileContent,
	}

	err := s.CreateConfigFile(ctx, cfg)
	if err != nil {
		t.Fatalf("createConfigFile error %s rootdir %s", err, rootdir)
	}

	// test the request retry
	err = s.CreateConfigFile(ctx, cfg)
	if err != nil {
		t.Fatalf("createConfigFile error %s rootdir %s", err, rootdir)
	}

	// negative case: create the existing config file with different fields
	cfg1 := controldb.CopyConfigFile(cfg)
	cfg1.FileName = fileName + updateSuffix
	err = s.CreateConfigFile(ctx, cfg1)
	if err != db.ErrDBConditionalCheckFailed {
		t.Fatalf("create the existing config file, expect error db.ErrDBConditionalCheckFailed, got %s, rootdir %s", err, rootdir)
	}

	// negative: get non-exist cfg
	key := &pb.ConfigFileKey{
		ServiceUUID: serviceUUID,
		FileID:      fileID + "xxx",
	}
	_, err = s.GetConfigFile(ctx, key)
	if err != db.ErrDBRecordNotFound {
		t.Fatalf("get non-exist config file, expect db.ErrDBRecordNotFound, got %s, rootdir %s", err, rootdir)
	}

	// get cfg
	key = &pb.ConfigFileKey{
		ServiceUUID: serviceUUID,
		FileID:      fileID,
	}
	cfg1, err = s.GetConfigFile(ctx, key)
	if err != nil {
		t.Fatalf("getConfigFile error %s key %s rootdir %s", err, key, rootdir)
	}
	if !controldb.EqualConfigFile(cfg, cfg1, false, skipContent) {
		t.Fatalf("getConfigFile returns %s, expect %s rootdir %s", cfg1, cfg, rootdir)
	}

	// update cfg
	cfg2 := controldb.CopyConfigFile(cfg)
	cfg2.Content = cfg.Content + "newcontent"
	cfg2.FileMD5 = utils.GenMD5(cfg2.Content)
	cfg2.LastModified = time.Now().UnixNano()
	updateReq := &pb.UpdateConfigFileRequest{
		OldCfg: cfg,
		NewCfg: cfg2,
	}
	err = s.UpdateConfigFile(ctx, updateReq)
	if err != nil {
		t.Fatalf("UpdateConfigFile error %s, %s %s", err, cfg, cfg2)
	}

	// get cfg again
	cfg1, err = s.GetConfigFile(ctx, key)
	if err != nil {
		t.Fatalf("getConfigFile error %s key %s rootdir %s", err, key, rootdir)
	}
	if !controldb.EqualConfigFile(cfg2, cfg1, false, skipContent) {
		t.Fatalf("getConfigFile returns %s, expect %s rootdir %s", cfg1, cfg2, rootdir)
	}
}

func TestConfigFileSvc(t *testing.T) {
	flag.Parse()
	time.Sleep(2 * time.Second)
	ti := time.Now().UnixNano()
	rootdir := "/tmp/fileutil" + strconv.FormatInt(ti, 10)
	err := utils.CreateDirIfNotExist(rootdir)
	if err != nil {
		t.Fatalf("create root dir error", err, rootdir)
	}

	s := newConfigFileSvc(rootdir)

	ctx := context.Background()

	serviceUUIDPrefix := "serviceuuid-"
	fileIDPrefix := "cfgid-"

	serviceNum := 5

	for i := 0; i < serviceNum; i++ {
		str := strconv.Itoa(i)
		serviceUUID := serviceUUIDPrefix + str
		fileID := fileIDPrefix + str
		testConfigFileOp(t, s, serviceUUID, fileID, str, rootdir)
	}

	// access the first service cfg again
	serviceUUID := serviceUUIDPrefix + strconv.Itoa(0)
	key := &pb.ConfigFileKey{
		ServiceUUID: serviceUUID,
		FileID:      fileIDPrefix + strconv.Itoa(0),
	}
	_, err = s.GetConfigFile(ctx, key)
	if err != nil {
		t.Fatalf("getConfigFile error %s rootdir %s, %s", err, rootdir, key)
	}

	// delete the 1st service cfg
	err = s.DeleteConfigFile(ctx, key)
	if err != nil {
		t.Fatalf("deleteConfigFile error %s rootdir %s, %s", err, rootdir, key)
	}

	// delete the 2nd service cfg
	serviceUUID = serviceUUIDPrefix + strconv.Itoa(1)
	key = &pb.ConfigFileKey{
		ServiceUUID: serviceUUID,
		FileID:      fileIDPrefix + strconv.Itoa(1),
	}
	err = s.DeleteConfigFile(ctx, key)
	if err != nil {
		t.Fatalf("deleteConfigFile error %s rootdir %s, %s", err, rootdir, key)
	}
	// getConfigFile should return error
	_, err = s.GetConfigFile(ctx, key)
	if err != db.ErrDBRecordNotFound {
		t.Fatalf("get not exist cfg %s, expect error db.ErrDBRecordNotFound, got %s", key, err)
	}

	// cleanup
	os.RemoveAll(rootdir)
}
