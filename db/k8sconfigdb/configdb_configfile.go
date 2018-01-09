package k8sconfigdb

import (
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

// CreateConfigFile creates one config file in DB
func (s *K8sConfigDB) CreateConfigFile(ctx context.Context, cfg *common.ConfigFile) error {
	requuid := utils.GetReqIDFromContext(ctx)

	cfgmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.genFileConfigMapName(cfg.ServiceUUID, cfg.FileID),
			Namespace: s.namespace,
			Labels:    s.genConfigFileListLabels(cfg.ServiceUUID),
		},
		Data: map[string]string{
			db.ServiceUUID:       cfg.ServiceUUID,
			db.ConfigFileID:      cfg.FileID,
			db.ConfigFileMD5:     cfg.FileMD5,
			db.ConfigFileName:    cfg.FileName,
			db.ConfigFileMode:    strconv.FormatUint(uint64(cfg.FileMode), 10),
			db.LastModified:      strconv.FormatInt(cfg.LastModified, 10),
			db.ConfigFileContent: cfg.Content,
		},
	}

	_, err := s.cliset.CoreV1().ConfigMaps(s.namespace).Create(cfgmap)
	if err != nil {
		glog.Errorln("failed to create config file", cfg.FileName, cfg.FileID,
			"serviceUUID", cfg.ServiceUUID, "error", err, "requuid", requuid)
		return s.convertError(err)
	}

	glog.Infoln("created config file", cfg.FileName, cfg.FileID, "serviceUUID", cfg.ServiceUUID, "requuid", requuid)
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

	mtime, err := strconv.ParseInt(cfgmap.Data[db.LastModified], 10, 64)
	if err != nil {
		glog.Errorln("ParseInt LastModified error", err, "requuid", requuid, cfgmap.Name, cfgmap.Namespace)
		return nil, db.ErrDBInternal
	}

	mode, err := strconv.ParseUint(cfgmap.Data[db.ConfigFileMode], 10, 64)
	if err != nil {
		glog.Errorln("ParseUint FileMode error", err, "requuid", requuid, cfgmap.Name, cfgmap.Namespace)
		return nil, db.ErrDBInternal
	}

	cfg, err = db.CreateConfigFile(serviceUUID,
		fileID,
		cfgmap.Data[db.ConfigFileMD5],
		cfgmap.Data[db.ConfigFileName],
		uint32(mode),
		mtime,
		cfgmap.Data[db.ConfigFileContent])
	if err != nil {
		glog.Errorln("CreateConfigFile error", err, "fileID", fileID, "serviceUUID", serviceUUID, "requuid", requuid)
		return nil, err
	}

	glog.Infoln("get config file", cfg.FileName, cfg.FileID, "serviceUUID", cfg.ServiceUUID, "requuid", requuid)
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
