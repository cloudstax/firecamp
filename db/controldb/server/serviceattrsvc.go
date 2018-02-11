package controldbserver

import (
	"os"
	"path"
	"time"

	"github.com/golang/glog"
	"github.com/golang/groupcache/lru"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/db"
	pb "github.com/cloudstax/firecamp/db/controldb/protocols"
	"github.com/cloudstax/firecamp/utils"
)

// serviceAttrSvc has a single goroutine to serve all coming requests.
// Could have multiple instances and hash service by serviceUUID to the instance,
// so the requests to different services could be served in parallel.
// But the requests to one service will still be serialized. This would not be
// the botteneck, as the concurrent reads would hit cache. The attr will be sent
// back to the caller, which could send the response back in parallel.
type serviceAttrSvc struct {
	rootDir string
	// the quitChan from the caller
	quitChan chan bool

	// ServiceAttr lru cache for the services, key: serviceUUID, value: attrCacheValue
	lruCache *lru.Cache

	// chan for the requests
	reqChan chan serviceAttrReq
}

type attrCacheValue struct {
	attrio *attrReadWriter
}

func newServiceAttrSvc(rootDir string) *serviceAttrSvc {
	return newServiceAttrSvcWithMaxCacheSize(rootDir, maxServiceAttrCacheEntry)
}

func newServiceAttrSvcWithMaxCacheSize(rootDir string, maxCacheEntry int) *serviceAttrSvc {
	s := &serviceAttrSvc{
		rootDir:  rootDir,
		lruCache: lru.New(maxCacheEntry),
		quitChan: make(chan bool),
		reqChan:  make(chan serviceAttrReq),
	}

	go s.serve()
	return s
}

func (s *serviceAttrSvc) CreateServiceAttr(ctx context.Context, attr *pb.ServiceAttr) error {
	subreq := serviceAttrCreateReq{
		attr: attr,
	}
	resp := s.handleReq(ctx, attr.ServiceUUID, subreq)
	return resp.err
}

func (s *serviceAttrSvc) GetServiceAttr(ctx context.Context, key *pb.ServiceAttrKey) (*pb.ServiceAttr, error) {
	subreq := serviceAttrGetReq{
		key: key,
	}
	resp := s.handleReq(ctx, key.ServiceUUID, subreq)
	return resp.attr, resp.err
}

func (s *serviceAttrSvc) DeleteServiceAttr(ctx context.Context, key *pb.ServiceAttrKey) error {
	subreq := serviceAttrDeleteReq{
		key: key,
	}
	resp := s.handleReq(ctx, key.ServiceUUID, subreq)
	return resp.err
}

func (s *serviceAttrSvc) UpdateServiceAttr(ctx context.Context, req *pb.UpdateServiceAttrRequest) error {
	subreq := serviceAttrUpdateReq{
		updateReq: req,
	}
	resp := s.handleReq(ctx, req.OldAttr.ServiceUUID, subreq)
	return resp.err
}

func (s *serviceAttrSvc) handleReq(ctx context.Context, serviceUUID string, subreq interface{}) serviceAttrResp {
	req := serviceAttrReq{
		ctx:         ctx,
		serviceUUID: serviceUUID,
		subreq:      subreq,
		resChan:     make(chan serviceAttrResp),
	}

	return s.sendAndWaitReq(req)
}

func (s *serviceAttrSvc) sendAndWaitReq(req serviceAttrReq) serviceAttrResp {
	requuid := utils.GetReqIDFromContext(req.ctx)

	// send req to chan
	s.reqChan <- req

	// wait for response
	select {
	case resp := <-req.resChan:
		return resp
	case <-req.ctx.Done():
		glog.Infoln("sendAndWaitReq request context done, requuid", requuid)
		return serviceAttrResp{err: db.ErrDBInternal}
	case <-time.After(requestTimeOutSecs * time.Second):
		glog.Errorln("sendAndWaitReq serviceAttrReq timeout, requuid", requuid)
		return serviceAttrResp{err: db.ErrDBInternal}
	}
}

func (s *serviceAttrSvc) sendResp(ctx context.Context, c chan<- serviceAttrResp, resp serviceAttrResp) {
	requuid := utils.GetReqIDFromContext(ctx)
	select {
	case c <- resp:
		glog.Infoln("sent resp done, requuid", requuid)
	case <-ctx.Done():
		glog.Infoln("sendResp request context done, requuid", requuid)
	case <-time.After(requestTimeOutSecs * time.Second):
		glog.Errorln("send serviceAttrResp timeout, requuid", requuid)
	}
}

func (s *serviceAttrSvc) Stop() {
	select {
	case s.quitChan <- true:
		glog.Infoln("stopped")
	case <-time.After(requestTimeOutSecs * time.Second):
		glog.Infoln("time out at stop")
	}
}

// the single entrance to serve
func (s *serviceAttrSvc) serve() {
	glog.Infoln("serviceAttrSvc start serving, rootDir", s.rootDir)
	for {
		select {
		case <-s.quitChan:
			glog.Infoln("receive quit")
			break

		case req := <-s.reqChan:
			attrio, err := s.getAttrReadWriter(req)
			if err != nil {
				resp := serviceAttrResp{err: err}
				s.sendResp(req.ctx, req.resChan, resp)
			} else {
				resp := serviceAttrResp{}
				switch subreq := req.subreq.(type) {
				case serviceAttrCreateReq:
					resp.err = attrio.createAttr(req.ctx, subreq.attr)
				case serviceAttrGetReq:
					resp.attr, resp.err = attrio.getAttr(req.ctx, subreq.key)
				case serviceAttrUpdateReq:
					resp.err = attrio.updateAttr(req.ctx, subreq.updateReq)
				case serviceAttrDeleteReq:
					resp.err = attrio.deleteAttr(req.ctx, subreq.key)
					if resp.err == nil {
						// service attr deleted, remove from lruCache
						s.lruCache.Remove(req.serviceUUID)
					}
				}

				s.sendResp(req.ctx, req.resChan, resp)
			}
		}
	}
	glog.Infoln("serviceAttrSvc stop serving, rootDir", s.rootDir)
}

func (s *serviceAttrSvc) getAttrReadWriter(req serviceAttrReq) (attrio *attrReadWriter, err error) {
	requuid := utils.GetReqIDFromContext(req.ctx)

	// get attrReadWriter instance from lruCache
	cacheVal, ok := s.lruCache.Get(req.serviceUUID)
	if ok {
		// attrReadWriter in cache
		cval := cacheVal.(attrCacheValue)
		attrio = cval.attrio
		glog.Infoln("get attrReadWriter from cache, for service", req.serviceUUID, "requuid", requuid)
	} else {
		// attrReadWriter not in cache, create one and add to cache
		_, isCreate := req.subreq.(serviceAttrCreateReq)
		if isCreate {
			attrio, err = newAttrReadWriter(req.ctx, s.rootDir, req.serviceUUID)
		} else {
			attrio, err = newAttrReadWriterIfExist(req.ctx, s.rootDir, req.serviceUUID)
		}

		if err != nil {
			return attrio, err
		}

		glog.Infoln("newAttrReadWriter for service", req.serviceUUID, "requuid", requuid)
		cval := attrCacheValue{
			attrio: attrio,
		}
		s.lruCache.Add(req.serviceUUID, cval)
	}

	return attrio, nil
}

type serviceAttrResp struct {
	attr *pb.ServiceAttr
	err  error
}

type serviceAttrReq struct {
	ctx         context.Context
	serviceUUID string

	// detail request
	subreq interface{}

	// the chan to send the response
	resChan chan serviceAttrResp
}

type serviceAttrCreateReq struct {
	attr *pb.ServiceAttr
}

type serviceAttrGetReq struct {
	key *pb.ServiceAttrKey
}

type serviceAttrUpdateReq struct {
	updateReq *pb.UpdateServiceAttrRequest
}

type serviceAttrDeleteReq struct {
	key *pb.ServiceAttrKey
}

// write/read the ServiceAttr to/from file
type attrReadWriter struct {
	serviceUUID string
	// the in-memory copy of the attribute
	attr  *pb.ServiceAttr
	store *versionfilestore
}

func newAttrReadWriterIfExist(ctx context.Context, parentDir string, serviceUUID string) (*attrReadWriter, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	serviceRootDir := path.Join(parentDir, serviceUUID)
	_, err := os.Stat(serviceRootDir)
	if err != nil {
		glog.Errorln("Stat serviceRootDir error", err, serviceRootDir, "requuid", requuid)
		if os.IsNotExist(err) {
			return nil, db.ErrDBRecordNotFound
		}
		return nil, db.ErrDBInternal
	}

	return newAttrReadWriter(ctx, parentDir, serviceUUID)
}

func newAttrReadWriter(ctx context.Context, parentDir string, serviceUUID string) (*attrReadWriter, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// /rootPath/cluster/service/serviceuuid/
	store, err := newVersionFileStore(fileTypeServiceAttr, path.Join(parentDir, serviceUUID))
	if err != nil {
		glog.Errorln("new service attr versionfilestore error", err,
			"parentDir", parentDir, "serviceUUID", serviceUUID, "requuid", requuid)
		return nil, db.ErrDBInternal
	}

	r := &attrReadWriter{
		serviceUUID: serviceUUID,
		attr:        nil,
		store:       store,
	}

	if !r.store.HasCurrentVersion() {
		glog.Infoln("newAttrReadWriter done, no attr, parentDir",
			parentDir, "serviceUUID", serviceUUID, "requuid", requuid)
		return r, nil
	}

	// load ServiceAttr from the current version file
	data, err := r.store.ReadCurrentVersion("newServiceAttrSvc", requuid)
	if err != nil {
		glog.Errorln("read current version error", err,
			"parentDir", parentDir, "serviceUUID", serviceUUID, "requuid", requuid)
		return nil, db.ErrDBInternal
	}

	// deserialize
	attr := &pb.ServiceAttr{}
	err = proto.Unmarshal(data, attr)
	if err != nil {
		glog.Errorln("Unmarshal ServiceAttr error", err, "parentDir",
			parentDir, "serviceUUID", serviceUUID, "requuid", requuid)
		return nil, db.ErrDBInternal
	}

	r.attr = attr

	glog.Infoln("newAttrReadWriter done, attr", attr, "parentDir", parentDir, "requuid", requuid)
	return r, nil
}

func (r *attrReadWriter) createAttr(ctx context.Context, attr *pb.ServiceAttr) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// sanity check
	if attr.ServiceUUID != r.serviceUUID {
		glog.Errorln("attr is not for service", r.serviceUUID, "attr", attr)
		return db.ErrDBInvalidRequest
	}

	if r.attr != nil {
		// service attr exists, return error. Client should get the attr and check whether it is a retry.
		glog.Errorln("createAttr service exists", r.attr, "creating attr", attr, "requuid", requuid)
		return db.ErrDBConditionalCheckFailed
	}

	// service attr not exists, create it
	data, err := proto.Marshal(attr)
	if err != nil {
		glog.Errorln("createAttr Marshal ServiceAttr error", err, attr, "requuid", requuid)
		return db.ErrDBInternal
	}

	err = r.store.CreateNewVersionFile(data, "createAttr", requuid)
	if err != nil {
		glog.Errorln("createAttr, createNewVersionFile error", err, attr, "requuid", requuid)
		return db.ErrDBInternal
	}

	// successfully created the attr file, update in-memory cache
	r.attr = attr

	glog.Infoln("createAttr done", attr, "requuid", requuid)
	return nil
}

func (r *attrReadWriter) getAttr(ctx context.Context, key *pb.ServiceAttrKey) (*pb.ServiceAttr, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// sanity check
	if key.ServiceUUID != r.serviceUUID {
		glog.Errorln("attr is not for service", r.serviceUUID, "key", key)
		return nil, db.ErrDBInvalidRequest
	}

	if r.attr == nil {
		glog.Errorln("getAttr attr not exist", key, "requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	glog.Infoln("getAttr", r.attr, "requuid", requuid)
	return r.attr, nil
}

func (r *attrReadWriter) updateAttr(ctx context.Context, req *pb.UpdateServiceAttrRequest) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// sanity check
	if r.attr == nil {
		glog.Errorln("updateAttr, no attr", r.serviceUUID, "requuid", requuid)
		return db.ErrDBRecordNotFound
	}

	if r.attr.ServiceUUID != req.OldAttr.ServiceUUID ||
		r.attr.ServiceUUID != req.NewAttr.ServiceUUID {
		glog.Errorln("attr is not for service", r.serviceUUID, "oldAttr", req.OldAttr.ServiceUUID, "newAttr", req.NewAttr.ServiceUUID)
		return db.ErrDBInvalidRequest
	}
	if r.attr.ServiceStatus != req.OldAttr.ServiceStatus ||
		r.attr.Replicas != req.OldAttr.Replicas ||
		r.attr.ClusterName != req.OldAttr.ClusterName ||
		r.attr.DomainName != req.OldAttr.DomainName ||
		r.attr.HostedZoneID != req.OldAttr.HostedZoneID ||
		r.attr.RegisterDNS != req.OldAttr.RegisterDNS ||
		r.attr.RequireStaticIP != req.OldAttr.RequireStaticIP ||
		r.attr.ServiceName != req.OldAttr.ServiceName ||
		r.attr.ServiceType != req.OldAttr.ServiceType {
		glog.Errorln("OldAttr does not match current attr", r.attr, "oldAttr", req.OldAttr, "requuid", requuid)
		return db.ErrDBConditionalCheckFailed
	}
	if r.attr.ClusterName != req.NewAttr.ClusterName ||
		r.attr.DomainName != req.NewAttr.DomainName ||
		r.attr.HostedZoneID != req.NewAttr.HostedZoneID ||
		r.attr.RegisterDNS != req.NewAttr.RegisterDNS ||
		r.attr.RequireStaticIP != req.NewAttr.RequireStaticIP ||
		r.attr.ServiceName != req.NewAttr.ServiceName ||
		r.attr.ServiceType != req.NewAttr.ServiceType {
		glog.Errorln("immutable fields could not be updated, current attr", r.attr, "newAttr", req.NewAttr, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	data, err := proto.Marshal(req.NewAttr)
	if err != nil {
		glog.Errorln("updateAttr Marshal ServiceAttr error", err, req.NewAttr, "requuid", requuid)
		return db.ErrDBInternal
	}

	err = r.store.CreateNewVersionFile(data, "updateAttr", requuid)
	if err != nil {
		glog.Errorln("updateAttr, createNewVersionFile error", err, r.serviceUUID, "requuid", requuid)
		return db.ErrDBInternal
	}

	// successfully created the attr file, update in-memory cache
	r.attr = req.NewAttr

	glog.Infoln("updateAttr done", r.serviceUUID, "requuid", requuid)
	return nil
}

func (r *attrReadWriter) deleteAttr(ctx context.Context, key *pb.ServiceAttrKey) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// sanity check
	if key.ServiceUUID != r.serviceUUID {
		glog.Errorln("attr is not for service", r.serviceUUID, "key", key)
		return db.ErrDBInvalidRequest
	}

	if r.attr == nil {
		glog.Errorln("deleteAttr attr not exist", key, "requuid", requuid)
		return db.ErrDBRecordNotFound
	}

	err := r.store.DeleteAll()
	if err != nil {
		glog.Errorln("delete service attr dir error", err, key, "requuid", requuid)
		return db.ErrDBInternal
	}

	// reset attr to nil
	attr := r.attr
	r.attr = nil

	glog.Infoln("deleteAttr", attr, "requuid", requuid)
	return nil
}
