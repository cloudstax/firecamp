package k8sconfigdb

import (
	"encoding/hex"
	"fmt"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/utils"
)

// CreateService creates a service.
func (s *K8sConfigDB) CreateService(ctx context.Context, svc *common.Service) error {
	requuid := utils.GetReqIDFromContext(ctx)

	cfgmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.genServiceConfigMapName(svc.ClusterName, svc.ServiceName),
			Namespace: s.namespace,
			Labels:    s.serviceLabels,
		},
		Data: map[string]string{
			db.ClusterName: svc.ClusterName,
			db.ServiceName: svc.ServiceName,
			db.ServiceUUID: svc.ServiceUUID,
		},
	}

	_, err := s.cliset.CoreV1().ConfigMaps(s.namespace).Create(cfgmap)
	if err != nil {
		glog.Errorln("create service error", err, "requuid", requuid, svc)
		return s.convertError(err)
	}

	glog.Infoln("created service", svc, "requuid", requuid)
	return nil
}

// GetService gets the service.
func (s *K8sConfigDB) GetService(ctx context.Context, clusterName string, serviceName string) (svc *common.Service, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	name := s.genServiceConfigMapName(clusterName, serviceName)
	cfgmap, err := s.cliset.CoreV1().ConfigMaps(s.namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		glog.Errorln("get service error", err, name, "requuid", requuid)
		return nil, s.convertError(err)
	}

	if cfgmap == nil {
		glog.Errorln("service config map not exist", name, "requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	service, ok := cfgmap.Data[db.ServiceName]
	if !ok || len(service) == 0 {
		glog.Errorln("service config map does not have service name", name, "requuid", requuid, cfgmap)
		return nil, db.ErrDBInternal
	}
	suuid, ok := cfgmap.Data[db.ServiceUUID]
	if !ok || len(suuid) == 0 {
		glog.Errorln("service config map does not have service uuid", name, "requuid", requuid, cfgmap)
		return nil, db.ErrDBInternal
	}

	svc = &common.Service{
		ClusterName: clusterName,
		ServiceName: service,
		ServiceUUID: suuid,
	}

	glog.Infoln("get service", svc, "requuid", requuid)
	return svc, nil
}

// DeleteService deletes one service.
func (s *K8sConfigDB) DeleteService(ctx context.Context, clusterName string, serviceName string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	name := s.genServiceConfigMapName(clusterName, serviceName)
	err := s.cliset.CoreV1().ConfigMaps(s.namespace).Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		glog.Errorln("delete service error", err, name, "requuid", requuid)
		return s.convertError(err)
	}

	glog.Infoln("deleted service", name, "requuid", requuid)
	return nil
}

// ListServices lists all services.
func (s *K8sConfigDB) ListServices(ctx context.Context, clusterName string) (svcs []*common.Service, err error) {
	return s.ListServicesWithLimit(ctx, clusterName, 0)
}

// ListServicesWithLimit does pagination list.
func (s *K8sConfigDB) ListServicesWithLimit(ctx context.Context, clusterName string, limit int64) (svcs []*common.Service, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	token := ""

	for true {
		opts := metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", labelName, serviceLabel),
		}
		if limit > 0 {
			opts.Limit = limit
		}
		if len(token) != 0 {
			opts.Continue = token
		}

		cfgmaplist, err := s.cliset.CoreV1().ConfigMaps(s.namespace).List(opts)
		if err != nil {
			glog.Errorln("list service error", err, opts.LabelSelector, "requuid", requuid)
			return nil, s.convertError(err)
		}

		if cfgmaplist == nil || len(cfgmaplist.Items) == 0 {
			if cfgmaplist != nil && len(cfgmaplist.Continue) != 0 {
				token = cfgmaplist.Continue
				glog.Errorln("no items in the response but continue token is not empty, requuid", requuid, cfgmaplist)
				continue
			}

			glog.Infoln("no more service, requuid", requuid, "service", len(svcs))
			return svcs, nil
		}

		for _, cfgmap := range cfgmaplist.Items {
			service, ok := cfgmap.Data[db.ServiceName]
			if !ok || len(service) == 0 {
				glog.Errorln("service config map does not have service name, requuid", requuid, cfgmap)
				return nil, db.ErrDBInternal
			}
			suuid, ok := cfgmap.Data[db.ServiceUUID]
			if !ok || len(suuid) == 0 {
				glog.Errorln("service config map does not have service name, requuid", requuid, cfgmap)
				return nil, db.ErrDBInternal
			}

			svc := &common.Service{
				ClusterName: clusterName,
				ServiceName: service,
				ServiceUUID: suuid,
			}
			svcs = append(svcs, svc)
		}

		glog.Infoln("listed", len(svcs), "services, requuid", requuid)

		token = cfgmaplist.Continue
		if len(token) == 0 {
			break
		}
	}

	return svcs, nil
}

func (s *K8sConfigDB) genServiceConfigMapName(clusterName string, serviceName string) string {
	hexcname := hex.EncodeToString([]byte(clusterName))
	hexname := hex.EncodeToString([]byte(serviceName))
	return fmt.Sprintf("%s-%s-%s-%s", common.SystemName, serviceLabel, hexcname, hexname)
}
