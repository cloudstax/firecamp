package k8sconfigdb

import (
	"encoding/json"
	"errors"
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

// CreateServiceMember creates one EBS serviceMember in DB
func (s *K8sConfigDB) CreateServiceMember(ctx context.Context, member *common.ServiceMember) error {
	requuid := utils.GetReqIDFromContext(ctx)

	cfgmap, err := s.memberToConfigMap(member, requuid)
	if err != nil {
		glog.Errorln("memberToConfigMap error", err, "requuid", requuid, member)
		return err
	}

	_, err = s.cliset.CoreV1().ConfigMaps(s.namespace).Create(cfgmap)
	if err != nil {
		glog.Errorln("failed to create serviceMember", member, "error", err, "requuid", requuid)
		return s.convertError(err)
	}

	glog.Infoln("created serviceMember", member, "requuid", requuid)
	return nil
}

// UpdateServiceMember updates the ServiceMember in DB
func (s *K8sConfigDB) UpdateServiceMember(ctx context.Context, oldMember *common.ServiceMember, newMember *common.ServiceMember) error {
	requuid := utils.GetReqIDFromContext(ctx)

	// sanity check. ServiceUUID, VolumeID, etc, are not allowed to update.
	if oldMember.ServiceUUID != newMember.ServiceUUID ||
		!db.EqualMemberVolumes(&(oldMember.Volumes), &(newMember.Volumes)) ||
		oldMember.AvailableZone != newMember.AvailableZone ||
		oldMember.MemberIndex != newMember.MemberIndex ||
		oldMember.MemberName != newMember.MemberName ||
		oldMember.StaticIP != newMember.StaticIP {
		glog.Errorln("immutable attributes are updated, oldMember", oldMember, "newMember", newMember, "requuid", requuid)
		return db.ErrDBInvalidRequest
	}

	cfgmap, err := s.memberToConfigMap(newMember, requuid)
	if err != nil {
		glog.Errorln("memberToConfigMap error", err, "requuid", requuid, newMember)
		return s.convertError(err)
	}

	_, err = s.cliset.CoreV1().ConfigMaps(s.namespace).Update(cfgmap)
	if err != nil {
		glog.Errorln("update serviceMember error", err, "requuid", requuid, newMember)
		return s.convertError(err)
	}

	glog.Infoln("updated serviceMember", oldMember, "to", newMember, "requuid", requuid)
	return nil
}

// GetServiceMember gets the serviceMemberItem from DB
func (s *K8sConfigDB) GetServiceMember(ctx context.Context, serviceUUID string, memberIndex int64) (member *common.ServiceMember, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	name := s.genServiceMemberConfigMapName(serviceUUID, memberIndex)
	cfgmap, err := s.cliset.CoreV1().ConfigMaps(s.namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		glog.Errorln("get serviceMember error", err, "memberIndex", memberIndex, "serviceUUID", serviceUUID, "requuid", requuid)
		return nil, s.convertError(err)
	}

	if cfgmap == nil {
		glog.Errorln("service member config map not exist", name, "requuid", requuid)
		return nil, db.ErrDBRecordNotFound
	}

	member, err = s.cfgmapToServiceMember(serviceUUID, cfgmap)
	if err != nil {
		glog.Errorln("cfgmapToServiceMember error", err, "requuid", requuid, cfgmap.Name, cfgmap.Namespace)
		return nil, err
	}

	glog.Infoln("get serviceMember", member, "requuid", requuid)
	return member, nil
}

// ListServiceMembers lists all serviceMembers of the service
func (s *K8sConfigDB) ListServiceMembers(ctx context.Context, serviceUUID string) (serviceMembers []*common.ServiceMember, err error) {
	return s.ListServiceMembersWithLimit(ctx, serviceUUID, -1)
}

// ListServiceMembersWithLimit limits the returned db.ServiceMembers at one query.
// This is for testing the pagination list.
func (s *K8sConfigDB) ListServiceMembersWithLimit(ctx context.Context, serviceUUID string, limit int64) (serviceMembers []*common.ServiceMember, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	token := ""

	for true {
		opts := metav1.ListOptions{
			LabelSelector: s.genServiceMemberLabelSelector(serviceUUID),
		}
		if limit > 0 {
			opts.Limit = limit
		}
		if len(token) != 0 {
			opts.Continue = token
		}

		cfgmaplist, err := s.cliset.CoreV1().ConfigMaps(s.namespace).List(opts)
		if err != nil {
			glog.Errorln("list service member error", err, "requuid", requuid, opts)
			return nil, s.convertError(err)
		}

		if cfgmaplist == nil || len(cfgmaplist.Items) == 0 {
			if cfgmaplist != nil && len(cfgmaplist.Continue) != 0 {
				token = cfgmaplist.Continue
				glog.Errorln("no items in the response but continue token is not empty, requuid", requuid, cfgmaplist)
				continue
			}

			glog.Infoln("no more service member item for service", serviceUUID, "members", len(serviceMembers), "requuid", requuid)
			return serviceMembers, nil
		}

		for _, cfgmap := range cfgmaplist.Items {
			member, err := s.cfgmapToServiceMember(serviceUUID, &cfgmap)
			if err != nil {
				glog.Errorln("cfgmapToServiceMember error", err, "requuid", requuid, cfgmap.Name, cfgmap.Namespace)
				return nil, err
			}
			serviceMembers = append(serviceMembers, member)
		}

		glog.Infoln("list", len(serviceMembers), "serviceMembers, serviceUUID", serviceUUID, "continue token", token, "requuid", requuid)

		token = cfgmaplist.Continue
		if len(token) == 0 {
			// no more serviceMembers
			break
		}
	}

	return serviceMembers, nil
}

// DeleteServiceMember deletes the serviceMember from DB
func (s *K8sConfigDB) DeleteServiceMember(ctx context.Context, serviceUUID string, memberIndex int64) error {
	requuid := utils.GetReqIDFromContext(ctx)

	name := s.genServiceMemberConfigMapName(serviceUUID, memberIndex)
	err := s.cliset.CoreV1().ConfigMaps(s.namespace).Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		glog.Errorln("delete serviceMember error", err, "memberIndex", memberIndex, "serviceUUID", serviceUUID, "requuid", requuid)
		return s.convertError(err)
	}

	glog.Infoln("deleted serviceMember", memberIndex, "serviceUUID", serviceUUID, "requuid", requuid)
	return nil
}

func (s *K8sConfigDB) cfgmapToServiceMember(serviceUUID string, cfgmap *corev1.ConfigMap) (*common.ServiceMember, error) {
	mtime, err := strconv.ParseInt(cfgmap.Data[db.LastModified], 10, 64)
	if err != nil {
		glog.Errorln("ParseInt LastModified error", err, cfgmap.Name, cfgmap.Namespace)
		return nil, db.ErrDBInternal
	}

	var configs []*common.ConfigID
	err = json.Unmarshal([]byte(cfgmap.Data[db.MemberConfigs]), &configs)
	if err != nil {
		glog.Errorln("Unmarshal json MemberConfigs error", err, cfgmap.Name, cfgmap.Namespace)
		return nil, db.ErrDBInternal
	}

	var volumes common.MemberVolumes
	err = json.Unmarshal([]byte(cfgmap.Data[db.MemberVolumes]), &volumes)
	if err != nil {
		glog.Errorln("Unmarshal json MemberVolumes error", err, cfgmap.Name, cfgmap.Namespace)
		return nil, db.ErrDBInternal
	}

	memberIndex, err := strconv.ParseInt(cfgmap.Data[db.MemberIndex], 10, 64)
	if err != nil {
		glog.Errorln("parse MemberIndex error", err, cfgmap.Name, cfgmap.Namespace)
		return nil, db.ErrDBInternal
	}

	member := db.CreateServiceMember(serviceUUID,
		memberIndex,
		cfgmap.Data[db.MemberStatus],
		cfgmap.Data[db.MemberName],
		cfgmap.Data[db.AvailableZone],
		cfgmap.Data[db.TaskID],
		cfgmap.Data[db.ContainerInstanceID],
		cfgmap.Data[db.ServerInstanceID],
		mtime,
		volumes,
		cfgmap.Data[db.StaticIP],
		configs)

	return member, nil
}

// UpdateServiceMemberVolume updates the ServiceMember's volume in DB
func (s *K8sConfigDB) UpdateServiceMemberVolume(ctx context.Context, member *common.ServiceMember, newVolID string, badVolID string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	if member.Volumes.JournalVolumeID != badVolID && member.Volumes.PrimaryVolumeID != badVolID {
		glog.Errorln("the bad volume", badVolID, "does not belong to member", member, member.Volumes)
		return errors.New("the bad volume does not belong to member")
	}

	if member.Volumes.JournalVolumeID == badVolID {
		member.Volumes.JournalVolumeID = newVolID
		glog.Infoln("replace the journal volume", badVolID, "with new volume", newVolID, "requuid", requuid, member)
	} else {
		member.Volumes.PrimaryVolumeID = newVolID
		glog.Infoln("replace the data volume", badVolID, "with new volume", newVolID, "requuid", requuid, member)
	}

	cfgmap, err := s.memberToConfigMap(member, requuid)
	if err != nil {
		glog.Errorln("memberToConfigMap error", err, "requuid", requuid, member)
		return s.convertError(err)
	}

	_, err = s.cliset.CoreV1().ConfigMaps(s.namespace).Update(cfgmap)
	if err != nil {
		glog.Errorln("update serviceMember error", err, "requuid", requuid, member)
		return s.convertError(err)
	}

	glog.Infoln("updated serviceMember to use new volume", newVolID, "bad volume", badVolID, "requuid", requuid, member)
	return nil
}

func (s *K8sConfigDB) memberToConfigMap(member *common.ServiceMember, requuid string) (*corev1.ConfigMap, error) {
	volBytes, err := json.Marshal(member.Volumes)
	if err != nil {
		glog.Errorln("Marshal MemberVolumes error", err, member, "requuid", requuid)
		return nil, err
	}
	configBytes, err := json.Marshal(member.Configs)
	if err != nil {
		glog.Errorln("Marshal MemberConfigs error", err, member, "requuid", requuid)
		return nil, err
	}

	cfgmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.genServiceMemberConfigMapName(member.ServiceUUID, member.MemberIndex),
			Namespace: s.namespace,
			Labels:    s.genServiceMemberListLabels(member.ServiceUUID),
		},
		Data: map[string]string{
			db.ServiceUUID:         member.ServiceUUID,
			db.MemberIndex:         strconv.FormatInt(member.MemberIndex, 10),
			db.MemberStatus:        member.Status,
			db.MemberName:          member.MemberName,
			db.LastModified:        strconv.FormatInt(member.LastModified, 10),
			db.AvailableZone:       member.AvailableZone,
			db.TaskID:              member.TaskID,
			db.ContainerInstanceID: member.ContainerInstanceID,
			db.ServerInstanceID:    member.ServerInstanceID,
			db.MemberVolumes:       string(volBytes),
			db.StaticIP:            member.StaticIP,
			db.MemberConfigs:       string(configBytes),
		},
	}

	return cfgmap, nil
}

func (s *K8sConfigDB) genServiceMemberConfigMapName(serviceUUID string, memberIndex int64) string {
	return fmt.Sprintf("%s-%s-%s-%d", common.SystemName, serviceMemberLabel, serviceUUID, memberIndex)
}

func (s *K8sConfigDB) genServiceMemberLabel(serviceUUID string) string {
	return fmt.Sprintf("%s-%s-%s", common.SystemName, serviceMemberLabel, serviceUUID)
}

func (s *K8sConfigDB) genServiceMemberListLabels(serviceUUID string) map[string]string {
	return map[string]string{
		labelName: s.genServiceMemberLabel(serviceUUID),
	}
}

func (s *K8sConfigDB) genServiceMemberLabelSelector(serviceUUID string) string {
	return fmt.Sprintf("%s=%s", labelName, s.genServiceMemberLabel(serviceUUID))
}
