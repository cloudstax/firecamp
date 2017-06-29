package controldbserver

import (
	"os"
	"path"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/golang/groupcache/lru"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/db"
	"github.com/cloudstax/openmanage/db/controldb"
	pb "github.com/cloudstax/openmanage/db/controldb/protocols"
	"github.com/cloudstax/openmanage/utils"
)

type volumeSvc struct {
	// /topRoot/cluster/servicedir/
	rootDir string

	// the quitChan from the caller
	quitChan chan bool

	// lru cache, key: serviceUUID, value: volumeCacheValue
	lruCache *lru.Cache

	// chan for the requests
	reqChan chan volumeReq
}

type volumeCacheValue struct {
	volio *volumeReadWriter
}

func newVolumeSvc(rootDir string) *volumeSvc {
	return newVolumeSvcWithMaxCacheSize(rootDir, maxVolumeCacheEntry)
}

func newVolumeSvcWithMaxCacheSize(rootDir string, maxCacheSize int) *volumeSvc {
	s := &volumeSvc{
		rootDir:  rootDir,
		lruCache: lru.New(maxCacheSize),
		quitChan: make(chan bool),
		reqChan:  make(chan volumeReq),
	}

	go s.serve()
	return s
}

func (s *volumeSvc) CreateVolume(ctx context.Context, vol *pb.Volume) error {
	subreq := volumeCreateReq{
		vol: vol,
	}
	resp := s.handleReq(ctx, vol.ServiceUUID, subreq)
	return resp.err
}

func (s *volumeSvc) GetVolume(ctx context.Context, key *pb.VolumeKey) (*pb.Volume, error) {
	subreq := volumeGetReq{
		key: key,
	}
	resp := s.handleReq(ctx, key.ServiceUUID, subreq)
	return resp.vol, resp.err
}

func (s *volumeSvc) DeleteVolume(ctx context.Context, key *pb.VolumeKey) error {
	subreq := volumeDeleteReq{
		key: key,
	}
	resp := s.handleReq(ctx, key.ServiceUUID, subreq)
	return resp.err
}

func (s *volumeSvc) UpdateVolume(ctx context.Context, req *pb.UpdateVolumeRequest) error {
	subreq := volumeUpdateReq{
		updateReq: req,
	}
	resp := s.handleReq(ctx, req.OldVol.ServiceUUID, subreq)
	return resp.err
}

func (s *volumeSvc) listVolumes(ctx context.Context, req *pb.ListVolumeRequest) ([]*pb.Volume, error) {
	subreq := volumeListReq{
		listReq: req,
	}
	resp := s.handleReq(ctx, req.ServiceUUID, subreq)
	return resp.volumes, resp.err
}

func (s *volumeSvc) ListVolumes(req *pb.ListVolumeRequest, stream pb.ControlDBService_ListVolumesServer) error {
	ctx := stream.Context()
	vols, err := s.listVolumes(ctx, req)
	if err != nil {
		glog.Errorln("ListVolumes error", err, "req", req)
		return err
	}

	for _, vol := range vols {
		err := stream.Send(vol)
		if err != nil {
			glog.Errorln("send volume error", err, vol)
			return err
		}
	}

	return nil
}

func (s *volumeSvc) handleReq(ctx context.Context, serviceUUID string, subreq interface{}) volumeResp {
	req := volumeReq{
		ctx:         ctx,
		serviceUUID: serviceUUID,
		subreq:      subreq,
		resChan:     make(chan volumeResp),
	}

	return s.sendAndWaitReq(req)
}

func (s *volumeSvc) sendAndWaitReq(req volumeReq) volumeResp {
	requuid := utils.GetReqIDFromContext(req.ctx)

	// send req to chan
	s.reqChan <- req

	// wait for response
	select {
	case resp := <-req.resChan:
		return resp
	case <-req.ctx.Done():
		glog.Infoln("sendAndWaitReq request context done, requuid", requuid)
		return volumeResp{err: db.ErrDBInternal}
	case <-time.After(requestTimeOutSecs * time.Second):
		glog.Errorln("sendAndWaitReq volumeReq timeout, requuid", requuid)
		return volumeResp{err: db.ErrDBInternal}
	}
}

func (s *volumeSvc) sendResp(ctx context.Context, c chan<- volumeResp, resp volumeResp) {
	requuid := utils.GetReqIDFromContext(ctx)
	select {
	case c <- resp:
		glog.Infoln("sent resp done, requuid", requuid)
	case <-ctx.Done():
		glog.Infoln("sendResp request context done, requuid", requuid)
	case <-time.After(requestTimeOutSecs * time.Second):
		glog.Errorln("send volumeResp timeout, requuid", requuid)
	}
}

func (s *volumeSvc) Stop() {
	select {
	case s.quitChan <- true:
		glog.Infoln("stopped")
	case <-time.After(requestTimeOutSecs * time.Second):
		glog.Infoln("time out at stop")
	}
}

// the single entrance to serve
func (s *volumeSvc) serve() {
	glog.Infoln("volumeSvc start serving, rootDir", s.rootDir)
	for {
		select {
		case <-s.quitChan:
			glog.Infoln("receive quit")
			break

		case req := <-s.reqChan:
			volio, err := s.getVolumeReadWriter(req)
			if err != nil {
				resp := volumeResp{err: err}
				s.sendResp(req.ctx, req.resChan, resp)
			} else {
				resp := volumeResp{}
				switch subreq := req.subreq.(type) {
				case volumeCreateReq:
					resp.err = volio.createVolume(req.ctx, subreq.vol)
				case volumeGetReq:
					resp.vol, resp.err = volio.getVolume(req.ctx, subreq.key)
				case volumeUpdateReq:
					resp.err = volio.updateVolume(req.ctx, subreq.updateReq)
				case volumeDeleteReq:
					resp.err = volio.deleteVolume(req.ctx, subreq.key)
				case volumeListReq:
					resp.volumes, resp.err = volio.listVolumes(subreq.listReq)
				}

				s.sendResp(req.ctx, req.resChan, resp)
			}
		}
	}
	glog.Infoln("volumeSvc stop serving, rootDir", s.rootDir)
}

func (s *volumeSvc) getVolumeReadWriter(req volumeReq) (volio *volumeReadWriter, err error) {
	requuid := utils.GetReqIDFromContext(req.ctx)

	// get volumeReadWriter instance from lruCache
	cacheVal, ok := s.lruCache.Get(req.serviceUUID)
	if ok {
		// volumeReadWriter in cache
		volio = cacheVal.(volumeCacheValue).volio
		glog.Infoln("get volumeReadWriter from cache, service", req.serviceUUID, "requuid", requuid)
	} else {
		// volumeReadWriter not in cache, create one and add to cache
		volio, err = newVolumeReadWriter(req.ctx, s.rootDir, req.serviceUUID)
		if err != nil {
			return nil, err
		}

		glog.Infoln("newVolumeReadWriter for service", req.serviceUUID, "requuid", requuid)
		cval := volumeCacheValue{
			volio: volio,
		}
		s.lruCache.Add(req.serviceUUID, cval)
	}

	return volio, nil
}

type volumeResp struct {
	// one volume, response for the Create/Get/Update/Delete request
	vol *pb.Volume
	// all volumes of one service, response for the List request
	volumes []*pb.Volume
	err     error
}

type volumeReq struct {
	ctx         context.Context
	serviceUUID string

	// detail request
	subreq interface{}

	// the chan to send the response
	resChan chan volumeResp
}

type volumeCreateReq struct {
	vol *pb.Volume
}

type volumeGetReq struct {
	key *pb.VolumeKey
}

type volumeUpdateReq struct {
	updateReq *pb.UpdateVolumeRequest
}

type volumeDeleteReq struct {
	key *pb.VolumeKey
}

type volumeListReq struct {
	listReq *pb.ListVolumeRequest
}

// volumeReadWriter will load all volumes of one service, as the major requests
// would be ListVolumes and UpdateVolume when a container tries to mount the volume.
// This class is not thread-safe. The caller will protect the concurrent accesses.
type volumeReadWriter struct {
	rootDir     string
	serviceUUID string
	volDir      string
	// all volumes of the service, key: volumeID
	vols map[string]*pb.Volume
}

func newVolumeReadWriter(ctx context.Context, rootDir string, serviceUUID string) (*volumeReadWriter, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	r := &volumeReadWriter{
		rootDir:     rootDir,
		serviceUUID: serviceUUID,
		volDir:      path.Join(rootDir, serviceUUID, volumeDirName),
		vols:        make(map[string]*pb.Volume),
	}

	// list all volumes
	err := utils.CreateDirIfNotExist(r.volDir)
	if err != nil {
		glog.Errorln("create volume dir error", err, r.volDir)
		return nil, db.ErrDBInternal
	}

	dir, err := os.Open(r.volDir)
	if err != nil {
		glog.Errorln("open volume dir error", err, r.volDir)
		return nil, db.ErrDBInternal
	}
	defer dir.Close()

	files, err := dir.Readdirnames(-1)
	if err != nil {
		glog.Errorln("readdir error", err, r.volDir)
		return nil, db.ErrDBInternal
	}

	for _, file := range files {
		filepath := path.Join(r.volDir, file)

		if r.isTmpFile(file) {
			glog.Infoln("tmp volume file", file, r.volDir)
			// recycle the possible garbage tmp file, this will only happen if controldb server
			// crashes before the tmp file is renamed to the volume file
			os.Remove(filepath)
			continue
		}

		// read volume file
		data, err := readFile(filepath, "newVolumeReadWriter", requuid)
		if err != nil {
			glog.Errorln("read volume file error", err, filepath, "requuid", requuid)
			return nil, db.ErrDBInternal
		}

		// deserialize
		vol := &pb.Volume{}
		err = proto.Unmarshal(data, vol)
		if err != nil {
			glog.Errorln("Unmarshal Volume error", err, file, "requuid", requuid)
			return nil, db.ErrDBInternal
		}

		r.vols[vol.VolumeID] = vol
	}

	glog.Infoln("newVolumeReadWriter done for service", serviceUUID, "volumes",
		len(r.vols), "requuid", requuid)
	return r, nil
}

func (r *volumeReadWriter) isTmpFile(filename string) bool {
	return strings.HasSuffix(filename, tmpFileSuffix)
}

func (r *volumeReadWriter) createVolume(ctx context.Context, vol *pb.Volume) error {
	requuid := utils.GetReqIDFromContext(ctx)

	if vol.ServiceUUID != r.serviceUUID {
		glog.Errorln("createVolume", vol, "not belong to service", r.serviceUUID)
		return db.ErrDBInvalidRequest
	}

	currvol, ok := r.vols[vol.VolumeID]
	if ok {
		// volume vol exists, check whether the creating vol is the same with the current vol
		skipMtime := true
		if controldb.EqualVolume(currvol, vol, skipMtime) {
			glog.Infoln("createVolume done, volume exists with same vol", vol, currvol, "requuid", requuid)
			return nil
		}

		glog.Errorln("createVolume volume exists", currvol, "creating vol", vol, "requuid", requuid)
		return db.ErrDBConditionalCheckFailed
	}

	// volume not exists, create it
	err := r.createVolumeFile(vol, "createVolume", requuid)
	if err != nil {
		return err
	}

	// successfully created the vol file, cache it
	r.vols[vol.VolumeID] = vol

	glog.Infoln("createVolume done", vol, "requuid", requuid)
	return nil
}

func (r *volumeReadWriter) createVolumeFile(vol *pb.Volume, op string, requuid string) error {
	data, err := proto.Marshal(vol)
	if err != nil {
		glog.Errorln(op, "Marshal Volume error", err, vol, "requuid", requuid)
		return db.ErrDBInternal
	}

	// write data and checksum to the tmp file
	tmpfilepath := path.Join(r.volDir, vol.VolumeID) + tmpFileSuffix
	err = createFile(tmpfilepath, data, op, requuid)
	if err != nil {
		glog.Errorln(op, "create tmp file", tmpfilepath, "error", err, "requuid", requuid)
		os.Remove(tmpfilepath)
		return db.ErrDBInternal
	}

	// rename tmpfile to the volume file
	filepath := path.Join(r.volDir, vol.VolumeID)
	err = os.Rename(tmpfilepath, filepath)
	if err != nil {
		glog.Errorln(op, "rename tmpfile", tmpfilepath, "to", filepath, "error", err, "requuid", requuid)
		os.Remove(tmpfilepath)
		return db.ErrDBInternal
	}

	return nil
}

func (r *volumeReadWriter) getVolume(ctx context.Context, key *pb.VolumeKey) (*pb.Volume, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	if key.ServiceUUID != r.serviceUUID {
		glog.Errorln("getVolume", key, "not belong to service", r.serviceUUID)
		return nil, db.ErrDBInvalidRequest
	}
	currvol, ok := r.vols[key.VolumeID]
	if !ok {
		glog.Errorln("getVolume vol not exist", key, "requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	glog.Infoln("getVolume done", currvol, "requuid", requuid)
	return currvol, nil
}

func (r *volumeReadWriter) updateVolume(ctx context.Context, req *pb.UpdateVolumeRequest) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// sanity check
	if req.NewVol.ServiceUUID != req.OldVol.ServiceUUID ||
		req.NewVol.VolumeID != req.OldVol.VolumeID ||
		req.NewVol.DeviceName != req.OldVol.DeviceName ||
		req.NewVol.AvailableZone != req.OldVol.AvailableZone ||
		req.NewVol.MemberName != req.OldVol.MemberName {
		glog.Errorln("updateVolume, the immutable volume attributes are updated", req.OldVol, req.NewVol, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}
	if req.OldVol.ServiceUUID != r.serviceUUID {
		glog.Errorln("updateVolume, req not belong to service", r.serviceUUID, "requuid", requuid, req.OldVol)
		return db.ErrDBInvalidRequest
	}

	// get the current volume
	currvol, ok := r.vols[req.OldVol.VolumeID]
	if !ok {
		glog.Errorln("updateVolume, no vol", req.OldVol.VolumeID, "requuid", requuid)
		return db.ErrDBRecordNotFound
	}

	// sanity check
	skipMtime := true
	if !controldb.EqualVolume(currvol, req.OldVol, skipMtime) {
		glog.Errorln("updateVolume, req oldVol", req.OldVol,
			"not match current vol", currvol, "requuid", requuid)
		return db.ErrDBConditionalCheckFailed
	}

	err := r.createVolumeFile(req.NewVol, "updateVolume", requuid)
	if err != nil {
		return err
	}

	r.vols[req.OldVol.VolumeID] = req.NewVol

	glog.Infoln("updateVolume done", req.NewVol, "requuid", requuid)
	return nil
}

func (r *volumeReadWriter) deleteVolume(ctx context.Context, key *pb.VolumeKey) error {
	requuid := utils.GetReqIDFromContext(ctx)

	if key.ServiceUUID != r.serviceUUID {
		glog.Errorln("deleteVolume", key, "not belong to service", r.serviceUUID)
		return db.ErrDBInvalidRequest
	}

	currvol, ok := r.vols[key.VolumeID]
	if !ok {
		glog.Errorln("deleteVolume vol not exist", key, "requuid", requuid)
		return db.ErrDBRecordNotFound
	}

	filepath := path.Join(r.volDir, key.VolumeID)
	err := os.Remove(filepath)
	if err != nil {
		glog.Errorln("delete volume file error", err, filepath, "requuid", requuid)
		return db.ErrDBInternal
	}

	delete(r.vols, key.VolumeID)

	glog.Infoln("deleteVolume done", currvol, "requuid", requuid)
	return nil
}

func (r *volumeReadWriter) listVolumes(req *pb.ListVolumeRequest) ([]*pb.Volume, error) {
	if req.ServiceUUID != r.serviceUUID {
		glog.Errorln("listVolume", req, "not belong to service", r.serviceUUID)
		return nil, db.ErrDBInvalidRequest
	}

	vols := make([]*pb.Volume, len(r.vols))
	i := 0
	for _, vol := range r.vols {
		vols[i] = vol
		i++
	}
	glog.Infoln("list", len(vols), "volumes, service", req.ServiceUUID)
	return vols, nil
}
