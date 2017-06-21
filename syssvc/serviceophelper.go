package serviceophelper

import (
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/manage"
	"github.com/cloudstax/openmanage/manage/client"
)

func WaitServiceRunning(ctx context.Context, cli *client.ManageClient, r *manage.ServiceCommonRequest) error {
	sleepSeconds := time.Duration(common.DefaultRetryWaitSeconds) * time.Second
	for sec := int64(0); sec < common.DefaultServiceWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		status, err := cli.GetServiceStatus(ctx, r)
		if err != nil {
			// The service is successfully created. It may be possible there are some
			// temporary error, such as network error. For example, ECS may return MISSING
			// for the GET right after the service creation.
			// Here just log the GetServiceStatus error and retry.
			glog.Errorln("GetServiceStatus error", err, r)
		} else {
			if status.RunningCount == status.DesiredCount {
				glog.Infoln("All service containers are running")
				return nil
			}
		}

		time.Sleep(sleepSeconds)
	}

	glog.Errorln("not all service containers are running after", common.DefaultServiceWaitSeconds)
	return common.ErrTimeout
}

func InitializeService(ctx context.Context, cli *client.ManageClient, region string, req *manage.RunTaskRequest) error {
	// check if service is initialized.
	r := &manage.ServiceCommonRequest{
		Region:      region,
		Cluster:     req.Service.Cluster,
		ServiceName: req.Service.ServiceName,
	}

	initialized, err := cli.IsServiceInitialized(ctx, r)
	if err != nil {
		glog.Errorln("check service initialized error", err, r)
		return err
	}
	if initialized {
		glog.Infoln("service is initialized", r)
		return nil
	}

	delReq := &manage.DeleteTaskRequest{
		Service:  req.Service,
		TaskType: req.TaskType,
	}
	defer cli.DeleteTask(ctx, delReq)

	getReq := &manage.GetTaskStatusRequest{
		Service: req.Service,
		TaskID:  "",
	}

	// run the init task
	for i := 0; i < common.DefaultTaskRetryCounts; i++ {
		taskID, err := cli.RunTask(ctx, req)
		if err != nil {
			glog.Errorln("run init task error", err, req.Service)
			return err
		}

		// wait init task done
		getReq.TaskID = taskID
		waitTask(ctx, cli, getReq)

		// check if service is initialized
		initialized, err := cli.IsServiceInitialized(ctx, req.Service)
		if err != nil {
			glog.Errorln("check service initialized error", err, req.Service)
		} else {
			if initialized {
				glog.Infoln("cluster", req.Service.Cluster, "service", req.Service.ServiceName, "is initialized")
				return nil
			}
			glog.Errorln("cluster", req.Service.Cluster, "service", req.Service.ServiceName, "is not initialized")
		}
	}

	glog.Errorln("cluster", req.Service.Cluster, "service", req.Service.ServiceName, "is not initialized after retry")
	return common.ErrTimeout
}

func waitTask(ctx context.Context, cli *client.ManageClient, r *manage.GetTaskStatusRequest) {
	glog.Infoln("wait the running task", r.TaskID, r.Service)

	sleepSeconds := time.Duration(common.DefaultRetryWaitSeconds) * time.Second
	for sec := int64(0); sec < common.DefaultTaskWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		// wait some time for the task to run
		time.Sleep(sleepSeconds)

		taskStatus, err := cli.GetTaskStatus(ctx, r)
		if err != nil {
			// wait task error, but there is no way to know whether the task finishes its work or not.
			// go ahead to check service status
			glog.Errorln("wait init task complete error", err, "taskID", r.TaskID, "taskStatus", taskStatus, r.Service)
		} else {
			// check if task completes
			glog.Infoln("task status", taskStatus)
			if taskStatus.Status == common.TaskStatusStopped {
				return
			}
		}
	}
}

func ListServiceVolumes(ctx context.Context, cli *client.ManageClient,
	region string, cluster string, service string) (volIDs []string, err error) {
	// list all volumes
	serviceReq := &manage.ServiceCommonRequest{
		Region:      region,
		Cluster:     cluster,
		ServiceName: service,
	}

	listReq := &manage.ListVolumeRequest{
		Service: serviceReq,
	}

	vols, err := cli.ListVolume(ctx, listReq)
	if err != nil {
		glog.Errorln("ListVolume error", err, serviceReq)
		return nil, err
	}

	volIDs = make([]string, len(vols))
	for i, vol := range vols {
		volIDs[i] = vol.VolumeID
	}

	glog.Infoln("get", len(vols), "volumes for service", service)
	return volIDs, nil
}

func DeleteService(ctx context.Context, cli *client.ManageClient, region string, cluster string, service string) error {
	// delete the service from the control plane.
	serviceReq := &manage.ServiceCommonRequest{
		Region:      region,
		Cluster:     cluster,
		ServiceName: service,
	}

	err := cli.DeleteService(ctx, serviceReq)
	if err != nil {
		glog.Errorln("DeleteService error", err, serviceReq)
		return err
	}

	glog.Infoln("deleted service", service, "cluster", cluster)
	return nil
}
