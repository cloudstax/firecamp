package serviceop

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/cloudstax/openmanage/common"
	"github.com/cloudstax/openmanage/manage"
	"github.com/cloudstax/openmanage/manage/client"
	"github.com/cloudstax/openmanage/utils"
)

type ServiceOp struct {
	region       string
	mgtserverurl string
	cli          *client.ManageClient
}

func NewServiceOp(region string, mgtserverurl string, tlsEnabled bool, caFile string, certFile string, keyFile string) (*ServiceOp, error) {
	var cli *client.ManageClient
	if tlsEnabled {
		// TODO how to pass the ca/cert/key files to container? one option is: store them in S3.
		if caFile == "" || certFile == "" || keyFile == "" {
			errmsg := fmt.Sprintf("tlsEnabled without ca file %s cert file %s or key file %s", caFile, certFile, keyFile)
			glog.Errorln(errmsg)
			return nil, errors.New(errmsg)
		}

		tlsConf, err := utils.GenClientTLSConfig(caFile, certFile, keyFile)
		if err != nil {
			glog.Errorln("GenClientTLSConfig error", err, "ca file", caFile, "cert file", certFile, "key file", keyFile)
			return nil, err
		}

		cli = client.NewManageClient(mgtserverurl, tlsConf)
	} else {
		cli = client.NewManageClient(mgtserverurl, nil)
	}

	s := &ServiceOp{
		region:       region,
		mgtserverurl: mgtserverurl,
		cli:          cli,
	}

	return s, nil
}

func (s *ServiceOp) CreateService(ctx context.Context, r *manage.CreateServiceRequest, maxWaitSeconds int64) error {
	err := s.cli.CreateService(ctx, r)
	if err != nil {
		glog.Errorln("CreateService error", err, r.Service)
		return err
	}

	fmt.Println("service created. wait for all containers running")

	for sec := int64(0); sec < maxWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		status, err := s.cli.GetServiceStatus(ctx, r.Service)
		if err != nil {
			// The service is successfully created. It may be possible there are some
			// temporary error, such as network error. For example, ECS may return MISSING
			// for the GET right after the service creation.
			// Here just log the GetServiceStatus error and retry.
			glog.Errorln("GetServiceStatus error", err, r.Service)
		} else {
			if status.RunningCount == status.DesiredCount {
				fmt.Println("All service containers are running")
				return nil
			}
		}

		time.Sleep(time.Duration(common.DefaultRetryWaitSeconds) * time.Second)
	}

	glog.Errorln("not all service containers are running after", maxWaitSeconds)
	return common.ErrTimeout
}

func (s *ServiceOp) InitializeService(ctx context.Context, req *manage.RunTaskRequest) error {
	// check if service is initialized.
	r := &manage.ServiceCommonRequest{
		Region:      s.region,
		Cluster:     req.Service.Cluster,
		ServiceName: req.Service.ServiceName,
	}

	initialized, err := s.cli.IsServiceInitialized(ctx, r)
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
	defer s.cli.DeleteTask(ctx, delReq)

	getReq := &manage.GetTaskStatusRequest{
		Service: req.Service,
		TaskID:  "",
	}

	// run the init task
	for i := 0; i < common.DefaultTaskRetryCounts; i++ {
		taskID, err := s.cli.RunTask(ctx, req)
		if err != nil {
			glog.Errorln("run init task error", err, req.Service)
			return err
		}

		// wait init task done
		getReq.TaskID = taskID
		s.waitTask(ctx, getReq)

		// check if service is initialized
		initialized, err := s.cli.IsServiceInitialized(ctx, req.Service)
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

func (s *ServiceOp) waitTask(ctx context.Context, r *manage.GetTaskStatusRequest) {
	glog.Infoln("wait the running task", r.TaskID, r.Service)

	for sec := int64(0); sec < common.DefaultTaskWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		// wait some time for the task to run
		time.Sleep(time.Duration(common.DefaultRetryWaitSeconds) * time.Second)

		taskStatus, err := s.cli.GetTaskStatus(ctx, r)
		if err != nil {
			// wait task error, but there is no way to know whether the task finishes its work or not.
			// go ahead to check service status
			glog.Errorln("wait init task complete error", err, "taskID", r.TaskID, "taskStatus", taskStatus, r.Service)
		} else {
			// check if task completes
			glog.Infoln("task status", taskStatus)
			if taskStatus.Status == "STOPPED" {
				return
			}
		}
	}
}

func (s *ServiceOp) SetServiceInitialized(ctx context.Context, r *manage.ServiceCommonRequest) error {
	return s.cli.SetServiceInitialized(ctx, r)
}

func (s *ServiceOp) ListServiceVolumes(ctx context.Context, cluster string, service string) (volIDs []string, err error) {
	// list all volumes
	serviceReq := &manage.ServiceCommonRequest{
		Region:      s.region,
		Cluster:     cluster,
		ServiceName: service,
	}

	listReq := &manage.ListVolumeRequest{
		Service: serviceReq,
	}

	vols, err := s.cli.ListVolume(ctx, listReq)
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

func (s *ServiceOp) DeleteService(ctx context.Context, cluster string, service string) error {
	// delete the service from the control plane.
	serviceReq := &manage.ServiceCommonRequest{
		Region:      s.region,
		Cluster:     cluster,
		ServiceName: service,
	}

	err := s.cli.DeleteService(ctx, serviceReq)
	if err != nil {
		glog.Errorln("DeleteService error", err, serviceReq)
		return err
	}

	glog.Infoln("deleted service", service, "cluster", cluster)
	return nil
}
