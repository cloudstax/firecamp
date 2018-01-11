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

const (
	serviceLabelName        = "app"
	initContainerNamePrefix = "init-"
	dataVolumeName          = "data"
	journalVolumeName       = "journal"
	pvName                  = "pv"
	pvcName                 = "pvc"
	awsStorageProvisioner   = "kubernetes.io/aws-ebs"
)

// K8sSvc implements the containersvc interface for kubernetes.
// https://kubernetes.io/docs/tasks/access-application-cluster/access-cluster
type K8sSvc struct {
	cliset        *kubernetes.Clientset
	namespace     string
	provisioner   string
	cloudPlatform string
	dbType        string

	// whether the init container works on the test mode
	testMode bool
}

// NewK8sSvc creates a new K8sSvc instance.
// TODO support different namespaces for different services? Wait for the real requirement.
func NewK8sSvc(cloudPlatform string, dbType string, namespace string) (*K8sSvc, error) {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Errorln("rest.InClusterConfig error", err)
		return nil, err
	}
	return newK8sSvcWithConfig(cloudPlatform, dbType, namespace, config)
}

// NewTestK8sSvc creates a new K8sSvc instance for test.
func NewTestK8sSvc(cloudPlatform string, namespace string, config *rest.Config) (*K8sSvc, error) {
	svc, err := newK8sSvcWithConfig(cloudPlatform, common.DBTypeMemDB, namespace, config)
	if err != nil {
		return svc, err
	}
	svc.testMode = true
	return svc, err
}

// newK8sSvcWithConfig creates a new K8sSvc instance with the config.
func newK8sSvcWithConfig(cloudPlatform string, dbType string, namespace string, config *rest.Config) (*K8sSvc, error) {
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
		provisioner:   provisioner,
		cloudPlatform: cloudPlatform,
		dbType:        dbType,
	}
	return svc, nil
}

// GetContainerSvcType gets the containersvc type.
func (s *K8sSvc) GetContainerSvcType() string {
	return common.ContainerPlatformK8s
}

// CreateServiceVolume creates PV and PVC for the service member.
func (s *K8sSvc) CreateServiceVolume(ctx context.Context, service string, memberIndex int64, volumeID string, volumeSizeGB int64, journal bool) (existingVolumeID string, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	pvname := s.genDataVolumePVName(service, memberIndex)
	pvcname := s.genDataVolumePVCName(service, memberIndex)
	sclassname := s.genDataVolumeStorageClassName(service)
	if journal {
		pvname = s.genJournalVolumePVName(service, memberIndex)
		pvcname = s.genJournalVolumePVCName(service, memberIndex)
		sclassname = s.genJournalVolumeStorageClassName(service)
	}

	// create pvc
	err = s.createPVC(service, pvcname, pvname, sclassname, volumeSizeGB, requuid)
	if err != nil {
		glog.Errorln("create pvc error", err, pvcname, "requuid", requuid)
		return "", err
	}

	// create pv
	volID, err := s.createPV(service, pvname, sclassname, volumeID, volumeSizeGB, requuid)
	if err != nil {
		if err != containersvc.ErrVolumeExist {
			glog.Errorln("createPV error", err, "volume", volumeID, "pvname", pvname, "requuid", requuid)
			return "", err
		}
		glog.Infoln("pv exist", pvname, "existing volumeID", volID, "new volumeID", volumeID, "requuid", requuid)
	} else {
		glog.Infoln("created pv", pvname, "volume", volumeID, "created volID", volID, "requuid", requuid)
	}

	return volID, err
}

// DeleteServiceVolume deletes the pv and pvc for the service member.
func (s *K8sSvc) DeleteServiceVolume(ctx context.Context, service string, memberIndex int64, journal bool) error {
	requuid := utils.GetReqIDFromContext(ctx)

	pvname := s.genDataVolumePVName(service, memberIndex)
	pvcname := s.genDataVolumePVCName(service, memberIndex)
	if journal {
		pvname = s.genJournalVolumePVName(service, memberIndex)
		pvcname = s.genJournalVolumePVCName(service, memberIndex)
	}

	err := s.deletePVC(pvcname, requuid)
	if err != nil {
		return err
	}

	return s.deletePV(pvname, requuid)
}

// createPV creates a PersistentVolume.
func (s *K8sSvc) createPV(service string, pvname string, sclassname string, volID string, volSizeGB int64, requuid string) (existingVolID string, err error) {
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
					FSType:   common.DefaultFSType,
				},
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
		},
	}

	_, err = s.cliset.CoreV1().PersistentVolumes().Create(pv)
	if err != nil {
		if !k8errors.IsAlreadyExists(err) {
			glog.Errorln("create PersistentVolume error", err, "pvname", pvname, "requuid", requuid)
			return "", err
		}

		currpv, err := s.cliset.CoreV1().PersistentVolumes().Get(pvname, metav1.GetOptions{})
		if err != nil {
			glog.Errorln("get PersistentVolume error", err, pvname, "requuid", requuid)
			return "", err
		}

		glog.Infoln("PersistentVolume exists", pvname, "requuid", requuid, currpv.Spec)

		awsBlockStore := currpv.Spec.PersistentVolumeSource.AWSElasticBlockStore

		// check if the existing PersistentVolume is the same
		if currpv.Name != pvname ||
			currpv.Spec.Capacity.StorageEphemeral().Cmp(*pv.Spec.Capacity.StorageEphemeral()) != 0 ||
			currpv.Spec.StorageClassName != pv.Spec.StorageClassName ||
			awsBlockStore.FSType != common.DefaultFSType {
			glog.Errorln("creating PersistentVolume is not the same with existing volume", currpv.Name, currpv.Spec.Capacity,
				currpv.Spec.StorageClassName, awsBlockStore, "creating volume", pvname, volSizeGB, sclassname, volID, "requuid", requuid)
			return "", errors.New("persistent volume exists with different attributes")
		}

		if awsBlockStore.VolumeID != volID {
			glog.Errorln("pv exists with a different volume id", awsBlockStore.VolumeID,
				"new volume id", volID, "pvname", pvname, "requuid", requuid)
			return awsBlockStore.VolumeID, containersvc.ErrVolumeExist
		}
	}

	glog.Infoln("created PersistentVolume", pvname, volID, volSizeGB, "requuid", requuid)
	return volID, nil
}

func (s *K8sSvc) deletePV(pvname string, requuid string) error {
	err := s.cliset.CoreV1().PersistentVolumes().Delete(pvname, &metav1.DeleteOptions{})
	if err != nil {
		if !k8errors.IsNotFound(err) {
			glog.Errorln("delete PersistentVolume error", err, pvname, "requuid", requuid)
			return err
		}

		glog.Infoln("PersistentVolume is already deleted", pvname, "requuid", requuid)
	} else {
		glog.Infoln("deleted PersistentVolume", pvname, "requuid", requuid)
	}
	return nil
}

func (s *K8sSvc) createPVC(service string, pvcname string, pvname string, sclassname string, volSizeGB int64, requuid string) error {
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

	_, err := s.cliset.CoreV1().PersistentVolumeClaims(s.namespace).Create(pvc)
	if err != nil {
		if !k8errors.IsAlreadyExists(err) {
			glog.Errorln("create PersistentVolumeClaim error", err, pvcname, "requuid", requuid)
			return err
		}

		currpvc, err := s.cliset.CoreV1().PersistentVolumeClaims(s.namespace).Get(pvcname, metav1.GetOptions{})
		if err != nil {
			glog.Errorln("get PersistentVolumeClaim error", err, pvcname, "requuid", requuid)
			return err
		}

		glog.Infoln("PersistentVolumeClaim exists", pvcname, "requuid", requuid, currpvc.Spec)

		// check if the existing PersistentVolumeClaim is the same
		if currpvc.Name != pvcname ||
			currpvc.Spec.Resources.Requests.StorageEphemeral().Cmp(*pvc.Spec.Resources.Requests.StorageEphemeral()) != 0 ||
			(currpvc.Spec.StorageClassName == nil || *(currpvc.Spec.StorageClassName) != sclassname) ||
			currpvc.Spec.VolumeName != pvname {
			glog.Errorln("creating PersistentVolumeClaim is not the same with existing claim", currpvc.Name, currpvc.Spec.Resources.Requests.StorageEphemeral(), currpvc.Spec.StorageClassName, currpvc.Spec.VolumeName, "creating claim", pvcname, volSizeGB, sclassname, pvname)
			return errors.New("PersistentVolumeClaim exists with different attributes")
		}
	}

	glog.Infoln("created PersistentVolumeClaim", pvcname, volSizeGB, "requuid", requuid)
	return nil
}

func (s *K8sSvc) deletePVC(pvcname string, requuid string) error {
	err := s.cliset.CoreV1().PersistentVolumeClaims(s.namespace).Delete(pvcname, &metav1.DeleteOptions{})
	if err != nil {
		if !k8errors.IsNotFound(err) {
			glog.Errorln("delete PersistentVolumeClaim error", err, pvcname, "requuid", requuid)
			return err
		}
		glog.Infoln("PersistentVolumeClaim is already deleted", pvcname, "requuid", requuid)
	} else {
		glog.Infoln("deleted PersistentVolumeClaim", pvcname, "requuid", requuid)
	}
	return nil
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
	ports := []corev1.ServicePort{}
	for _, m := range opts.PortMappings {
		if m.IsServicePort {
			ports = append(ports, corev1.ServicePort{Port: int32(m.ContainerPort)})
		}
	}
	if len(ports) == 0 {
		glog.Errorln("headless service of statefulset does not have the listening port, requuid", requuid, opts)
		return common.ErrInternal
	}

	// create the headless service
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.Common.ServiceName,
			Namespace: s.namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Ports:     ports,
			Selector:  labels,
		},
	}

	_, err := s.cliset.CoreV1().Services(s.namespace).Create(svc)
	return err
}

func (s *K8sSvc) createStatefulSet(ctx context.Context, opts *containersvc.CreateServiceOptions, labels map[string]string, requuid string) error {
	// set statefulset resource limits and requests
	res := s.createResource(opts)
	glog.Infoln("create statefulset resource", res, "requuid", requuid, opts.Common)

	// set statefulset volume mounts and claims
	volMounts, volClaims := s.createVolumeMountsAndClaims(opts, labels)
	glog.Infoln("statefulset VolumeMounts", volMounts, "requuid", requuid, opts.Common)

	envs := make([]corev1.EnvVar, len(opts.Envkvs))
	for i, e := range opts.Envkvs {
		envs[i] = corev1.EnvVar{
			Name:  e.Name,
			Value: e.Value,
		}
	}

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
							Env:             envs,
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
		op := containersvc.InitContainerOpInit
		if s.testMode {
			op = containersvc.InitContainerOpTest
		}

		// expose the pod name, such as service-0, to the init container.
		// the init container could not get the ordinal from the hostname, as the HostNetwork is used.
		// https://kubernetes.io/docs/tasks/inject-data-application/environment-variable-expose-pod-information/
		podNameEnvSource := &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: "metadata.name",
			},
		}
		envs := []corev1.EnvVar{
			{Name: containersvc.EnvInitContainerOp, Value: op},
			{Name: containersvc.EnvInitContainerCluster, Value: opts.Common.Cluster},
			{Name: containersvc.EnvInitContainerServiceName, Value: opts.Common.ServiceName},
			{Name: containersvc.EnvInitContainerPodName, ValueFrom: podNameEnvSource},
			{Name: common.ENV_K8S_NAMESPACE, Value: s.namespace},
			{Name: common.ENV_DB_TYPE, Value: common.DBTypeK8sDB},
		}
		statefulset.Spec.Template.Spec.InitContainers = []corev1.Container{
			{
				Name:         initContainerNamePrefix + opts.Common.ServiceName,
				Image:        opts.KubeOptions.InitContainerImage,
				VolumeMounts: volMounts,
				Env:          envs,
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

		// use host network by default for better performance.
		// k8s requires "If this option is set, the ports that will be used must be specified."
		statefulset.Spec.Template.Spec.HostNetwork = true
	}

	_, err := s.cliset.AppsV1beta2().StatefulSets(s.namespace).Create(statefulset)
	return err
}

// CreateService creates the headless service, storage class and statefulset.
func (s *K8sSvc) CreateService(ctx context.Context, opts *containersvc.CreateServiceOptions) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// sanity check
	if opts.KubeOptions == nil {
		glog.Errorln("invalid request, CreateServiceOptions does not have KubeOptions, requuid", requuid, opts.Common)
		return common.ErrInternal
	}

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

	err := s.stopService(cluster, service, requuid)
	if err != nil {
		if k8errors.IsNotFound(err) {
			glog.Infoln("statefulset not found, service", service, "requuid", requuid)
			return nil
		}
		glog.Errorln("stopService error", err, "requuid", requuid, "service", service, "namespace", s.namespace)
		return err
	}

	// wait till all pods are stopped
	var statefulset *appsv1.StatefulSet
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

func (s *K8sSvc) stopService(cluster string, service string, requuid string) error {
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

	glog.Infoln("set statefulset replicas to 0", service, "requuid", requuid)
	return nil
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

// CreateReplicaSet creates a k8s replicaset.
// Note: currently volume is skipped for ReplicaSet.
func (s *K8sSvc) CreateReplicaSet(ctx context.Context, opts *containersvc.CreateServiceOptions) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// sanity check
	if opts.KubeOptions == nil {
		glog.Errorln("invalid request, CreateServiceOptions does not have KubeOptions, requuid", requuid, opts.Common)
		return common.ErrInternal
	}

	// set replicaset resource limits and requests
	res := s.createResource(opts)
	glog.Infoln("create replicaset resource", res, "requuid", requuid, opts.Common)

	// set env
	envs := make([]corev1.EnvVar, len(opts.Envkvs))
	for i, e := range opts.Envkvs {
		envs[i] = corev1.EnvVar{
			Name:  e.Name,
			Value: e.Value,
		}
	}

	labels := make(map[string]string)
	labels[serviceLabelName] = opts.Common.ServiceName

	replicaset := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.Common.ServiceName,
			Namespace: s.namespace,
			Labels:    labels,
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: s.int32Ptr(int32(opts.Replicas)),
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
							Resources:       res,
							ImagePullPolicy: corev1.PullAlways,
							Env:             envs,
						},
					},
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			},
		},
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

		replicaset.Spec.Template.Spec.Containers[0].Ports = ports

		// use host network by default for better performance.
		// k8s requires "If this option is set, the ports that will be used must be specified."
		replicaset.Spec.Template.Spec.HostNetwork = true
	}

	_, err := s.cliset.AppsV1beta2().ReplicaSets(s.namespace).Create(replicaset)
	return err
}

// DeleteReplicaSet deletes a k8s replicaset.
func (s *K8sSvc) DeleteReplicaSet(ctx context.Context, service string) error {
	return s.cliset.AppsV1beta2().ReplicaSets(s.namespace).Delete(service, &metav1.DeleteOptions{})
}

func (s *K8sSvc) createResource(opts *containersvc.CreateServiceOptions) corev1.ResourceRequirements {
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
	return res
}

func (s *K8sSvc) createVolumeMountsAndClaims(opts *containersvc.CreateServiceOptions, labels map[string]string) ([]corev1.VolumeMount, []corev1.PersistentVolumeClaim) {
	volMounts := []corev1.VolumeMount{}
	volClaims := []corev1.PersistentVolumeClaim{}

	if opts.DataVolume != nil {
		scname := s.genDataVolumeStorageClassName(opts.Common.ServiceName)
		dataVolume, dataVolClaim := s.createVolumeAndClaim(opts.DataVolume, scname, labels)
		volMounts = append(volMounts, *dataVolume)
		volClaims = append(volClaims, *dataVolClaim)
	}
	if opts.JournalVolume != nil {
		scname := s.genJournalVolumeStorageClassName(opts.Common.ServiceName)
		journalVolume, journalVolClaim := s.createVolumeAndClaim(opts.JournalVolume, scname, labels)
		volMounts = append(volMounts, *journalVolume)
		volClaims = append(volClaims, *journalVolClaim)
	}
	return volMounts, volClaims
}

func (s *K8sSvc) createVolumeAndClaim(volOpts *containersvc.VolumeOptions, scname string, labels map[string]string) (*corev1.VolumeMount, *corev1.PersistentVolumeClaim) {
	vol := &corev1.VolumeMount{
		Name:      scname,
		MountPath: volOpts.MountPath,
	}
	volClaim := &corev1.PersistentVolumeClaim{
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
					corev1.ResourceStorage: *resource.NewQuantity(volOpts.SizeGB*1024*1024*1024, resource.BinarySI),
				},
			},
			StorageClassName: &scname,
		},
	}
	return vol, volClaim
}

func (s *K8sSvc) int32Ptr(i int32) *int32 {
	return &i
}

func (s *K8sSvc) genDataVolumeStorageClassName(service string) string {
	return fmt.Sprintf("%s-%s", service, dataVolumeName)
}

func (s *K8sSvc) genJournalVolumeStorageClassName(service string) string {
	return fmt.Sprintf("%s-%s", service, journalVolumeName)
}

func (s *K8sSvc) genDataVolumePVName(service string, memberIndex int64) string {
	// example: service-data-pv-0
	return fmt.Sprintf("%s-%s-%s-%d", service, dataVolumeName, pvName, memberIndex)
}

func (s *K8sSvc) genJournalVolumePVName(service string, memberIndex int64) string {
	// example: service-journal-pv-0
	return fmt.Sprintf("%s-%s-%s-%d", service, journalVolumeName, pvName, memberIndex)
}

func (s *K8sSvc) genDataVolumePVCName(service string, memberIndex int64) string {
	// example: service-data-service-0.
	// Note: this format could not be changed. this is the default k8s format.
	// statefulset relies on this name to select the pvc.
	return fmt.Sprintf("%s-%s-%s-%d", service, dataVolumeName, service, memberIndex)
}

func (s *K8sSvc) genJournalVolumePVCName(service string, memberIndex int64) string {
	// example: service-journal-service-0
	// Note: this format could not be changed. this is the default k8s format.
	// statefulset relies on this name to select the pvc.
	return fmt.Sprintf("%s-%s-%s-%d", service, journalVolumeName, service, memberIndex)
}
