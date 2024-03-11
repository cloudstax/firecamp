package managesvc

import (
	"encoding/json"
	"net/http"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/jazzl0ver/firecamp/api/manage"
)

func (s *ManageHTTPServer) internalGetOp(ctx context.Context, w http.ResponseWriter,
	r *http.Request, trimURL string, requuid string) (errmsg string, errcode int) {
	switch trimURL {
	case manage.InternalGetServiceTaskOp:
		return s.internalGetServiceTask(ctx, w, r, requuid)
	case manage.InternalListActiveServiceTasksOp:
		return s.internalListActiveServiceTasks(ctx, w, r, requuid)
	default:
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}
}

func (s *ManageHTTPServer) internalGetServiceTask(ctx context.Context, w http.ResponseWriter,
	r *http.Request, requuid string) (errmsg string, errcode int) {
	req := &manage.InternalGetServiceTaskRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("InternalGetServiceTask decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Cluster != s.cluster || req.Region != s.region {
		glog.Errorln("InternalGetServiceTask invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	taskID, err := s.containersvcIns.GetServiceTask(ctx, req.Cluster, req.ServiceName, req.ContainerInstanceID)
	if err != nil {
		glog.Errorln("GetServiceTask error", err, "requuid", requuid, req)
		return convertToHTTPError(err)
	}

	glog.Infoln("GetServiceTask", taskID, "requuid", requuid, req)

	resp := &manage.InternalGetServiceTaskResponse{
		ServiceTaskID: taskID,
	}
	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal InternalGetServiceTaskResponse error", err, "requuid", requuid, req)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}

func (s *ManageHTTPServer) internalListActiveServiceTasks(ctx context.Context, w http.ResponseWriter,
	r *http.Request, requuid string) (errmsg string, errcode int) {
	req := &manage.InternalListActiveServiceTasksRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		glog.Errorln("InternalListActiveServiceTasks decode request error", err, "requuid", requuid)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	if req.Cluster != s.cluster || req.Region != s.region {
		glog.Errorln("InternalListActiveServiceTasks invalid request, local cluster", s.cluster,
			"region", s.region, "requuid", requuid, req)
		return http.StatusText(http.StatusBadRequest), http.StatusBadRequest
	}

	taskIDs, err := s.containersvcIns.ListActiveServiceTasks(ctx, req.Cluster, req.ServiceName)
	if err != nil {
		glog.Errorln("ListActiveServiceTasks error", err, "requuid", requuid, req)
		return convertToHTTPError(err)
	}

	glog.Infoln("ListActiveServiceTasks", len(taskIDs), "requuid", requuid, req)

	resp := &manage.InternalListActiveServiceTasksResponse{
		ServiceTaskIDs: taskIDs,
	}
	b, err := json.Marshal(resp)
	if err != nil {
		glog.Errorln("Marshal InternalListActiveServiceTasksResponse error", err, "requuid", requuid, req)
		return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
	}

	w.WriteHeader(http.StatusOK)
	w.Write(b)

	return "", http.StatusOK
}
