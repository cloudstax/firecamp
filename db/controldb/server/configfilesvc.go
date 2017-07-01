package controldbserver

import (
	"os"
	"path"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/db"
	"github.com/cloudstax/openmanage/db/controldb"
	pb "github.com/cloudstax/openmanage/db/controldb/protocols"
	"github.com/cloudstax/openmanage/utils"
)

const (
	parallelReqs = 4
)

type configFileSvc struct {
	// /topRoot/cluster/servicedir/
	rootDir string

	// the quitChan from the caller
	quitChan chan bool

	// chan for the requests
	reqChan chan configFileReq
	// reqChan fans out the requests to these ReadWriters
	cfgReadWriters [parallelReqs]*configFileReadWriter
}

func newConfigFileSvc(rootDir string) *configFileSvc {
	s := &configFileSvc{
		rootDir:  rootDir,
		quitChan: make(chan bool),
		reqChan:  make(chan configFileReq),
	}

	for i := 0; i < parallelReqs; i++ {
		s.cfgReadWriters[i] = newConfigFileReadWriter(i, rootDir)
	}

	go s.serve()
	return s
}

func (s *configFileSvc) CreateConfigFile(ctx context.Context, cfg *pb.ConfigFile) error {
	subreq := configFileCreateReq{
		cfg: cfg,
	}
	resp := s.handleReq(ctx, cfg.ServiceUUID, subreq)
	return resp.err
}

func (s *configFileSvc) GetConfigFile(ctx context.Context, key *pb.ConfigFileKey) (*pb.ConfigFile, error) {
	subreq := configFileGetReq{
		key: key,
	}
	resp := s.handleReq(ctx, key.ServiceUUID, subreq)
	return resp.cfg, resp.err
}

func (s *configFileSvc) DeleteConfigFile(ctx context.Context, key *pb.ConfigFileKey) error {
	subreq := configFileDeleteReq{
		key: key,
	}
	resp := s.handleReq(ctx, key.ServiceUUID, subreq)
	return resp.err
}

func (s *configFileSvc) handleReq(ctx context.Context, serviceUUID string, subreq interface{}) configFileResp {
	req := configFileReq{
		ctx:         ctx,
		serviceUUID: serviceUUID,
		subreq:      subreq,
		resChan:     make(chan configFileResp),
	}

	return s.sendAndWaitReq(req)
}

func (s *configFileSvc) sendAndWaitReq(req configFileReq) configFileResp {
	requuid := utils.GetReqIDFromContext(req.ctx)

	// send req to chan
	s.reqChan <- req

	// wait for response
	select {
	case resp := <-req.resChan:
		return resp
	case <-req.ctx.Done():
		glog.Infoln("sendAndWaitReq request context done, requuid", requuid)
		return configFileResp{err: db.ErrDBInternal}
	case <-time.After(requestTimeOutSecs * time.Second):
		glog.Errorln("sendAndWaitReq configFileReq timeout, requuid", requuid)
		return configFileResp{err: db.ErrDBInternal}
	}
}

func (s *configFileSvc) Stop() {
	select {
	case s.quitChan <- true:
		glog.Infoln("stopped")
	case <-time.After(requestTimeOutSecs * time.Second):
		glog.Infoln("time out at stop")
	}
}

// the single entrance to serve
func (s *configFileSvc) serve() {
	glog.Infoln("configFileSvc start serving, rootDir", s.rootDir)
	for {
		select {
		case <-s.quitChan:
			glog.Infoln("receive quit")
			// fan out quit
			for i := 0; i < parallelReqs; i++ {
				s.cfgReadWriters[i].quit()
			}
			break

		case req := <-s.reqChan:
			// fan out request
			idx := utils.Hash(req.serviceUUID) % parallelReqs
			cfgio := s.cfgReadWriters[idx]
			cfgio.reqChan <- req
		}
	}
	glog.Infoln("configFileSvc stop serving, rootDir", s.rootDir)
}

type configFileResp struct {
	// one configFile, response for the Create/Get/Update/Delete request
	cfg *pb.ConfigFile
	err error
}

type configFileReq struct {
	ctx         context.Context
	serviceUUID string

	// detail request
	subreq interface{}

	// the chan to send the response
	resChan chan configFileResp
}

type configFileCreateReq struct {
	cfg *pb.ConfigFile
}

type configFileGetReq struct {
	key *pb.ConfigFileKey
}

type configFileDeleteReq struct {
	key *pb.ConfigFileKey
}

// configFileReadWriter serves the config file read & write operations.
// One config file may not be small. For examle, the default cassandra yaml is 45KB.
// Also the config file is usually not used after the serviceMember creates the config file.
// So no config file will be cached.
type configFileReadWriter struct {
	index    int
	rootDir  string
	quitChan chan bool
	reqChan  chan configFileReq
}

func newConfigFileReadWriter(index int, rootDir string) *configFileReadWriter {
	r := &configFileReadWriter{
		index:    index,
		rootDir:  rootDir,
		quitChan: make(chan bool),
		reqChan:  make(chan configFileReq),
	}

	go r.serve()
	return r
}

func (r *configFileReadWriter) isTmpFile(filename string) bool {
	return strings.HasSuffix(filename, tmpFileSuffix)
}

func (r *configFileReadWriter) sendResp(ctx context.Context, c chan<- configFileResp, resp configFileResp) {
	requuid := utils.GetReqIDFromContext(ctx)
	select {
	case c <- resp:
		glog.Infoln("sent resp done, requuid", requuid)
	case <-ctx.Done():
		glog.Infoln("sendResp request context done, requuid", requuid)
	case <-time.After(requestTimeOutSecs * time.Second):
		glog.Errorln("send configFileResp timeout, requuid", requuid)
	}
}

func (r *configFileReadWriter) serve() {
	glog.Infoln("configFileReadWriter", r.index, "start serving, rootDir", r.rootDir)
	for {
		select {
		case <-r.quitChan:
			glog.Infoln("receive quit")
			break

		case req := <-r.reqChan:
			// handle the request
			resp := configFileResp{}
			switch subreq := req.subreq.(type) {
			case configFileCreateReq:
				resp.err = r.createConfigFile(req.ctx, subreq.cfg)
			case configFileGetReq:
				resp.cfg, resp.err = r.getConfigFile(req.ctx, subreq.key)
			case configFileDeleteReq:
				resp.err = r.deleteConfigFile(req.ctx, subreq.key)
			}

			r.sendResp(req.ctx, req.resChan, resp)
		}
	}
	glog.Infoln("configFileReadWriter", r.index, "stop serving, rootDir", r.rootDir)
}

func (r *configFileReadWriter) quit() {
	r.quitChan <- true
}

func (r *configFileReadWriter) createConfigFile(ctx context.Context, cfg *pb.ConfigFile) error {
	requuid := utils.GetReqIDFromContext(ctx)

	cmd5 := utils.GenMD5(cfg.Content)
	if cmd5 != cfg.FileMD5 {
		glog.Errorln("InvalidRequest - createConfigFile content md5 mismatch",
			cfg.FileMD5, cmd5, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	cfgDir := path.Join(r.rootDir, cfg.ServiceUUID, configDirName)
	err := utils.CreateDirIfNotExist(cfgDir)
	if err != nil {
		glog.Errorln("create config dir error", err, cfgDir)
		return err
	}

	fpath := path.Join(cfgDir, cfg.FileID)
	exist, err := utils.IsFileExist(fpath)
	if err != nil {
		glog.Errorln("check config file exist error", err, fpath, "requuid", requuid)
		return err
	}

	if exist {
		// configFile exists, check whether the creating configFile is the same with the current file
		currCfg, err := r.readConfigFile(fpath, "createConfigFile", requuid)
		if err != nil {
			glog.Errorln("createConfigFile, read config file error", err, fpath, "requuid", requuid)
			return err
		}

		// check whether the checksum matches
		skipMtime := true
		skipContent := true
		if !controldb.EqualConfigFile(cfg, currCfg, skipMtime, skipContent) {
			glog.Errorln("config file", controldb.PrintConfigFile(cfg), "diff with existing file",
				controldb.PrintConfigFile(currCfg), fpath, "requuid", requuid)
			return db.ErrDBConditionalCheckFailed
		}

		// checksum matches, would be a retry request, return success.
		glog.Infoln("config file exists", fpath, "requuid", requuid)
		return nil
	}

	err = r.writeConfigFile(cfg, fpath, "createConfigFile", requuid)
	if err != nil {
		glog.Errorln("writeConfigFile error", err, fpath, "requuid", requuid)
		return err
	}

	glog.Infoln("created config file", fpath, "requuid", requuid)
	return nil
}

func (r *configFileReadWriter) getConfigFile(ctx context.Context, key *pb.ConfigFileKey) (*pb.ConfigFile, error) {
	requuid := utils.GetReqIDFromContext(ctx)
	fpath := path.Join(r.rootDir, key.ServiceUUID, configDirName, key.FileID)

	exist, err := utils.IsFileExist(fpath)
	if err != nil {
		glog.Errorln("check config file exist error", err, fpath, "requuid", requuid)
		return nil, err
	}
	if !exist {
		glog.Errorln("config file not exist", key, "requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	cfg, err := r.readConfigFile(fpath, "getConfigFile", requuid)
	if err != nil {
		glog.Errorln("getConfigFile, read config file error", err, fpath, "requuid", requuid)
		return nil, err
	}

	glog.Infoln("get config file, serviceUUID", cfg.ServiceUUID, "fileID", cfg.FileID, "fileMD5",
		cfg.FileMD5, "fileName", cfg.FileName, "LastModified", cfg.LastModified, "requuid", requuid)
	return cfg, nil
}

func (r *configFileReadWriter) deleteConfigFile(ctx context.Context, key *pb.ConfigFileKey) error {
	requuid := utils.GetReqIDFromContext(ctx)
	fpath := path.Join(r.rootDir, key.ServiceUUID, configDirName, key.FileID)

	exist, err := utils.IsFileExist(fpath)
	if err != nil {
		glog.Errorln("check config file exist error", err, fpath, "requuid", requuid)
		return err
	}
	if !exist {
		glog.Errorln("config file not exist", key, "requuid", requuid)
		return db.ErrDBRecordNotFound
	}

	err = os.Remove(fpath)
	if err != nil {
		glog.Errorln("deleteConfigFile, read config file error", err, fpath, "requuid", requuid)
		return err
	}

	glog.Infoln("deleted config file", fpath, "requuid", requuid)
	return nil
}

func (r *configFileReadWriter) writeConfigFile(cfg *pb.ConfigFile, fpath string, op string, requuid string) error {
	data, err := proto.Marshal(cfg)
	if err != nil {
		glog.Errorln("Marshal ConfigFile error", err, fpath, "requuid", requuid)
		return err
	}

	// write data and checksum to the tmp file
	tmpfilepath := fpath + tmpFileSuffix
	err = createFile(tmpfilepath, data, op, requuid)
	if err != nil {
		glog.Errorln("create tmp file", tmpfilepath, "error", err, "requuid", requuid)
		os.Remove(tmpfilepath)
		return db.ErrDBInternal
	}

	// rename tmpfile to the config file
	err = os.Rename(tmpfilepath, fpath)
	if err != nil {
		glog.Errorln("rename tmpfile", tmpfilepath, "to", fpath, "error", err, "requuid", requuid)
		os.Remove(tmpfilepath)
		return db.ErrDBInternal
	}

	return nil
}

func (r *configFileReadWriter) readConfigFile(fpath string, op string, requuid string) (*pb.ConfigFile, error) {
	data, err := readFile(fpath, op, requuid)
	if err != nil {
		glog.Errorln(op, "error", err, fpath, "requuid", requuid)
		return nil, err
	}

	cfg := &pb.ConfigFile{}
	err = proto.Unmarshal(data, cfg)
	if err != nil {
		glog.Errorln("Unmarshal ConfigFile error", err, fpath, "requuid", requuid)
		return nil, err
	}

	return cfg, nil
}
