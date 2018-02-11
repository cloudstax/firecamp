package k8sconfigdb

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	"github.com/cloudstax/firecamp/utils"
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

	// Only support updating ServiceStatus, Replicas or UserAttr at v1, all other attributes are immutable.
	if oldAttr.ClusterName != newAttr.ClusterName ||
		oldAttr.DomainName != newAttr.DomainName ||
		oldAttr.HostedZoneID != newAttr.HostedZoneID ||
		oldAttr.RegisterDNS != newAttr.RegisterDNS ||
		oldAttr.RequireStaticIP != newAttr.RequireStaticIP ||
		oldAttr.ServiceName != newAttr.ServiceName ||
		!db.EqualResources(&oldAttr.Resource, &newAttr.Resource) ||
		!db.EqualServiceVolumes(&oldAttr.Volumes, &newAttr.Volumes) ||
		oldAttr.ServiceType != newAttr.ServiceType {
		glog.Errorln("immutable fields could not be updated, oldAttr", oldAttr, "newAttr", newAttr, "requuid", requuid)
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

	replicas, err := strconv.ParseInt(cfgmap.Data[db.Replicas], 10, 64)
	if err != nil {
		glog.Errorln("ParseInt Replicas error", err, "requuid", requuid, cfgmap)
		return nil, db.ErrDBInternal
	}
	mtime, err := strconv.ParseInt(cfgmap.Data[db.LastModified], 10, 64)
	if err != nil {
		glog.Errorln("ParseInt LastModified error", err, "requuid", requuid, cfgmap)
		return nil, db.ErrDBInternal
	}
	registerDNS, err := strconv.ParseBool(cfgmap.Data[db.RegisterDNS])
	if err != nil {
		glog.Errorln("ParseBool RegisterDNS error", err, "requuid", requuid, cfgmap)
		return nil, db.ErrDBInternal
	}
	requireStaticIP, err := strconv.ParseBool(cfgmap.Data[db.RequireStaticIP])
	if err != nil {
		glog.Errorln("ParseBool RequireStaticIP error", err, "requuid", requuid, cfgmap)
		return nil, db.ErrDBInternal
	}
	var vols common.ServiceVolumes
	err = json.Unmarshal([]byte(cfgmap.Data[db.ServiceVolumes]), &vols)
	if err != nil {
		glog.Errorln("Unmarshal ServiceVolumes error", err, "requuid", requuid, cfgmap)
		return nil, db.ErrDBInternal
	}
	var userAttr *common.ServiceUserAttr
	if _, ok := cfgmap.Data[db.UserAttr]; ok {
		tmpAttr := &common.ServiceUserAttr{}
		err = json.Unmarshal([]byte(cfgmap.Data[db.UserAttr]), tmpAttr)
		if err != nil {
			glog.Errorln("Unmarshal ServiceUserAttr error", err, "requuid", requuid, cfgmap)
			return nil, db.ErrDBInternal
		}
		userAttr = tmpAttr
	}
	res := common.Resources{
		MaxCPUUnits:     common.DefaultMaxCPUUnits,
		ReserveCPUUnits: common.DefaultReserveCPUUnits,
		MaxMemMB:        common.DefaultMaxMemoryMB,
		ReserveMemMB:    common.DefaultReserveMemoryMB,
	}
	if _, ok := cfgmap.Data[db.Resource]; ok {
		err = json.Unmarshal([]byte(cfgmap.Data[db.Resource]), &res)
		if err != nil {
			glog.Errorln("Unmarshal Resource error", err, "requuid", requuid, cfgmap)
			return nil, db.ErrDBInternal
		}
	}

	attr = db.CreateServiceAttr(
		serviceUUID,
		cfgmap.Data[db.ServiceStatus],
		mtime,
		replicas,
		cfgmap.Data[db.ClusterName],
		cfgmap.Data[db.ServiceName],
		vols,
		registerDNS,
		cfgmap.Data[db.DomainName],
		cfgmap.Data[db.HostedZoneID],
		requireStaticIP,
		userAttr,
		res,
		cfgmap.Data[db.ServiceType])

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
	volBytes, err := json.Marshal(attr.Volumes)
	if err != nil {
		glog.Errorln("Marshal ServiceVolumes error", err, "requuid", requuid, attr)
		return nil, err
	}
	resBytes, err := json.Marshal(attr.Resource)
	if err != nil {
		glog.Errorln("Marshal Resources error", err, "requuid", requuid, attr.Resource, attr)
		return nil, err
	}

	cfgmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.genServiceAttrConfigMapName(attr.ServiceUUID),
			Namespace: s.namespace,
			Labels:    s.attrLabels,
		},
		Data: map[string]string{
			db.ServiceUUID:     attr.ServiceUUID,
			db.ServiceStatus:   attr.ServiceStatus,
			db.LastModified:    strconv.FormatInt(attr.LastModified, 10),
			db.Replicas:        strconv.FormatInt(attr.Replicas, 10),
			db.ClusterName:     attr.ClusterName,
			db.ServiceName:     attr.ServiceName,
			db.ServiceVolumes:  string(volBytes),
			db.RegisterDNS:     strconv.FormatBool(attr.RegisterDNS),
			db.DomainName:      attr.DomainName,
			db.HostedZoneID:    attr.HostedZoneID,
			db.RequireStaticIP: strconv.FormatBool(attr.RequireStaticIP),
			db.Resource:        string(resBytes),
			db.ServiceType:     attr.ServiceType,
		},
	}
	if attr.UserAttr != nil {
		userAttrBytes, err := json.Marshal(attr.UserAttr)
		if err != nil {
			glog.Errorln("Marshal ServiceUserAttr error", err, "requuid", requuid, attr)
			return nil, err
		}
		cfgmap.Data[db.UserAttr] = string(userAttrBytes)
	}

	return cfgmap, nil
}

func (s *K8sConfigDB) genServiceAttrConfigMapName(serviceUUID string) string {
	return fmt.Sprintf("%s-%s-%s", common.SystemName, serviceAttrLabel, serviceUUID)
}
