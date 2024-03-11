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

// CreateConfigFile creates one config file in DB
func (s *K8sConfigDB) CreateConfigFile(ctx context.Context, cfg *common.ConfigFile) error {
	requuid := utils.GetReqIDFromContext(ctx)

	metaBytes, err := json.Marshal(cfg.Meta)
	if err != nil {
		glog.Errorln("Marshal ConfigFileMeta error", err, "requuid", requuid, cfg)
		return err
	}
	specBytes, err := json.Marshal(cfg.Spec)
	if err != nil {
		glog.Errorln("Marshal ConfigFileSpec error", err, "requuid", requuid, cfg)
		return err
	}

	cfgmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.genFileConfigMapName(cfg.ServiceUUID, cfg.FileID),
			Namespace: s.namespace,
			Labels:    s.genConfigFileListLabels(cfg.ServiceUUID),
		},
		Data: map[string]string{
			db.ServiceUUID:    cfg.ServiceUUID,
			db.ConfigFileID:   cfg.FileID,
			db.Revision:       strconv.FormatInt(cfg.Revision, 10),
			db.ConfigFileMeta: string(metaBytes),
			db.ConfigFileSpec: string(specBytes),
		},
	}

	_, err = s.cliset.CoreV1().ConfigMaps(s.namespace).Create(cfgmap)
	if err != nil {
		glog.Errorln("failed to create config file", cfg.Meta.FileName, cfg.FileID,
			"serviceUUID", cfg.ServiceUUID, "error", err, "requuid", requuid)
		return s.convertError(err)
	}

	glog.Infoln("created config file", cfg.Meta.FileName, cfg.FileID, "serviceUUID", cfg.ServiceUUID, "requuid", requuid)
	return nil
}

// GetConfigFile gets the config fileItem from DB
func (s *K8sConfigDB) GetConfigFile(ctx context.Context, serviceUUID string, fileID string) (cfg *common.ConfigFile, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	name := s.genFileConfigMapName(serviceUUID, fileID)
	cfgmap, err := s.cliset.CoreV1().ConfigMaps(s.namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		glog.Errorln("failed to get config file", fileID, "serviceUUID", serviceUUID, "error", err, "requuid", requuid)
		return nil, s.convertError(err)
	}

	revision, err := strconv.ParseInt(cfgmap.Data[db.Revision], 10, 64)
	if err != nil {
		glog.Errorln("ParseInt Revision error", err, "requuid", requuid, cfgmap.Name, cfgmap.Namespace)
		return nil, db.ErrDBInternal
	}
	var meta common.ConfigFileMeta
	err = json.Unmarshal([]byte(cfgmap.Data[db.ConfigFileMeta]), &meta)
	if err != nil {
		glog.Errorln("Unmarshal ConfigFileMeta error", err, "requuid", requuid, cfgmap)
		return nil, db.ErrDBInternal
	}
	var spec common.ConfigFileSpec
	err = json.Unmarshal([]byte(cfgmap.Data[db.ConfigFileSpec]), &spec)
	if err != nil {
		glog.Errorln("Unmarshal ConfigFileSpec error", err, "requuid", requuid, cfgmap)
		return nil, db.ErrDBInternal
	}

	cfg = db.CreateConfigFile(serviceUUID, fileID, revision, &meta, &spec)

	glog.Infoln("get config file", cfg.Meta.FileName, cfg.FileID, "serviceUUID", cfg.ServiceUUID, "requuid", requuid)
	return cfg, nil
}

// DeleteConfigFile deletes the config file from DB
func (s *K8sConfigDB) DeleteConfigFile(ctx context.Context, serviceUUID string, fileID string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	name := s.genFileConfigMapName(serviceUUID, fileID)
	err := s.cliset.CoreV1().ConfigMaps(s.namespace).Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		glog.Errorln("failed to delete config file", fileID, "serviceUUID", serviceUUID, "error", err, "requuid", requuid)
		return s.convertError(err)
	}

	glog.Infoln("deleted config file", fileID, "serviceUUID", serviceUUID, "requuid", requuid)
	return nil
}

func (s *K8sConfigDB) genFileConfigMapName(serviceUUID string, fileID string) string {
	return fmt.Sprintf("%s-%s-%s-%s", common.SystemName, configFileLabel, serviceUUID, fileID)
}

func (s *K8sConfigDB) genConfigFileLabel(serviceUUID string) string {
	return fmt.Sprintf("%s-%s-%s", common.SystemName, configFileLabel, serviceUUID)
}

func (s *K8sConfigDB) genConfigFileListLabels(serviceUUID string) map[string]string {
	return map[string]string{
		labelName: s.genConfigFileLabel(serviceUUID),
	}
}
