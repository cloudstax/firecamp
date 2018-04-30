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

	if oldMember.Revision+1 != newMember.Revision ||
		!db.EqualServiceMemberImmutableFields(oldMember, newMember) {
		glog.Errorln("revision is not increased by 1 or immutable attributes are updated, oldMember",
			oldMember, "newMember", newMember, "requuid", requuid)
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
func (s *K8sConfigDB) GetServiceMember(ctx context.Context, serviceUUID string, memberName string) (member *common.ServiceMember, err error) {
	requuid := utils.GetReqIDFromContext(ctx)

	name := s.genServiceMemberConfigMapName(serviceUUID, memberName)
	cfgmap, err := s.cliset.CoreV1().ConfigMaps(s.namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		glog.Errorln("get serviceMember error", err, "member", memberName, "serviceUUID", serviceUUID, "requuid", requuid)
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
func (s *K8sConfigDB) DeleteServiceMember(ctx context.Context, serviceUUID string, memberName string) error {
	requuid := utils.GetReqIDFromContext(ctx)

	name := s.genServiceMemberConfigMapName(serviceUUID, memberName)
	err := s.cliset.CoreV1().ConfigMaps(s.namespace).Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		glog.Errorln("delete serviceMember error", err, "member", memberName, "serviceUUID", serviceUUID, "requuid", requuid)
		return s.convertError(err)
	}

	glog.Infoln("deleted serviceMember", memberName, "serviceUUID", serviceUUID, "requuid", requuid)
	return nil
}

func (s *K8sConfigDB) cfgmapToServiceMember(serviceUUID string, cfgmap *corev1.ConfigMap) (*common.ServiceMember, error) {
	revision, err := strconv.ParseInt(cfgmap.Data[db.Revision], 10, 64)
	if err != nil {
		glog.Errorln("ParseInt Revision error", err, cfgmap.Name, cfgmap.Namespace)
		return nil, db.ErrDBInternal
	}

	var meta common.MemberMeta
	err = json.Unmarshal([]byte(cfgmap.Data[db.MemberMeta]), &meta)
	if err != nil {
		glog.Errorln("Unmarshal json MemberMeta error", err, cfgmap.Name, cfgmap.Namespace)
		return nil, db.ErrDBInternal
	}

	var spec common.MemberSpec
	err = json.Unmarshal([]byte(cfgmap.Data[db.MemberSpec]), &spec)
	if err != nil {
		glog.Errorln("Unmarshal json MemberSpec error", err, cfgmap.Name, cfgmap.Namespace)
		return nil, db.ErrDBInternal
	}

	memberName := cfgmap.Data[db.MemberName]

	member := db.CreateServiceMember(serviceUUID, memberName, revision, &meta, &spec)
	return member, nil
}

func (s *K8sConfigDB) memberToConfigMap(member *common.ServiceMember, requuid string) (*corev1.ConfigMap, error) {
	metaBytes, err := json.Marshal(member.Meta)
	if err != nil {
		glog.Errorln("Marshal MemberMeta error", err, member, "requuid", requuid)
		return nil, err
	}
	specBytes, err := json.Marshal(member.Spec)
	if err != nil {
		glog.Errorln("Marshal MemberSpec error", err, member, "requuid", requuid)
		return nil, err
	}

	cfgmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.genServiceMemberConfigMapName(member.ServiceUUID, member.MemberName),
			Namespace: s.namespace,
			Labels:    s.genServiceMemberListLabels(member.ServiceUUID),
		},
		Data: map[string]string{
			db.ServiceUUID: member.ServiceUUID,
			db.MemberName:  member.MemberName,
			db.Revision:    strconv.FormatInt(member.Revision, 10),
			db.MemberMeta:  string(metaBytes),
			db.MemberSpec:  string(specBytes),
		},
	}

	return cfgmap, nil
}

func (s *K8sConfigDB) genServiceMemberConfigMapName(serviceUUID string, memberName string) string {
	return fmt.Sprintf("%s-%s-%s-%s", common.SystemName, serviceMemberLabel, serviceUUID, memberName)
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
