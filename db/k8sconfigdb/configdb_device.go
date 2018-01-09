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

// CreateDevice creates a device.
func (s *K8sConfigDB) CreateDevice(ctx context.Context, dev *common.Device) error {
	requuid := utils.GetReqIDFromContext(ctx)

	cfgmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.genDeviceConfigMapName(dev.ClusterName, dev.DeviceName),
			Namespace: s.namespace,
			Labels:    s.devLabels,
		},
		Data: map[string]string{
			db.ClusterName: dev.ClusterName,
			db.DeviceName:  dev.DeviceName,
			db.ServiceName: dev.ServiceName,
		},
	}

	_, err := s.cliset.CoreV1().ConfigMaps(s.namespace).Create(cfgmap)
	if err != nil {
		glog.Errorln("create device error", err, "requuid", requuid, dev)
		return s.convertError(err)
	}

	glog.Infoln("created device", dev, "requuid", requuid)
	return nil
}

// GetDevice gets the device.
func (s *K8sConfigDB) GetDevice(ctx context.Context, clusterName string, deviceName string) (dev *common.Device, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	name := s.genDeviceConfigMapName(clusterName, deviceName)
	cfgmap, err := s.cliset.CoreV1().ConfigMaps(s.namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		glog.Errorln("get device error", err, name, "requuid", requuid)
		return nil, s.convertError(err)
	}

	if cfgmap == nil {
		glog.Errorln("device config map not exist", name, "requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	service, ok := cfgmap.Data[db.ServiceName]
	if !ok || len(service) == 0 {
		glog.Errorln("device config map does not have service name", name, "requuid", requuid, cfgmap)
		return nil, db.ErrDBInternal
	}

	dev = &common.Device{
		ClusterName: clusterName,
		DeviceName:  deviceName,
		ServiceName: service,
	}

	glog.Infoln("get device", dev, "requuid", requuid)
	return dev, nil
}

// DeleteDevice deletes one device.
func (s *K8sConfigDB) DeleteDevice(ctx context.Context, clusterName string, deviceName string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	name := s.genDeviceConfigMapName(clusterName, deviceName)
	err := s.cliset.CoreV1().ConfigMaps(s.namespace).Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		glog.Errorln("delete device error", err, name, "requuid", requuid)
		return s.convertError(err)
	}

	glog.Infoln("deleted device", name, "requuid", requuid)
	return nil
}

// ListDevices lists all devices.
func (s *K8sConfigDB) ListDevices(ctx context.Context, clusterName string) (devs []*common.Device, err error) {
	return s.ListDevicesWithLimit(ctx, clusterName, 0)
}

// ListDevicesWithLimit does pagination list.
func (s *K8sConfigDB) ListDevicesWithLimit(ctx context.Context, clusterName string, limit int64) (devs []*common.Device, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	token := ""

	for true {
		opts := metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", labelName, deviceLabel),
		}
		if limit > 0 {
			opts.Limit = limit
		}
		if len(token) != 0 {
			opts.Continue = token
		}

		cfgmaplist, err := s.cliset.CoreV1().ConfigMaps(s.namespace).List(opts)
		if err != nil {
			glog.Errorln("list device error", err, opts.LabelSelector, "requuid", requuid)
			return nil, s.convertError(err)
		}

		if cfgmaplist == nil || len(cfgmaplist.Items) == 0 {
			if cfgmaplist != nil && len(cfgmaplist.Continue) != 0 {
				token = cfgmaplist.Continue
				glog.Errorln("no items in the response but continue token is not empty, requuid", requuid, cfgmaplist)
				continue
			}

			glog.Infoln("no more device, requuid", requuid, "devices", len(devs))
			return devs, nil
		}

		for _, cfgmap := range cfgmaplist.Items {
			device, ok := cfgmap.Data[db.DeviceName]
			if !ok || len(device) == 0 {
				glog.Errorln("device config map does not have device name, requuid", requuid, cfgmap)
				return nil, db.ErrDBInternal
			}
			service, ok := cfgmap.Data[db.ServiceName]
			if !ok || len(service) == 0 {
				glog.Errorln("device config map does not have service name, requuid", requuid, cfgmap)
				return nil, db.ErrDBInternal
			}

			dev := &common.Device{
				ClusterName: clusterName,
				DeviceName:  device,
				ServiceName: service,
			}
			devs = append(devs, dev)
		}

		glog.Infoln("listed", len(devs), "devices, requuid", requuid)

		token = cfgmaplist.Continue
		if len(token) == 0 {
			break
		}
	}

	return devs, nil
}

func (s *K8sConfigDB) genDeviceConfigMapName(clusterName string, deviceName string) string {
	hexcname := hex.EncodeToString([]byte(clusterName))
	hexdevname := hex.EncodeToString([]byte(deviceName))
	return fmt.Sprintf("%s-%s-%s-%s", common.SystemName, deviceLabel, hexcname, hexdevname)
}
