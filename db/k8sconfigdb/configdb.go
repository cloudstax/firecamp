package k8sconfigdb

import (
	"github.com/golang/glog"
	"golang.org/x/net/context"

	k8errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
)

const (
	labelName          = common.SystemName
	deviceLabel        = "device"
	serviceLabel       = "service"
	serviceAttrLabel   = "serviceattr"
	serviceMemberLabel = "servicemember"
	configFileLabel    = "configfile"
	staticIPLabel      = "staticip"
)

// K8sConfigDB implements DB interface using k8s configmap.
type K8sConfigDB struct {
	cliset        *kubernetes.Clientset
	namespace     string
	devLabels     map[string]string
	serviceLabels map[string]string
	attrLabels    map[string]string
}

// NewK8sConfigDB creates a K8sConfigDB instance.
func NewK8sConfigDB(namespace string) (*K8sConfigDB, error) {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Errorln("rest.InClusterConfig error", err)
		return nil, err
	}

	return NewK8sConfigDBWithConfig(namespace, config)
}

// NewK8sConfigDBWithConfig creates a K8sConfigDB instance.
func NewK8sConfigDBWithConfig(namespace string, config *rest.Config) (*K8sConfigDB, error) {
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Errorln("kubernetes.NewForConfig error", err)
		return nil, err
	}

	s := &K8sConfigDB{
		cliset:    clientset,
		namespace: namespace,
		devLabels: map[string]string{
			labelName: deviceLabel,
		},
		serviceLabels: map[string]string{
			labelName: serviceLabel,
		},
		attrLabels: map[string]string{
			labelName: serviceAttrLabel,
		},
	}
	return s, nil
}

// CreateSystemTables is a non-op.
func (s *K8sConfigDB) CreateSystemTables(ctx context.Context) error {
	return nil
}

// SystemTablesReady is a non-op.
func (s *K8sConfigDB) SystemTablesReady(ctx context.Context) (tableStatus string, ready bool, err error) {
	return db.TableStatusActive, true, nil
}

// DeleteSystemTables is a non-op. Assume all records are already deleted.
func (s *K8sConfigDB) DeleteSystemTables(ctx context.Context) error {
	return nil
}

// convert k8s error to db error.
func (s *K8sConfigDB) convertError(err error) error {
	if k8errors.IsAlreadyExists(err) || k8errors.IsConflict(err) {
		return db.ErrDBConditionalCheckFailed
	}
	if k8errors.IsNotFound(err) {
		return db.ErrDBRecordNotFound
	}
	if k8errors.IsTooManyRequests(err) {
		return db.ErrDBLimitExceeded
	}
	if k8errors.IsBadRequest(err) {
		return db.ErrDBInvalidRequest
	}
	return db.ErrDBInternal
}
