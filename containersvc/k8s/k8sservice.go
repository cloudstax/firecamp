package k8ssvc

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	appsv1 "k8s.io/api/apps/v1beta2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/utils"
)

var (
	// ErrVolumeExist means PersistentVolume exists
	ErrVolumeExist = errors.New("PersistentVolume Exists")
	// ErrVolumeClaimExist means PersistentVolumeClaim exists
	ErrVolumeClaimExist = errors.New("PersistentVolumeClaim Exists")
)

const (
	serviceLabelName        = "app"
	initContainerNamePrefix = "init-"
	dataVolumeNameSuffix    = "-data"
	journalVolumeNameSuffix = "-journal"
	awsStorageProvisioner   = "kubernetes.io/aws-ebs"
)

// K8sSvc implements the containersvc interface for kubernetes.
// https://kubernetes.io/docs/tasks/access-application-cluster/access-cluster
type K8sSvc struct {
	cliset        *kubernetes.Clientset
	namespace     string
	cloudPlatform string
	provisioner   string
}

// NewK8sSvc creates a new K8sSvc instance.
func NewK8sSvc(cloudPlatform string, namespace string) (*K8sSvc, error) {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Errorln("rest.InClusterConfig error", err)
		return nil, err
	}
	return NewK8sSvcWithConfig(cloudPlatform, namespace, config)
}

// NewK8sSvcWithConfig creates a new K8sSvc instance with the config.
func NewK8sSvcWithConfig(cloudPlatform string, namespace string, config *rest.Config) (*K8sSvc, error) {
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Errorln("kubernetes.NewForConfig error", err)
		return nil, err
	}

	provisioner := awsStorageProvisioner
	if cloudPlatform != common.CloudPlatformAWS {
		glog.Errorln("unsupport cloud platform", cloudPlatform)
		return nil, common.ErrNotSupported
	}

	svc := &K8sSvc{
		cliset:        clientset,
		namespace:     namespace,
		cloudPlatform: cloudPlatform,
		provisioner:   provisioner,
	}
	return svc, nil
}

// CreatePV creates a PersistentVolume.
func (s *K8sSvc) CreatePV(ctx context.Context, service string, pvname string, sclassname string, volID string, volSizeGB int64, fsType string) (*corev1.PersistentVolume, error) {
	labels := make(map[string]string)
	labels[serviceLabelName] = service

	// create one pv
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvname,
			Namespace: s.namespace,
			Labels:    labels,
		},
		Spec: corev1.PersistentVolumeSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: *resource.NewQuantity(volSizeGB*1024*1024*1024, resource.BinarySI),
			},
			StorageClassName: sclassname,
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				AWSElasticBlockStore: &corev1.AWSElasticBlockStoreVolumeSource{
					VolumeID: volID,
					FSType:   fsType,
				},
			},
		},
	}

	currpv, err := s.cliset.CoreV1().PersistentVolumes().Create(pv)
	if err != nil {
		if !k8errors.IsAlreadyExists(err) {
			glog.Errorln("create PersistentVolume error", err, "service", service, "pvname", pvname)
			return nil, err
		}

		glog.Infoln("PersistentVolume exists", pvname, "service", service)

		// check if the existing PersistentVolume is the same
		if currpv.Spec.Capacity.StorageEphemeral().Cmp(*pv.Spec.Capacity.StorageEphemeral()) != 0 ||
			currpv.Spec.StorageClassName != pv.Spec.StorageClassName ||
			currpv.Spec.PersistentVolumeSource.AWSElasticBlockStore.VolumeID != volID ||
			currpv.Spec.PersistentVolumeSource.AWSElasticBlockStore.FSType != fsType {
			glog.Errorln("creating PersistentVolume is not the same with existing volume", currpv.Spec.Capacity, currpv.Spec.StorageClassName, currpv.Spec.PersistentVolumeSource.AWSElasticBlockStore, "creating volume", volSizeGB, sclassname, volID, fsType)
			return nil, ErrVolumeExist
		}
	}

	glog.Infoln("created PersistentVolume for service", service, pvname, sclassname, volID, volSizeGB, fsType)
	return currpv, nil
}

// CreatePVC creates a PersistentVolumeClaim.
func (s *K8sSvc) CreatePVC(ctx context.Context, service string, pvcname string, pvname string, sclassname string, volSizeGB int64) (*corev1.PersistentVolumeClaim, error) {
	labels := make(map[string]string)
	labels[serviceLabelName] = service

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcname,
			Namespace: s.namespace,
			Labels:    labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: *resource.NewQuantity(volSizeGB*1024*1024*1024, resource.BinarySI),
				},
			},
			StorageClassName: &sclassname,
			VolumeName:       pvname,
		},
	}

	currpvc, err := s.cliset.CoreV1().PersistentVolumeClaims(s.namespace).Create(pvc)
	if err != nil {
		if !k8errors.IsAlreadyExists(err) {
			glog.Errorln("create PersistentVolumeClaim error", err, "service", service, "pvcname", pvcname)
			return nil, err
		}

		glog.Infoln("PersistentVolumeClaim exists", pvcname, "service", service)

		// check if the existing PersistentVolumeClaim is the same
		if currpvc.Spec.Resources.Requests.StorageEphemeral().Cmp(*pvc.Spec.Resources.Requests.StorageEphemeral()) != 0 ||
			*(currpvc.Spec.StorageClassName) != sclassname ||
			currpvc.Spec.VolumeName != pvname {
			glog.Errorln("creating PersistentVolumeClaim is not the same with existing claim", currpvc.Spec.Resources.Requests.StorageEphemeral(), currpvc.Spec.StorageClassName, currpvc.Spec.VolumeName, "creating claim", volSizeGB, sclassname, pvname)
			return nil, ErrVolumeClaimExist
		}
	}

	glog.Infoln("created PersistentVolumeClaim for service", service, pvname, sclassname, volSizeGB)
	return currpvc, nil

}

func (s *K8sSvc) createStorageClass(ctx context.Context, opts *containersvc.CreateServiceOptions, requuid string) error {
	scname := s.genDataVolumeStorageClassName(opts.Common.ServiceName)
	err := s.createVolumeStorageClass(ctx, scname, opts.DataVolume)
	if err != nil {
		if !k8errors.IsAlreadyExists(err) {
			glog.Errorln("create data volume storage class error", err, "requuid", requuid, opts.Common)
			return err
		}
		glog.Infoln("data volume storage class already exists, requuid", requuid, opts.Common)
	} else {
		glog.Infoln("created data volume storage class, requuid", requuid, opts.Common)
	}

	if opts.JournalVolume != nil {
		scname = s.genJournalVolumeStorageClassName(opts.Common.ServiceName)
		err = s.createVolumeStorageClass(ctx, scname, opts.JournalVolume)
		if err != nil {
			if !k8errors.IsAlreadyExists(err) {
				glog.Errorln("create journal volume storage class error", err, "requuid", requuid, opts.Common)
				return err
			}
			glog.Infoln("journal volume storage class already exists, requuid", requuid, opts.Common)
		} else {
			glog.Infoln("created journal volume storage class, requuid", requuid, opts.Common)
		}
	}
	return nil
}

func (s *K8sSvc) createVolumeStorageClass(ctx context.Context, scname string, vol *containersvc.VolumeOptions) error {
	params := make(map[string]string)
	params["type"] = vol.VolumeType
	if vol.VolumeType == common.VolumeTypeIOPSSSD {
		params["iopsPerGB"] = strconv.FormatInt(vol.Iops, 10)
	}

	sc := &storagev1.StorageClass{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StorageClass",
			APIVersion: "storage.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      scname,
			Namespace: s.namespace,
		},
		Provisioner: s.provisioner,
		Parameters:  params,
	}

	_, err := s.cliset.StorageV1().StorageClasses().Create(sc)
	return err
}

func (s *K8sSvc) deleteStorageClass(ctx context.Context, service string, requuid string) error {
	scname := s.genDataVolumeStorageClassName(service)
	err := s.cliset.StorageV1().StorageClasses().Delete(scname, &metav1.DeleteOptions{})
	if err != nil {
		if !k8errors.IsNotFound(err) {
			glog.Errorln("delete data volume storage class error", err, "service", service, "requuid", requuid)
			return err
		}
		glog.Infoln("data volume storage class not exists, service", service, "requuid", requuid)
	} else {
		glog.Infoln("deleted data volume storage class, service", service, "requuid", requuid)
	}

	scname = s.genJournalVolumeStorageClassName(service)
	err = s.cliset.StorageV1().StorageClasses().Delete(scname, &metav1.DeleteOptions{})
	if err != nil {
		if !k8errors.IsNotFound(err) {
			glog.Errorln("delete journal volume storage class error", err, "service", service, "requuid", requuid)
			return err
		}
		glog.Infoln("journal volume storage class not exists, service", service, "requuid", requuid)
	} else {
		glog.Infoln("deleted journal volume storage class, service", service, "requuid", requuid)
	}
	return nil
}

// IsServiceExist checks if service exists. If not exist, return false & nil. If exists, return true & nil.
// If meets any error, error will be returned.
func (s *K8sSvc) IsServiceExist(ctx context.Context, cluster string, service string) (bool, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	statefulset, err := s.cliset.AppsV1beta2().StatefulSets(s.namespace).Get(service, metav1.GetOptions{})
	if err != nil {
		if k8errors.IsNotFound(err) {
			glog.Infoln("statefulset not exist", service, "requuid", requuid)
			return false, nil
		}

		glog.Errorln("get statefulset error", service, "requuid", requuid)
		return false, err
	}

	glog.Infoln("get statefulset for service", service, "requuid", requuid, "statefulset status", statefulset.Status)
	return true, nil
}

func (s *K8sSvc) isHeadlessServiceExist(ctx context.Context, cluster string, service string) (bool, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	// k8s needs one k8s headless service and one statefulset for one service.
	k8svc, err := s.cliset.CoreV1().Services(s.namespace).Get(service, metav1.GetOptions{})
	if err != nil {
		if k8errors.IsNotFound(err) {
			glog.Infoln("headless service not exist", service, "requuid", requuid)
			return false, nil
		}

		glog.Errorln("get headless service error", err, service, "requuid", requuid)
		return false, err
	}

	glog.Infoln("get headless service", service, "status", k8svc.Status, "requuid", requuid)
	return true, nil
}

func (s *K8sSvc) createHeadlessService(ctx context.Context, opts *containersvc.CreateServiceOptions, labels map[string]string, requuid string) error {
	// create the headless service
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.Common.ServiceName,
			Namespace: s.namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Ports: []corev1.ServicePort{
				{Port: int32(opts.KubeOptions.ServicePort)},
			},
			Selector: labels,
		},
	}

	_, err := s.cliset.CoreV1().Services(s.namespace).Create(svc)
	return err
}

func (s *K8sSvc) createStatefulSet(ctx context.Context, opts *containersvc.CreateServiceOptions, labels map[string]string, requuid string) error {
	// set statefulset resource limits and requests
	var res corev1.ResourceRequirements
	if opts.Common.Resource != nil {
		if opts.Common.Resource.MaxCPUUnits != common.DefaultMaxCPUUnits || opts.Common.Resource.MaxMemMB != common.DefaultMaxMemoryMB {
			res.Limits = make(corev1.ResourceList)
			if opts.Common.Resource.MaxCPUUnits != common.DefaultMaxCPUUnits {
				res.Limits[corev1.ResourceCPU] = *resource.NewMilliQuantity(opts.Common.Resource.MaxCPUUnits, resource.BinarySI)
			}
			if opts.Common.Resource.MaxMemMB != common.DefaultMaxMemoryMB {
				res.Limits[corev1.ResourceMemory] = *resource.NewQuantity(opts.Common.Resource.MaxMemMB*1024*1024, resource.BinarySI)
			}
		}

		if opts.Common.Resource.ReserveCPUUnits != common.DefaultMaxCPUUnits || opts.Common.Resource.ReserveMemMB != common.DefaultMaxMemoryMB {
			res.Requests = make(corev1.ResourceList)
			if opts.Common.Resource.ReserveCPUUnits != common.DefaultMaxCPUUnits {
				res.Requests[corev1.ResourceCPU] = *resource.NewMilliQuantity(opts.Common.Resource.ReserveCPUUnits, resource.BinarySI)
			}
			if opts.Common.Resource.ReserveMemMB != common.DefaultMaxMemoryMB {
				res.Requests[corev1.ResourceMemory] = *resource.NewQuantity(opts.Common.Resource.ReserveMemMB*1024*1024, resource.BinarySI)
			}
		}
	}

	glog.Infoln("create statefulset resource", res, "requuid", requuid, opts.Common)

	// set statefulset volume mounts and claims
	scname := s.genDataVolumeStorageClassName(opts.Common.ServiceName)
	dataVolume := corev1.VolumeMount{
		Name:      scname,
		MountPath: opts.DataVolume.MountPath,
	}
	dataVolClaim := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scname,
			Namespace: s.namespace,
			Labels:    labels,
			//Annotations:
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: *resource.NewQuantity(opts.JournalVolume.SizeGB*1024*1024*1024, resource.BinarySI),
				},
			},
			StorageClassName: &scname,
		},
	}
	volMounts := []corev1.VolumeMount{dataVolume}
	volClaims := []corev1.PersistentVolumeClaim{dataVolClaim}
	if opts.JournalVolume != nil {
		scname := s.genJournalVolumeStorageClassName(opts.Common.ServiceName)
		journalVolume := corev1.VolumeMount{
			Name:      scname,
			MountPath: opts.JournalVolume.MountPath,
		}
		journalVolClaim := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      scname,
				Namespace: s.namespace,
				Labels:    labels,
				//Annotations:
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: *resource.NewQuantity(opts.JournalVolume.SizeGB*1024*1024*1024, resource.BinarySI),
					},
				},
				StorageClassName: &scname,
			},
		}

		volMounts = append(volMounts, journalVolume)
		volClaims = append(volClaims, journalVolClaim)
	}

	glog.Infoln("statefulset VolumeMounts", volMounts, "requuid", requuid, opts.Common)

	// The ParallelPodManagement policy is used instead of OrderedReadyPodManagement.
	// The OrderedReadyPodManagement create pods in strictly increasing order. This may introduce
	// some issue when running in cloud. For example, Cassandra service has 3 replicas on 3 AZs.
	// The replica0 is on AZ1. If AZ1 goes down, the pods for replica1 and 2 will keep waiting
	// for replica0.
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.Common.ServiceName,
			Namespace: s.namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:            s.int32Ptr(int32(opts.Replicas)),
			ServiceName:         opts.Common.ServiceName,
			PodManagementPolicy: appsv1.ParallelPodManagement,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				// TODO would be better to let the service set the update strategy.
				// For service like MongoDB that requires the specific update sequence, use OnDelete.
				// For service like Cassandra, could simply use RollingUpdate.
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: s.namespace,
					Labels:    labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            opts.Common.ServiceName,
							Image:           opts.Common.ContainerImage,
							VolumeMounts:    volMounts,
							Resources:       res,
							ImagePullPolicy: corev1.PullAlways,
						},
					},
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			},
			VolumeClaimTemplates: volClaims,
		},
	}

	// set the statefulset init container
	if len(opts.KubeOptions.InitContainerImage) != 0 {
		statefulset.Spec.Template.Spec.InitContainers = []corev1.Container{
			{
				Name:         initContainerNamePrefix + opts.Common.ServiceName,
				Image:        opts.KubeOptions.InitContainerImage,
				VolumeMounts: volMounts,
			},
		}
	}

	// set port exposing
	if len(opts.PortMappings) != 0 {
		glog.Infoln("expose port", opts.PortMappings, "requuid", requuid, opts.Common)

		ports := make([]corev1.ContainerPort, len(opts.PortMappings))
		for i, p := range opts.PortMappings {
			ports[i] = corev1.ContainerPort{
				ContainerPort: int32(p.ContainerPort),
			}
			if opts.KubeOptions.ExternalDNS {
				// TODO current needs to expose the host port for ExternalDNS, so replicas could talk with each other.
				// refactor it when using the k8s external dns project.
				ports[i].HostPort = int32(p.HostPort)
			}
		}
		statefulset.Spec.Template.Spec.Containers[0].Ports = ports
	}

	_, err := s.cliset.AppsV1beta2().StatefulSets(s.namespace).Create(statefulset)
	return err
}

// CreateService creates the headless service, storage class and statefulset.
func (s *K8sSvc) CreateService(ctx context.Context, opts *containersvc.CreateServiceOptions) error {
	requuid := utils.GetReqIDFromContext(ctx)

	labels := make(map[string]string)
	labels[serviceLabelName] = opts.Common.ServiceName

	// create the headless service
	err := s.createHeadlessService(ctx, opts, labels, requuid)
	if err != nil {
		if !k8errors.IsAlreadyExists(err) {
			glog.Errorln("create headless service error", err, "requuid", requuid, opts.Common)
			return err
		}
		glog.Infoln("the headless service already exists, requuid", requuid, opts.Common)
	} else {
		glog.Infoln("created headless service, requuid", requuid, opts.Common)
	}

	// create the storage class
	err = s.createStorageClass(ctx, opts, requuid)
	if err != nil {
		glog.Errorln("create storage class error", err, "requuid", requuid, opts.Common)
		return err
	}

	// create the statefulset
	err = s.createStatefulSet(ctx, opts, labels, requuid)
	if err != nil {
		if !k8errors.IsAlreadyExists(err) {
			glog.Errorln("create statefulset error", err, "requuid", requuid, opts.Common)
			return err
		}
		glog.Infoln("the statefulset exists, requuid", requuid, opts.Common)
	} else {
		glog.Infoln("created the statefulset, requuid", requuid, opts.Common)
	}

	return nil
}

// GetServiceStatus returns the service status.
func (s *K8sSvc) GetServiceStatus(ctx context.Context, cluster string, service string) (*common.ServiceStatus, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	statefulset, err := s.cliset.AppsV1beta2().StatefulSets(s.namespace).Get(service, metav1.GetOptions{})
	if err != nil {
		glog.Errorln("get statefulset error", err, "requuid", requuid, "service", service, "namespace", s.namespace)
		return nil, err
	}

	glog.Infoln("get statefulset for service", service, "requuid", requuid, statefulset.Status)

	status := &common.ServiceStatus{
		RunningCount: int64(statefulset.Status.ReadyReplicas),
		DesiredCount: int64(statefulset.Status.Replicas),
	}
	return status, nil
}

// StopService stops the service on the container platform, and waits till all containers are stopped.
// Expect no error (nil) if service is already stopped or does not exist.
func (s *K8sSvc) StopService(ctx context.Context, cluster string, service string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// get statefulset
	statefulset, err := s.cliset.AppsV1beta2().StatefulSets(s.namespace).Get(service, metav1.GetOptions{})
	if err != nil {
		glog.Errorln("get statefulset error", err, "requuid", requuid, "service", service, "namespace", s.namespace)
		return err
	}

	glog.Infoln("get statefulset for service", service, "requuid", requuid, statefulset.Status)

	// update statefulset Replicas to 0
	statefulset.Spec.Replicas = s.int32Ptr(0)
	_, err = s.cliset.AppsV1beta2().StatefulSets(s.namespace).Update(statefulset)
	if err != nil {
		glog.Errorln("update statefulset error", err, "requuid", requuid, "service", service, "namespace", s.namespace)
		return err
	}

	// wait till all pods are stopped
	for sec := int64(0); sec < common.DefaultServiceWaitSeconds; sec += common.DefaultRetryWaitSeconds {
		statefulset, err = s.cliset.AppsV1beta2().StatefulSets(s.namespace).Get(service, metav1.GetOptions{})
		if err != nil {
			glog.Errorln("get statefulset error", err, "requuid", requuid, "service", service, "namespace", s.namespace)
			return err
		}
		if statefulset.Status.ReadyReplicas == 0 {
			glog.Infoln("service has no running task", service, "requuid", requuid)
			return nil
		}

		glog.Infoln("service", service, "still has", statefulset.Status.ReadyReplicas,
			"running pods, requuid", requuid, statefulset.Status)

		time.Sleep(time.Duration(common.DefaultRetryWaitSeconds) * time.Second)
	}

	glog.Errorln("service", service, "still has", statefulset.Status.ReadyReplicas,
		"running pods, after", common.DefaultServiceWaitSeconds, "requuid", requuid, statefulset.Status)

	return common.ErrTimeout
}

// ScaleService scales the service containers up/down to the desiredCount. Note: it does not wait till all containers are started or stopped.
func (s *K8sSvc) ScaleService(ctx context.Context, cluster string, service string, desiredCount int64) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// get statefulset
	statefulset, err := s.cliset.AppsV1beta2().StatefulSets(s.namespace).Get(service, metav1.GetOptions{})
	if err != nil {
		glog.Errorln("get statefulset error", err, "requuid", requuid, "service", service, "namespace", s.namespace)
		return err
	}

	glog.Infoln("get statefulset for service", service, "requuid", requuid, statefulset.Status)

	// update statefulset Replicas
	statefulset.Spec.Replicas = s.int32Ptr(int32(desiredCount))
	_, err = s.cliset.AppsV1beta2().StatefulSets(s.namespace).Update(statefulset)
	if err != nil {
		glog.Errorln("update statefulset error", err, "requuid", requuid, "service", service, "namespace", s.namespace)
		return err
	}

	glog.Infoln("ScaleService complete", service, "desiredCount", desiredCount, "requuid", requuid)
	return nil
}

// DeleteService deletes the service on the container platform.
// Expect no error (nil) if service does not exist.
func (s *K8sSvc) DeleteService(ctx context.Context, cluster string, service string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	delOpt := &metav1.DeleteOptions{}

	// delete statefulset
	err := s.cliset.AppsV1beta2().StatefulSets(s.namespace).Delete(service, delOpt)
	if err != nil {
		if !k8errors.IsNotFound(err) {
			glog.Errorln("delete statefulset error", err, "service", service, "requuid", requuid)
			return err
		}
		glog.Infoln("statefulset not found, service", service, "requuid", requuid)
	} else {
		glog.Infoln("deleted statefulset, service", service, "requuid", requuid)
	}

	// delete the headless service
	err = s.cliset.CoreV1().Services(s.namespace).Delete(service, delOpt)
	if err != nil {
		if !k8errors.IsNotFound(err) {
			glog.Errorln("delete headless service error", err, "service", service, "requuid", requuid)
			return err
		}
		glog.Infoln("headless service not found, service", service, "requuid", requuid)
	} else {
		glog.Infoln("deleted headless service, service", service, "requuid", requuid)
	}

	// delete the storage class
	err = s.deleteStorageClass(ctx, service, requuid)
	if err != nil {
		return err
	}

	glog.Infoln("deleted service", service, "requuid", requuid)
	return nil
}

// ListActiveServiceTasks lists the active (pending and running) tasks of the service.
func (s *K8sSvc) ListActiveServiceTasks(ctx context.Context, cluster string, service string) (serviceTaskIDs map[string]bool, err error) {
	return nil, common.ErrNotSupported
}

// GetServiceTask gets the service task on the container instance.
func (s *K8sSvc) GetServiceTask(ctx context.Context, cluster string, service string, containerInstanceID string) (serviceTaskID string, err error) {
	return "", common.ErrNotSupported
}

// RunTask runs a task.
func (s *K8sSvc) RunTask(ctx context.Context, opts *containersvc.RunTaskOptions) (taskID string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	taskID = opts.Common.ServiceName + common.NameSeparator + opts.TaskType

	envs := make([]corev1.EnvVar, len(opts.Envkvs))
	for i, e := range opts.Envkvs {
		envs[i] = corev1.EnvVar{
			Name:  e.Name,
			Value: e.Value,
		}
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      taskID,
			Namespace: s.namespace,
		},
		Spec: batchv1.JobSpec{
			Parallelism: s.int32Ptr(1),
			Completions: s.int32Ptr(1),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskID,
					Namespace: s.namespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  taskID,
							Image: opts.Common.ContainerImage,
							Env:   envs,
						},
					},
					RestartPolicy: corev1.RestartPolicyOnFailure,
				},
			},
		},
	}

	_, err = s.cliset.BatchV1().Jobs(s.namespace).Create(job)
	if err != nil {
		if k8errors.IsAlreadyExists(err) {
			glog.Infoln("service task exist", taskID, "requuid", requuid)
			return taskID, nil
		}
		glog.Errorln("create service task error", taskID, "requuid", requuid)
		return "", err
	}

	glog.Infoln("created service task", taskID, "requuid", requuid)
	return taskID, nil
}

// GetTaskStatus gets the task status.
func (s *K8sSvc) GetTaskStatus(ctx context.Context, cluster string, taskID string) (*common.TaskStatus, error) {
	requuid := utils.GetReqIDFromContext(ctx)

	job, err := s.cliset.BatchV1().Jobs(s.namespace).Get(taskID, metav1.GetOptions{})
	if err != nil {
		glog.Errorln("get task error", err, "taskID", taskID, "requuid", requuid)
		return nil, err
	}

	glog.Infoln("get task", taskID, job.Status, "requuid", requuid)

	status := &common.TaskStatus{
		Status: common.TaskStatusRunning,
	}
	if job.Status.StartTime != nil {
		status.StartedAt = job.Status.StartTime.String()
	}
	if job.Status.CompletionTime != nil {
		status.FinishedAt = job.Status.CompletionTime.String()
	}

	if job.Status.Succeeded > 0 {
		glog.Infoln("task succeeded, taskID", taskID, "requuid", requuid)
		status.Status = common.TaskStatusStopped
		status.StoppedReason = "success"
		return status, nil
	}

	if len(job.Status.Conditions) != 0 {
		glog.Infoln("task status conditions", job.Status.Conditions[0], "taskID", taskID, "requuid", requuid)

		if job.Status.Conditions[0].Type == batchv1.JobComplete ||
			job.Status.Conditions[0].Type == batchv1.JobFailed {
			status.Status = common.TaskStatusStopped
			status.StoppedReason = job.Status.Conditions[0].Message
			return status, nil
		}
	}

	reason := fmt.Sprintf("unknown task status, actively running pods %d, failed pods %d", job.Status.Active, job.Status.Failed)
	glog.Infoln(reason, "taskID", taskID, "requuid", requuid, job.Status)
	return status, nil
}

// DeleteTask deletes the task.
func (s *K8sSvc) DeleteTask(ctx context.Context, cluster string, service string, taskType string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	taskID := service + common.NameSeparator + taskType

	err := s.cliset.BatchV1().Jobs(s.namespace).Delete(taskID, &metav1.DeleteOptions{})
	if err != nil {
		if k8errors.IsNotFound(err) {
			glog.Infoln("task not found", taskID, "requuid", requuid)
			return nil
		}
		glog.Errorln("delete task error", err, "taskID", taskID, "requuid", requuid)
		return err
	}

	glog.Infoln("deleted task", taskID, "requuid", requuid)
	return nil
}

func (s *K8sSvc) int32Ptr(i int32) *int32 {
	return &i
}

func (s *K8sSvc) genDataVolumeStorageClassName(service string) string {
	return service + dataVolumeNameSuffix
}

func (s *K8sSvc) genJournalVolumeStorageClassName(service string) string {
	return service + journalVolumeNameSuffix
}
