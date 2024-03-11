package k8sconfigdb

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/jazzl0ver/firecamp/api/common"
	"github.com/jazzl0ver/firecamp/pkg/db"
	"github.com/jazzl0ver/firecamp/pkg/utils"
)

// CreateServiceAttr creates a service attr.
func (s *K8sConfigDB) CreateServiceAttr(ctx context.Context, attr *common.ServiceAttr) error {
	requuid := utils.GetReqIDFromContext(ctx)

	cfgmap, err := s.attrToConfigMap(attr, requuid)
	if err != nil {
		glog.Errorln("attrToConfigMap error", err, "requuid", requuid, attr)
		return err
	}

	_, err = s.cliset.CoreV1().ConfigMaps(s.namespace).Create(cfgmap)
	if err != nil {
		glog.Errorln("create service attr error", err, "requuid", requuid, attr)
		return s.convertError(err)
	}

	glog.Infoln("created service attr", attr, "requuid", requuid)
	return nil
}

// UpdateServiceAttr updates the ServiceAttr in DB.
func (s *K8sConfigDB) UpdateServiceAttr(ctx context.Context, oldAttr *common.ServiceAttr, newAttr *common.ServiceAttr) error {
	requuid := utils.GetReqIDFromContext(ctx)

	if (oldAttr.Revision+1) != newAttr.Revision ||
		!db.EqualServiceAttrImmutableFields(oldAttr, newAttr) {
		glog.Errorln("revision not increased by 1 or immutable fields are updated, oldAttr", oldAttr, "newAttr", newAttr, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	// k8s configmap does not support conditional update. While, this is not an issue,
	// as k8s statefulset knows the member index and always load the same volume for one pod.
	cfgmap, err := s.attrToConfigMap(newAttr, requuid)
	if err != nil {
		glog.Errorln("attrToConfigMap error", err, "requuid", requuid, newAttr)
		return s.convertError(err)
	}

	_, err = s.cliset.CoreV1().ConfigMaps(s.namespace).Update(cfgmap)
	if err != nil {
		glog.Errorln("failed to update service attr", oldAttr, "to", newAttr, "error", err, "requuid", requuid)
		return s.convertError(err)
	}

	glog.Infoln("updated service attr", oldAttr, "to", newAttr, "requuid", requuid)
	return nil
}

// GetServiceAttr gets the service attr.
func (s *K8sConfigDB) GetServiceAttr(ctx context.Context, serviceUUID string) (attr *common.ServiceAttr, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	name := s.genServiceAttrConfigMapName(serviceUUID)
	cfgmap, err := s.cliset.CoreV1().ConfigMaps(s.namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		glog.Errorln("get service attr error", err, name, "requuid", requuid)
		return nil, s.convertError(err)
	}

	if cfgmap == nil {
		glog.Errorln("service attr config map not exist", name, "requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	revision, err := strconv.ParseInt(cfgmap.Data[db.Revision], 10, 64)
	if err != nil {
		glog.Errorln("ParseInt Revision error", err, "requuid", requuid, cfgmap)
		return nil, db.ErrDBInternal
	}
	var meta common.ServiceMeta
	err = json.Unmarshal([]byte(cfgmap.Data[db.ServiceMeta]), &meta)
	if err != nil {
		glog.Errorln("Unmarshal ServiceMeta error", err, "requuid", requuid, cfgmap)
		return nil, db.ErrDBInternal
	}
	var spec common.ServiceSpec
	err = json.Unmarshal([]byte(cfgmap.Data[db.ServiceSpec]), &spec)
	if err != nil {
		glog.Errorln("Unmarshal ServiceSpec error", err, "requuid", requuid, cfgmap)
		return nil, db.ErrDBInternal
	}

	attr = db.CreateServiceAttr(serviceUUID, revision, &meta, &spec)

	glog.Infoln("get service attr", attr, "requuid", requuid)
	return attr, nil
}

// DeleteServiceAttr deletes one service attr.
func (s *K8sConfigDB) DeleteServiceAttr(ctx context.Context, serviceUUID string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	name := s.genServiceAttrConfigMapName(serviceUUID)
	err := s.cliset.CoreV1().ConfigMaps(s.namespace).Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		glog.Errorln("delete service attr error", err, name, "requuid", requuid)
		return s.convertError(err)
	}

	glog.Infoln("deleted service attr", name, "requuid", requuid)
	return nil
}

func (s *K8sConfigDB) attrToConfigMap(attr *common.ServiceAttr, requuid string) (*corev1.ConfigMap, error) {
	metaBytes, err := json.Marshal(attr.Meta)
	if err != nil {
		glog.Errorln("Marshal ServiceMeta error", err, "requuid", requuid, attr)
		return nil, err
	}
	specBytes, err := json.Marshal(attr.Spec)
	if err != nil {
		glog.Errorln("Marshal ServiceSpec error", err, "requuid", requuid, attr)
		return nil, err
	}

	cfgmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.genServiceAttrConfigMapName(attr.ServiceUUID),
			Namespace: s.namespace,
			Labels:    s.attrLabels,
		},
		Data: map[string]string{
			db.ServiceUUID: attr.ServiceUUID,
			db.Revision:    strconv.FormatInt(attr.Revision, 10),
			db.ServiceMeta: string(metaBytes),
			db.ServiceSpec: string(specBytes),
		},
	}

	return cfgmap, nil
}

func (s *K8sConfigDB) genServiceAttrConfigMapName(serviceUUID string) string {
	return fmt.Sprintf("%s-%s-%s", common.SystemName, serviceAttrLabel, serviceUUID)
}
