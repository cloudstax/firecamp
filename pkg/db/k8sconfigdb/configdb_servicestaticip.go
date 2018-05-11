package k8sconfigdb

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/pkg/db"
	"github.com/cloudstax/firecamp/pkg/utils"
)

// CreateServiceStaticIP creates one ServiceStaticIP in DB
func (s *K8sConfigDB) CreateServiceStaticIP(ctx context.Context, serviceip *common.ServiceStaticIP) error {
	requuid := utils.GetReqIDFromContext(ctx)

	cfgmap, err := s.serviceIPToConfigMap(serviceip)
	if err != nil {
		return err
	}

	_, err = s.cliset.CoreV1().ConfigMaps(s.namespace).Create(cfgmap)
	if err != nil {
		glog.Errorln("failed to create service static ip", serviceip, "error", err, "requuid", requuid)
		return s.convertError(err)
	}

	glog.Infoln("created service static ip", serviceip, "requuid", requuid)
	return nil
}

// UpdateServiceStaticIP updates the ServiceStaticIP in DB
func (s *K8sConfigDB) UpdateServiceStaticIP(ctx context.Context, oldIP *common.ServiceStaticIP, newIP *common.ServiceStaticIP) error {
	requuid := utils.GetReqIDFromContext(ctx)

	if (oldIP.Revision+1) != newIP.Revision ||
		!db.EqualServiceStaticIPImmutableFields(oldIP, newIP) {
		glog.Errorln("revision is not increased by 1 or immutable attributes are updated, oldIP", oldIP, "newIP", newIP, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	cfgmap, err := s.serviceIPToConfigMap(newIP)
	if err != nil {
		return err
	}

	_, err = s.cliset.CoreV1().ConfigMaps(s.namespace).Update(cfgmap)
	if err != nil {
		glog.Errorln("failed to update service static ip", oldIP, "to", newIP, "error", err, "requuid", requuid)
		return s.convertError(err)
	}

	glog.Infoln("updated service static ip", oldIP, "to", newIP, "requuid", requuid)
	return nil
}

// GetServiceStaticIP gets the ServiceStaticIP from DB
func (s *K8sConfigDB) GetServiceStaticIP(ctx context.Context, staticIP string) (serviceip *common.ServiceStaticIP, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	name := s.genStaticIPConfigMapName(staticIP)

	cfgmap, err := s.cliset.CoreV1().ConfigMaps(s.namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		glog.Errorln("failed to get service static ip", name, "error", err, "requuid", requuid)
		return nil, s.convertError(err)
	}

	if cfgmap == nil {
		glog.Infoln("service static ip", name, "not found, requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	revision, err := strconv.ParseInt(cfgmap.Data[db.Revision], 10, 64)
	if err != nil {
		glog.Errorln("ParseInt Revision error", err, "requuid", requuid, cfgmap)
		return nil, db.ErrDBInternal
	}

	var spec common.StaticIPSpec
	err = json.Unmarshal([]byte(cfgmap.Data[db.StaticIPSpec]), &spec)
	if err != nil {
		glog.Errorln("Unmarshal StaticIPSpec error", err, "requuid", requuid, cfgmap)
		return nil, db.ErrDBInternal
	}

	serviceip = db.CreateServiceStaticIP(staticIP, revision, &spec)

	glog.Infoln("get service static ip", serviceip, "requuid", requuid)
	return serviceip, nil
}

// DeleteServiceStaticIP deletes the service static ip from DB
func (s *K8sConfigDB) DeleteServiceStaticIP(ctx context.Context, staticIP string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	name := s.genStaticIPConfigMapName(staticIP)

	err := s.cliset.CoreV1().ConfigMaps(s.namespace).Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		glog.Errorln("failed to delete service static ip", name, "error", err, "requuid", requuid)
		return s.convertError(err)
	}

	glog.Infoln("deleted service static ip", name, "requuid", requuid)
	return nil
}

func (s *K8sConfigDB) serviceIPToConfigMap(serviceip *common.ServiceStaticIP) (*corev1.ConfigMap, error) {
	specBytes, err := json.Marshal(serviceip.Spec)
	if err != nil {
		glog.Errorln("Marshal ServiceSpec error", err, serviceip.Spec)
		return nil, err
	}

	cfgmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.genStaticIPConfigMapName(serviceip.StaticIP),
			Namespace: s.namespace,
			Labels:    s.genStaticIPListLabels(serviceip.Spec.ServiceUUID),
		},
		Data: map[string]string{
			db.StaticIP:     serviceip.StaticIP,
			db.Revision:     strconv.FormatInt(serviceip.Revision, 10),
			db.StaticIPSpec: string(specBytes),
		},
	}
	return cfgmap, nil
}

func (s *K8sConfigDB) genStaticIPConfigMapName(staticIP string) string {
	return fmt.Sprintf("%s-%s-%s", common.SystemName, staticIPLabel, staticIP)
}

func (s *K8sConfigDB) genStaticIPLabel(serviceUUID string) string {
	return fmt.Sprintf("%s-%s-%s", common.SystemName, staticIPLabel, serviceUUID)
}

func (s *K8sConfigDB) genStaticIPListLabels(serviceUUID string) map[string]string {
	return map[string]string{
		labelName: s.genStaticIPLabel(serviceUUID),
	}
}
