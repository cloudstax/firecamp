package controldbserver

import (
	"os"
	"path"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/db/controldb"
	pb "github.com/cloudstax/firecamp/db/controldb/protocols"
	"github.com/cloudstax/firecamp/utils"
)

type serviceStaticIPSvc struct {
	// /topRoot/cluster/serviceipdir/
	rootDir string

	// the quitChan from the caller
	quitChan chan bool

	// chan for the requests
	reqChan chan serviceStaticIPReq
	// reqChan fans out the requests to these ReadWriters
	readWriters [parallelReqs]*serviceStaticIPReadWriter
}

func newServiceStaticIPSvc(rootDir string) *serviceStaticIPSvc {
	s := &serviceStaticIPSvc{
		rootDir:  rootDir,
		quitChan: make(chan bool),
		reqChan:  make(chan serviceStaticIPReq),
	}

	for i := 0; i < parallelReqs; i++ {
		s.readWriters[i] = newServiceStaticIPReadWriter(i, rootDir)
	}

	go s.serve()
	return s
}

func (s *serviceStaticIPSvc) CreateServiceStaticIP(ctx context.Context, serviceip *pb.ServiceStaticIP) error {
	subreq := serviceStaticIPCreateReq{
		serviceip: serviceip,
	}
	resp := s.handleReq(ctx, serviceip.StaticIP, subreq)
	return resp.err
}

func (s *serviceStaticIPSvc) UpdateServiceStaticIP(ctx context.Context, req *pb.UpdateServiceStaticIPRequest) error {
	subreq := serviceStaticIPUpdateReq{
		oldip: req.OldIP,
		newip: req.NewIP,
	}
	resp := s.handleReq(ctx, subreq.oldip.StaticIP, subreq)
	return resp.err
}

func (s *serviceStaticIPSvc) GetServiceStaticIP(ctx context.Context, key *pb.ServiceStaticIPKey) (*pb.ServiceStaticIP, error) {
	subreq := serviceStaticIPGetReq{
		key: key,
	}
	resp := s.handleReq(ctx, key.StaticIP, subreq)
	return resp.serviceip, resp.err
}

func (s *serviceStaticIPSvc) DeleteServiceStaticIP(ctx context.Context, key *pb.ServiceStaticIPKey) error {
	subreq := serviceStaticIPDeleteReq{
		key: key,
	}
	resp := s.handleReq(ctx, key.StaticIP, subreq)
	return resp.err
}

func (s *serviceStaticIPSvc) handleReq(ctx context.Context, staticIP string, subreq interface{}) serviceStaticIPResp {
	req := serviceStaticIPReq{
		ctx:      ctx,
		staticIP: staticIP,
		subreq:   subreq,
		resChan:  make(chan serviceStaticIPResp),
	}

	return s.sendAndWaitReq(req)
}

func (s *serviceStaticIPSvc) sendAndWaitReq(req serviceStaticIPReq) serviceStaticIPResp {
	requuid := utils.GetReqIDFromContext(req.ctx)

	// send req to chan
	s.reqChan <- req

	// wait for response
	select {
	case resp := <-req.resChan:
		return resp
	case <-req.ctx.Done():
		glog.Infoln("sendAndWaitReq request context done, requuid", requuid)
		return serviceStaticIPResp{err: db.ErrDBInternal}
	case <-time.After(requestTimeOutSecs * time.Second):
		glog.Errorln("sendAndWaitReq serviceStaticIPReq timeout, requuid", requuid)
		return serviceStaticIPResp{err: db.ErrDBInternal}
	}
}

func (s *serviceStaticIPSvc) Stop() {
	select {
	case s.quitChan <- true:
		glog.Infoln("stopped")
	case <-time.After(requestTimeOutSecs * time.Second):
		glog.Infoln("time out at stop")
	}
}

// the single entrance to serve
func (s *serviceStaticIPSvc) serve() {
	glog.Infoln("serviceStaticIPSvc start serving, rootDir", s.rootDir)
	for {
		select {
		case <-s.quitChan:
			glog.Infoln("receive quit")
			// fan out quit
			for i := 0; i < parallelReqs; i++ {
				s.readWriters[i].quit()
			}
			break

		case req := <-s.reqChan:
			// fan out request
			idx := utils.Hash(req.staticIP) % parallelReqs
			serviceipio := s.readWriters[idx]
			serviceipio.reqChan <- req
		}
	}
	glog.Infoln("serviceStaticIPSvc stop serving, rootDir", s.rootDir)
}

type serviceStaticIPResp struct {
	serviceip *pb.ServiceStaticIP
	err       error
}

type serviceStaticIPReq struct {
	ctx      context.Context
	staticIP string

	// detail request
	subreq interface{}

	// the chan to send the response
	resChan chan serviceStaticIPResp
}

type serviceStaticIPCreateReq struct {
	serviceip *pb.ServiceStaticIP
}

type serviceStaticIPUpdateReq struct {
	oldip *pb.ServiceStaticIP
	newip *pb.ServiceStaticIP
}

type serviceStaticIPGetReq struct {
	key *pb.ServiceStaticIPKey
}

type serviceStaticIPDeleteReq struct {
	key *pb.ServiceStaticIPKey
}

// serviceStaticIPReadWriter serves the static ip read & write operations.
// One static ip may not be small. For examle, the default cassandra yaml is 45KB.
// Also the static ip is usually not used after the serviceMember creates the static ip.
// So no static ip will be cached.
type serviceStaticIPReadWriter struct {
	index    int
	rootDir  string
	quitChan chan bool
	reqChan  chan serviceStaticIPReq
}

func newServiceStaticIPReadWriter(index int, rootDir string) *serviceStaticIPReadWriter {
	r := &serviceStaticIPReadWriter{
		index:    index,
		rootDir:  rootDir,
		quitChan: make(chan bool),
		reqChan:  make(chan serviceStaticIPReq),
	}

	go r.serve()
	return r
}

func (r *serviceStaticIPReadWriter) isTmpFile(filename string) bool {
	return strings.HasSuffix(filename, tmpFileSuffix)
}

func (r *serviceStaticIPReadWriter) sendResp(ctx context.Context, c chan<- serviceStaticIPResp, resp serviceStaticIPResp) {
	requuid := utils.GetReqIDFromContext(ctx)
	select {
	case c <- resp:
		glog.Infoln("sent resp done, requuid", requuid)
	case <-ctx.Done():
		glog.Infoln("sendResp request context done, requuid", requuid)
	case <-time.After(requestTimeOutSecs * time.Second):
		glog.Errorln("send serviceStaticIPResp timeout, requuid", requuid)
	}
}

func (r *serviceStaticIPReadWriter) serve() {
	glog.Infoln("serviceStaticIPReadWriter", r.index, "start serving, rootDir", r.rootDir)
	for {
		select {
		case <-r.quitChan:
			glog.Infoln("receive quit")
			break

		case req := <-r.reqChan:
			// handle the request
			resp := serviceStaticIPResp{}
			switch subreq := req.subreq.(type) {
			case serviceStaticIPCreateReq:
				resp.err = r.createServiceStaticIP(req.ctx, subreq.serviceip)
			case serviceStaticIPUpdateReq:
				resp.err = r.updateServiceStaticIP(req.ctx, subreq.oldip, subreq.newip)
			case serviceStaticIPGetReq:
				resp.serviceip, resp.err = r.getServiceStaticIP(req.ctx, subreq.key)
			case serviceStaticIPDeleteReq:
				resp.err = r.deleteServiceStaticIP(req.ctx, subreq.key)
			}

			r.sendResp(req.ctx, req.resChan, resp)
		}
	}
	glog.Infoln("serviceStaticIPReadWriter", r.index, "stop serving, rootDir", r.rootDir)
}

func (r *serviceStaticIPReadWriter) quit() {
	r.quitChan <- true
}

func (r *serviceStaticIPReadWriter) createServiceStaticIP(ctx context.Context, serviceip *pb.ServiceStaticIP) error {
	requuid := utils.GetReqIDFromContext(ctx)

	fpath := path.Join(r.rootDir, serviceip.StaticIP)
	exist, err := utils.IsFileExist(fpath)
	if err != nil {
		glog.Errorln("check static ip exist error", err, fpath, "requuid", requuid)
		return err
	}

	if exist {
		// serviceStaticIP exists, check whether the creating serviceStaticIP is the same with the current file
		currip, err := r.readServiceStaticIP(fpath, "createServiceStaticIP", requuid)
		if err != nil {
			glog.Errorln("createServiceStaticIP, read static ip error", err, fpath, "requuid", requuid)
			return err
		}

		// check whether the serviceip matches
		if !controldb.EqualServiceStaticIP(serviceip, currip) {
			glog.Errorln("static ip", serviceip, "diff with existing file", currip, fpath, "requuid", requuid)
			return db.ErrDBConditionalCheckFailed
		}

		// serviceip matches, would be a retry request, return success.
		glog.Infoln("static ip exists", fpath, "requuid", requuid)
		return nil
	}

	err = r.writeServiceStaticIP(serviceip, fpath, "createServiceStaticIP", requuid)
	if err != nil {
		glog.Errorln("writeServiceStaticIP error", err, fpath, "requuid", requuid)
		return err
	}

	glog.Infoln("created static ip", fpath, "requuid", requuid)
	return nil
}

func (r *serviceStaticIPReadWriter) updateServiceStaticIP(ctx context.Context, oldip *pb.ServiceStaticIP, newip *pb.ServiceStaticIP) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// sanity check. ServiceUUID, AvailableZone, etc, are not allowed to update.
	if oldip.StaticIP != newip.StaticIP ||
		oldip.ServiceUUID != newip.ServiceUUID ||
		oldip.AvailableZone != newip.AvailableZone {
		glog.Errorln("immutable attributes are updated, old ip", oldip, "new ip", newip, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	fpath := path.Join(r.rootDir, oldip.StaticIP)
	exist, err := utils.IsFileExist(fpath)
	if err != nil {
		glog.Errorln("check static ip exist error", err, fpath, "requuid", requuid)
		return err
	}
	if !exist {
		glog.Errorln("the static ip not exist", fpath, "requuid", requuid)
		return db.ErrDBRecordNotFound
	}

	// serviceStaticIP exists, check whether the creating serviceStaticIP is the same with the current file
	currip, err := r.readServiceStaticIP(fpath, "updateServiceStaticIP", requuid)
	if err != nil {
		glog.Errorln("updateServiceStaticIP, read static ip error", err, fpath, "requuid", requuid)
		return err
	}

	// check whether the serviceip matches
	if !controldb.EqualServiceStaticIP(oldip, currip) {
		glog.Errorln("old static ip", oldip, "diff with existing file", currip, fpath, "requuid", requuid)
		return db.ErrDBConditionalCheckFailed
	}

	err = r.writeServiceStaticIP(newip, fpath, "updateServiceStaticIP", requuid)
	if err != nil {
		glog.Errorln("writeServiceStaticIP error", err, fpath, "requuid", requuid)
		return err
	}

	glog.Infoln("updated static ip to", newip, "requuid", requuid)
	return nil
}

func (r *serviceStaticIPReadWriter) getServiceStaticIP(ctx context.Context, key *pb.ServiceStaticIPKey) (*pb.ServiceStaticIP, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	fpath := path.Join(r.rootDir, key.StaticIP)
	exist, err := utils.IsFileExist(fpath)
	if err != nil {
		glog.Errorln("check static ip exist error", err, fpath, "requuid", requuid)
		return nil, err
	}
	if !exist {
		glog.Errorln("static ip not exist", key, "requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	serviceip, err := r.readServiceStaticIP(fpath, "getServiceStaticIP", requuid)
	if err != nil {
		glog.Errorln("getServiceStaticIP, read static ip error", err, fpath, "requuid", requuid)
		return nil, err
	}

	glog.Infoln("get static ip, staticIP", serviceip, "requuid", requuid)
	return serviceip, nil
}

func (r *serviceStaticIPReadWriter) deleteServiceStaticIP(ctx context.Context, key *pb.ServiceStaticIPKey) error {
	requuid := utils.GetReqIDFromContext(ctx)

	fpath := path.Join(r.rootDir, key.StaticIP)
	exist, err := utils.IsFileExist(fpath)
	if err != nil {
		glog.Errorln("check static ip exist error", err, fpath, "requuid", requuid)
		return err
	}
	if !exist {
		glog.Errorln("static ip not exist", key, "requuid", requuid)
		return db.ErrDBRecordNotFound
	}

	err = os.Remove(fpath)
	if err != nil {
		glog.Errorln("deleteServiceStaticIP, read static ip error", err, fpath, "requuid", requuid)
		return err
	}

	glog.Infoln("deleted static ip", fpath, "requuid", requuid)
	return nil
}

func (r *serviceStaticIPReadWriter) writeServiceStaticIP(serviceip *pb.ServiceStaticIP, fpath string, op string, requuid string) error {
	data, err := proto.Marshal(serviceip)
	if err != nil {
		glog.Errorln("Marshal ServiceStaticIP error", err, fpath, "requuid", requuid)
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

	// rename tmpfile to the static ip
	err = os.Rename(tmpfilepath, fpath)
	if err != nil {
		glog.Errorln("rename tmpfile", tmpfilepath, "to", fpath, "error", err, "requuid", requuid)
		os.Remove(tmpfilepath)
		return db.ErrDBInternal
	}

	return nil
}

func (r *serviceStaticIPReadWriter) readServiceStaticIP(fpath string, op string, requuid string) (*pb.ServiceStaticIP, error) {
	data, err := readFile(fpath, op, requuid)
	if err != nil {
		glog.Errorln(op, "error", err, fpath, "requuid", requuid)
		return nil, err
	}

	serviceip := &pb.ServiceStaticIP{}
	err = proto.Unmarshal(data, serviceip)
	if err != nil {
		glog.Errorln("Unmarshal ServiceStaticIP error", err, fpath, "requuid", requuid)
		return nil, err
	}

	return serviceip, nil
}
