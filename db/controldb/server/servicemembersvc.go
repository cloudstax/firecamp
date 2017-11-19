package controldbserver

import (
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/golang/groupcache/lru"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"

	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/db/controldb"
	pb "github.com/cloudstax/firecamp/db/controldb/protocols"
	"github.com/cloudstax/firecamp/utils"
)

type serviceMemberSvc struct {
	// /topRoot/cluster/servicedir/
	rootDir string

	// the quitChan from the caller
	quitChan chan bool

	// lru cache, key: serviceUUID, value: serviceMemberCacheValue
	lruCache *lru.Cache

	// chan for the requests
	reqChan chan serviceMemberReq
}

type serviceMemberCacheValue struct {
	memberio *serviceMemberReadWriter
}

func newServiceMemberSvc(rootDir string) *serviceMemberSvc {
	return newServiceMemberSvcWithMaxCacheSize(rootDir, maxServiceMemberCacheEntry)
}

func newServiceMemberSvcWithMaxCacheSize(rootDir string, maxCacheSize int) *serviceMemberSvc {
	s := &serviceMemberSvc{
		rootDir:  rootDir,
		lruCache: lru.New(maxCacheSize),
		quitChan: make(chan bool),
		reqChan:  make(chan serviceMemberReq),
	}

	go s.serve()
	return s
}

func (s *serviceMemberSvc) CreateServiceMember(ctx context.Context, member *pb.ServiceMember) error {
	subreq := serviceMemberCreateReq{
		member: member,
	}
	resp := s.handleReq(ctx, member.ServiceUUID, subreq)
	return resp.err
}

func (s *serviceMemberSvc) GetServiceMember(ctx context.Context, key *pb.ServiceMemberKey) (*pb.ServiceMember, error) {
	subreq := serviceMemberGetReq{
		key: key,
	}
	resp := s.handleReq(ctx, key.ServiceUUID, subreq)
	return resp.member, resp.err
}

func (s *serviceMemberSvc) DeleteServiceMember(ctx context.Context, key *pb.ServiceMemberKey) error {
	subreq := serviceMemberDeleteReq{
		key: key,
	}
	resp := s.handleReq(ctx, key.ServiceUUID, subreq)
	return resp.err
}

func (s *serviceMemberSvc) UpdateServiceMember(ctx context.Context, req *pb.UpdateServiceMemberRequest) error {
	subreq := serviceMemberUpdateReq{
		updateReq: req,
	}
	resp := s.handleReq(ctx, req.OldMember.ServiceUUID, subreq)
	return resp.err
}

func (s *serviceMemberSvc) listServiceMembers(ctx context.Context, req *pb.ListServiceMemberRequest) ([]*pb.ServiceMember, error) {
	subreq := serviceMemberListReq{
		listReq: req,
	}
	resp := s.handleReq(ctx, req.ServiceUUID, subreq)
	return resp.serviceMembers, resp.err
}

func (s *serviceMemberSvc) ListServiceMembers(req *pb.ListServiceMemberRequest, stream pb.ControlDBService_ListServiceMembersServer) error {
	ctx := stream.Context()
	members, err := s.listServiceMembers(ctx, req)
	if err != nil {
		glog.Errorln("ListServiceMembers error", err, "req", req)
		return err
	}

	for _, member := range members {
		err := stream.Send(member)
		if err != nil {
			glog.Errorln("send serviceMember error", err, member)
			return err
		}
	}

	return nil
}

func (s *serviceMemberSvc) handleReq(ctx context.Context, serviceUUID string, subreq interface{}) serviceMemberResp {
	req := serviceMemberReq{
		ctx:         ctx,
		serviceUUID: serviceUUID,
		subreq:      subreq,
		resChan:     make(chan serviceMemberResp),
	}

	return s.sendAndWaitReq(req)
}

func (s *serviceMemberSvc) sendAndWaitReq(req serviceMemberReq) serviceMemberResp {
	requuid := utils.GetReqIDFromContext(req.ctx)

	// send req to chan
	s.reqChan <- req

	// wait for response
	select {
	case resp := <-req.resChan:
		return resp
	case <-req.ctx.Done():
		glog.Infoln("sendAndWaitReq request context done, requuid", requuid)
		return serviceMemberResp{err: db.ErrDBInternal}
	case <-time.After(requestTimeOutSecs * time.Second):
		glog.Errorln("sendAndWaitReq serviceMemberReq timeout, requuid", requuid)
		return serviceMemberResp{err: db.ErrDBInternal}
	}
}

func (s *serviceMemberSvc) sendResp(ctx context.Context, c chan<- serviceMemberResp, resp serviceMemberResp) {
	requuid := utils.GetReqIDFromContext(ctx)
	select {
	case c <- resp:
		glog.Infoln("sent resp done, requuid", requuid)
	case <-ctx.Done():
		glog.Infoln("sendResp request context done, requuid", requuid)
	case <-time.After(requestTimeOutSecs * time.Second):
		glog.Errorln("send serviceMemberResp timeout, requuid", requuid)
	}
}

func (s *serviceMemberSvc) Stop() {
	select {
	case s.quitChan <- true:
		glog.Infoln("stopped")
	case <-time.After(requestTimeOutSecs * time.Second):
		glog.Infoln("time out at stop")
	}
}

// the single entrance to serve
func (s *serviceMemberSvc) serve() {
	glog.Infoln("serviceMemberSvc start serving, rootDir", s.rootDir)
	for {
		select {
		case <-s.quitChan:
			glog.Infoln("receive quit")
			break

		case req := <-s.reqChan:
			memberio, err := s.getServiceMemberReadWriter(req)
			if err != nil {
				resp := serviceMemberResp{err: err}
				s.sendResp(req.ctx, req.resChan, resp)
			} else {
				resp := serviceMemberResp{}
				switch subreq := req.subreq.(type) {
				case serviceMemberCreateReq:
					resp.err = memberio.createServiceMember(req.ctx, subreq.member)
				case serviceMemberGetReq:
					resp.member, resp.err = memberio.getServiceMember(req.ctx, subreq.key)
				case serviceMemberUpdateReq:
					resp.err = memberio.updateServiceMember(req.ctx, subreq.updateReq)
				case serviceMemberDeleteReq:
					resp.err = memberio.deleteServiceMember(req.ctx, subreq.key)
				case serviceMemberListReq:
					resp.serviceMembers, resp.err = memberio.listServiceMembers(subreq.listReq)
				}

				s.sendResp(req.ctx, req.resChan, resp)
			}
		}
	}
	glog.Infoln("serviceMemberSvc stop serving, rootDir", s.rootDir)
}

func (s *serviceMemberSvc) getServiceMemberReadWriter(req serviceMemberReq) (memberio *serviceMemberReadWriter, err error) {
	requuid := utils.GetReqIDFromContext(req.ctx)

	// get serviceMemberReadWriter instance from lruCache
	cacheVal, ok := s.lruCache.Get(req.serviceUUID)
	if ok {
		// serviceMemberReadWriter in cache
		memberio = cacheVal.(serviceMemberCacheValue).memberio
		glog.Infoln("get serviceMemberReadWriter from cache, service", req.serviceUUID, "requuid", requuid)
	} else {
		// serviceMemberReadWriter not in cache, create one and add to cache
		memberio, err = newServiceMemberReadWriter(req.ctx, s.rootDir, req.serviceUUID)
		if err != nil {
			return nil, err
		}

		glog.Infoln("newServiceMemberReadWriter for service", req.serviceUUID, "requuid", requuid)
		cval := serviceMemberCacheValue{
			memberio: memberio,
		}
		s.lruCache.Add(req.serviceUUID, cval)
	}

	return memberio, nil
}

type serviceMemberResp struct {
	// one serviceMember, response for the Create/Get/Update/Delete request
	member *pb.ServiceMember
	// all serviceMembers of one service, response for the List request
	serviceMembers []*pb.ServiceMember
	err            error
}

type serviceMemberReq struct {
	ctx         context.Context
	serviceUUID string

	// detail request
	subreq interface{}

	// the chan to send the response
	resChan chan serviceMemberResp
}

type serviceMemberCreateReq struct {
	member *pb.ServiceMember
}

type serviceMemberGetReq struct {
	key *pb.ServiceMemberKey
}

type serviceMemberUpdateReq struct {
	updateReq *pb.UpdateServiceMemberRequest
}

type serviceMemberDeleteReq struct {
	key *pb.ServiceMemberKey
}

type serviceMemberListReq struct {
	listReq *pb.ListServiceMemberRequest
}

// serviceMemberReadWriter will load all serviceMembers of one service, as the major requests
// would be ListServiceMembers and UpdateServiceMember when a container tries to mount the serviceMember.
// This class is not thread-safe. The caller will protect the concurrent accesses.
type serviceMemberReadWriter struct {
	rootDir     string
	serviceUUID string
	memberDir   string
	// all serviceMembers of the service, key: MemberIndex
	members map[int64]*pb.ServiceMember
}

func newServiceMemberReadWriter(ctx context.Context, rootDir string, serviceUUID string) (*serviceMemberReadWriter, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	r := &serviceMemberReadWriter{
		rootDir:     rootDir,
		serviceUUID: serviceUUID,
		memberDir:   path.Join(rootDir, serviceUUID, serviceMemberDirName),
		members:     make(map[int64]*pb.ServiceMember),
	}

	// list all serviceMembers
	err := utils.CreateDirIfNotExist(r.memberDir)
	if err != nil {
		glog.Errorln("create serviceMember dir error", err, r.memberDir)
		return nil, db.ErrDBInternal
	}

	dir, err := os.Open(r.memberDir)
	if err != nil {
		glog.Errorln("open serviceMember dir error", err, r.memberDir)
		return nil, db.ErrDBInternal
	}
	defer dir.Close()

	files, err := dir.Readdirnames(-1)
	if err != nil {
		glog.Errorln("readdir error", err, r.memberDir)
		return nil, db.ErrDBInternal
	}

	for _, file := range files {
		filepath := path.Join(r.memberDir, file)

		if r.isTmpFile(file) {
			glog.Infoln("tmp serviceMember file", file, r.memberDir)
			// recycle the possible garbage tmp file, this will only happen if controldb server
			// crashes before the tmp file is renamed to the serviceMember file
			os.Remove(filepath)
			continue
		}

		// read serviceMember file
		data, err := readFile(filepath, "newServiceMemberReadWriter", requuid)
		if err != nil {
			glog.Errorln("read serviceMember file error", err, filepath, "requuid", requuid)
			return nil, db.ErrDBInternal
		}

		// deserialize
		member := &pb.ServiceMember{}
		err = proto.Unmarshal(data, member)
		if err != nil {
			glog.Errorln("Unmarshal ServiceMember error", err, file, "requuid", requuid)
			return nil, db.ErrDBInternal
		}

		r.members[member.MemberIndex] = member
	}

	glog.Infoln("newServiceMemberReadWriter done for service", serviceUUID, "serviceMembers",
		len(r.members), "requuid", requuid)
	return r, nil
}

func (r *serviceMemberReadWriter) isTmpFile(filename string) bool {
	return strings.HasSuffix(filename, tmpFileSuffix)
}

func (r *serviceMemberReadWriter) createServiceMember(ctx context.Context, member *pb.ServiceMember) error {
	requuid := utils.GetReqIDFromContext(ctx)

	if member.ServiceUUID != r.serviceUUID {
		glog.Errorln("createServiceMember", member, "not belong to service", r.serviceUUID)
		return db.ErrDBInvalidRequest
	}

	currmember, ok := r.members[member.MemberIndex]
	if ok {
		// serviceMember member exists, check whether the creating member is the same with the current member
		skipMtime := true
		if controldb.EqualServiceMember(currmember, member, skipMtime) {
			glog.Infoln("createServiceMember done, serviceMember exists with same member", member, currmember, "requuid", requuid)
			return nil
		}

		glog.Errorln("createServiceMember serviceMember exists", currmember, "creating member", member, "requuid", requuid)
		return db.ErrDBConditionalCheckFailed
	}

	// serviceMember not exists, create it
	err := r.createServiceMemberFile(member, "createServiceMember", requuid)
	if err != nil {
		return err
	}

	// successfully created the member file, cache it
	r.members[member.MemberIndex] = member

	glog.Infoln("createServiceMember done", member, "requuid", requuid)
	return nil
}

func (r *serviceMemberReadWriter) createServiceMemberFile(member *pb.ServiceMember, op string, requuid string) error {
	data, err := proto.Marshal(member)
	if err != nil {
		glog.Errorln(op, "Marshal ServiceMember error", err, member, "requuid", requuid)
		return db.ErrDBInternal
	}

	// write data and checksum to the tmp file
	tmpfilepath := path.Join(r.memberDir, strconv.FormatInt(member.MemberIndex, 10)) + tmpFileSuffix
	err = createFile(tmpfilepath, data, op, requuid)
	if err != nil {
		glog.Errorln(op, "create tmp file", tmpfilepath, "error", err, "requuid", requuid)
		os.Remove(tmpfilepath)
		return db.ErrDBInternal
	}

	// rename tmpfile to the serviceMember file
	filepath := path.Join(r.memberDir, strconv.FormatInt(member.MemberIndex, 10))
	err = os.Rename(tmpfilepath, filepath)
	if err != nil {
		glog.Errorln(op, "rename tmpfile", tmpfilepath, "to", filepath, "error", err, "requuid", requuid)
		os.Remove(tmpfilepath)
		return db.ErrDBInternal
	}

	return nil
}

func (r *serviceMemberReadWriter) getServiceMember(ctx context.Context, key *pb.ServiceMemberKey) (*pb.ServiceMember, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	if key.ServiceUUID != r.serviceUUID {
		glog.Errorln("getServiceMember", key, "not belong to service", r.serviceUUID)
		return nil, db.ErrDBInvalidRequest
	}
	currmember, ok := r.members[key.MemberIndex]
	if !ok {
		glog.Errorln("getServiceMember member not exist", key, "requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	glog.Infoln("getServiceMember done", currmember, "requuid", requuid)
	return currmember, nil
}

func (r *serviceMemberReadWriter) updateServiceMember(ctx context.Context, req *pb.UpdateServiceMemberRequest) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// sanity check
	if req.NewMember.ServiceUUID != req.OldMember.ServiceUUID ||
		!controldb.EqualsMemberVolumes(req.NewMember.Volumes, req.OldMember.Volumes) ||
		req.NewMember.AvailableZone != req.OldMember.AvailableZone ||
		req.NewMember.MemberIndex != req.OldMember.MemberIndex ||
		req.NewMember.MemberName != req.OldMember.MemberName ||
		req.NewMember.StaticIP != req.OldMember.StaticIP {
		glog.Errorln("updateServiceMember, the immutable serviceMember attributes are updated", req.OldMember, req.NewMember, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}
	if req.OldMember.ServiceUUID != r.serviceUUID {
		glog.Errorln("updateServiceMember, req not belong to service", r.serviceUUID, "requuid", requuid, req.OldMember)
		return db.ErrDBInvalidRequest
	}

	// get the current serviceMember
	currmember, ok := r.members[req.OldMember.MemberIndex]
	if !ok {
		glog.Errorln("updateServiceMember, no member", req.OldMember.MemberName, "requuid", requuid)
		return db.ErrDBRecordNotFound
	}

	// sanity check
	skipMtime := true
	if !controldb.EqualServiceMember(currmember, req.OldMember, skipMtime) {
		glog.Errorln("updateServiceMember, req oldMember", req.OldMember,
			"not match current member", currmember, "requuid", requuid)
		return db.ErrDBConditionalCheckFailed
	}

	err := r.createServiceMemberFile(req.NewMember, "updateServiceMember", requuid)
	if err != nil {
		return err
	}

	r.members[req.OldMember.MemberIndex] = req.NewMember

	glog.Infoln("updateServiceMember done", req.NewMember, "requuid", requuid)
	return nil
}

func (r *serviceMemberReadWriter) deleteServiceMember(ctx context.Context, key *pb.ServiceMemberKey) error {
	requuid := utils.GetReqIDFromContext(ctx)

	if key.ServiceUUID != r.serviceUUID {
		glog.Errorln("deleteServiceMember", key, "not belong to service", r.serviceUUID)
		return db.ErrDBInvalidRequest
	}

	currmember, ok := r.members[key.MemberIndex]
	if !ok {
		glog.Errorln("deleteServiceMember member not exist", key, "requuid", requuid)
		return db.ErrDBRecordNotFound
	}

	filepath := path.Join(r.memberDir, strconv.FormatInt(key.MemberIndex, 10))
	err := os.Remove(filepath)
	if err != nil {
		glog.Errorln("delete serviceMember file error", err, filepath, "requuid", requuid)
		return db.ErrDBInternal
	}

	delete(r.members, key.MemberIndex)

	glog.Infoln("deleteServiceMember done", currmember, "requuid", requuid)
	return nil
}

func (r *serviceMemberReadWriter) listServiceMembers(req *pb.ListServiceMemberRequest) ([]*pb.ServiceMember, error) {
	if req.ServiceUUID != r.serviceUUID {
		glog.Errorln("listServiceMember", req, "not belong to service", r.serviceUUID)
		return nil, db.ErrDBInvalidRequest
	}

	members := make([]*pb.ServiceMember, len(r.members))
	i := 0
	for _, member := range r.members {
		members[i] = member
		i++
	}
	glog.Infoln("list", len(members), "serviceMembers, service", req.ServiceUUID)
	return members, nil
}
