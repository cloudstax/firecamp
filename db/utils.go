package db

import (
	"errors"
	"fmt"
	"time"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/utils"
)

const (
	// Service members need to be created in advance. So TaskID, ContainerInstanceID
	// and ServerInstanceID would be empty at service member creation.
	// set them to default values, this will help the later conditional update.
	DefaultTaskID              = "defaultTaskID"
	DefaultContainerInstanceID = "defaultContainerInstanceID"
	DefaultServerInstanceID    = "defaultServerInstanceID"
)

func CreateDevice(cluster string, device string, service string) *common.Device {
	return &common.Device{
		ClusterName: cluster,
		DeviceName:  device,
		ServiceName: service,
	}
}

func EqualDevice(t1 *common.Device, t2 *common.Device) bool {
	if t1.ClusterName == t2.ClusterName &&
		t1.DeviceName == t2.DeviceName &&
		t1.ServiceName == t2.ServiceName {
		return true
	}
	return false
}

func CopyDevice(d *common.Device) *common.Device {
	return &common.Device{
		ClusterName: d.ClusterName,
		DeviceName:  d.DeviceName,
		ServiceName: d.ServiceName,
	}
}

func CreateService(cluster string, service string, serviceUUID string) *common.Service {
	return &common.Service{
		ClusterName: cluster,
		ServiceName: service,
		ServiceUUID: serviceUUID,
	}
}

func EqualService(t1 *common.Service, t2 *common.Service) bool {
	if t1.ClusterName == t2.ClusterName &&
		t1.ServiceName == t2.ServiceName &&
		t1.ServiceUUID == t2.ServiceUUID {
		return true
	}
	return false
}

func CopyService(s *common.Service) *common.Service {
	return &common.Service{
		ClusterName: s.ClusterName,
		ServiceName: s.ServiceName,
		ServiceUUID: s.ServiceUUID,
	}
}

func CreateServiceMeta(cluster string, service string, mtime int64, serviceType string, status string) *common.ServiceMeta {
	return &common.ServiceMeta{
		ClusterName:   cluster,
		ServiceName:   service,
		LastModified:  mtime,
		ServiceType:   serviceType,
		ServiceStatus: status,
	}
}

func EqualServiceMeta(s1 *common.ServiceMeta, s2 *common.ServiceMeta, skipMtime bool) bool {
	return s1.ClusterName == s2.ClusterName &&
		s1.ServiceName == s2.ServiceName &&
		(skipMtime || s1.LastModified == s2.LastModified) &&
		s1.ServiceType == s2.ServiceType &&
		s1.ServiceStatus == s2.ServiceStatus
}

func CopyServiceMeta(s *common.ServiceMeta) *common.ServiceMeta {
	return &common.ServiceMeta{
		ClusterName:   s.ClusterName,
		ServiceName:   s.ServiceName,
		LastModified:  s.LastModified,
		ServiceType:   s.ServiceType,
		ServiceStatus: s.ServiceStatus,
	}
}

func CreateServiceSpec(replicas int64, res *common.Resources, registerDNS bool,
	domain string, hostedZoneID string, requireStaticIP bool,
	serviceCfgs []common.ConfigID, vols *common.ServiceVolumes) *common.ServiceSpec {
	return &common.ServiceSpec{
		Replicas:        replicas,
		Resource:        *res,
		RegisterDNS:     registerDNS,
		DomainName:      domain,
		HostedZoneID:    hostedZoneID,
		RequireStaticIP: requireStaticIP,
		ServiceConfigs:  serviceCfgs,
		Volumes:         *vols,
	}
}

func EqualServiceSpec(s1 *common.ServiceSpec, s2 *common.ServiceSpec) bool {
	return s1.Replicas == s2.Replicas &&
		EqualResources(&s1.Resource, &s2.Resource) &&
		s1.RegisterDNS == s2.RegisterDNS &&
		s1.DomainName == s2.DomainName &&
		s1.HostedZoneID == s2.HostedZoneID &&
		s1.RequireStaticIP == s2.RequireStaticIP &&
		EqualConfigs(s1.ServiceConfigs, s2.ServiceConfigs) &&
		EqualServiceVolumes(&s1.Volumes, &s2.Volumes)
}

func CopyServiceSpec(s *common.ServiceSpec) *common.ServiceSpec {
	return &common.ServiceSpec{
		Replicas:        s.Replicas,
		Resource:        *CopyResources(&s.Resource),
		RegisterDNS:     s.RegisterDNS,
		DomainName:      s.DomainName,
		HostedZoneID:    s.HostedZoneID,
		RequireStaticIP: s.RequireStaticIP,
		ServiceConfigs:  CopyConfigs(s.ServiceConfigs),
		Volumes:         *CopyServiceVolumes(&s.Volumes),
	}
}

func CreateServiceAttr(serviceUUID string, revision int64, meta *common.ServiceMeta, spec *common.ServiceSpec) *common.ServiceAttr {
	return &common.ServiceAttr{
		ServiceUUID: serviceUUID,
		Revision:    revision,
		Meta:        *meta,
		Spec:        *spec,
	}
}

func EqualServiceAttr(s1 *common.ServiceAttr, s2 *common.ServiceAttr, skipMtime bool) bool {
	return s1.ServiceUUID == s2.ServiceUUID &&
		s1.Revision == s2.Revision &&
		EqualServiceMeta(&s1.Meta, &s2.Meta, skipMtime) &&
		EqualServiceSpec(&s1.Spec, &s2.Spec)
}

func EqualServiceAttrImmutableFields(s1 *common.ServiceAttr, s2 *common.ServiceAttr) bool {
	// Only Revision, Meta.LastModified, Meta.ServiceStatus, Spec.Replicas,
	// Spec.Resources, Spec.ServiceConfigs are allowed to change.
	return s1.ServiceUUID == s2.ServiceUUID &&
		s1.Meta.ClusterName == s2.Meta.ClusterName &&
		s1.Meta.ServiceName == s2.Meta.ServiceName &&
		s1.Meta.ServiceType == s2.Meta.ServiceType &&
		s1.Spec.RegisterDNS == s2.Spec.RegisterDNS &&
		s1.Spec.DomainName == s2.Spec.DomainName &&
		s1.Spec.HostedZoneID == s2.Spec.HostedZoneID &&
		s1.Spec.RequireStaticIP == s2.Spec.RequireStaticIP &&
		EqualServiceVolumes(&s1.Spec.Volumes, &s2.Spec.Volumes)
}

func CopyServiceAttr(s *common.ServiceAttr) *common.ServiceAttr {
	return &common.ServiceAttr{
		ServiceUUID: s.ServiceUUID,
		Revision:    s.Revision,
		Meta:        *CopyServiceMeta(&s.Meta),
		Spec:        *CopyServiceSpec(&s.Spec),
	}
}

func UpdateServiceStatus(t1 *common.ServiceAttr, status string) *common.ServiceAttr {
	newMeta := CopyServiceMeta(&t1.Meta)
	newMeta.LastModified = time.Now().UnixNano()
	newMeta.ServiceStatus = status

	return &common.ServiceAttr{
		ServiceUUID: t1.ServiceUUID,
		Revision:    t1.Revision + 1,
		Meta:        *newMeta,
		Spec:        t1.Spec,
	}
}

func UpdateServiceReplicas(t1 *common.ServiceAttr, replicas int64) *common.ServiceAttr {
	newMeta := CopyServiceMeta(&t1.Meta)
	newMeta.LastModified = time.Now().UnixNano()

	newSpec := CopyServiceSpec(&t1.Spec)
	newSpec.Replicas = replicas

	return &common.ServiceAttr{
		ServiceUUID: t1.ServiceUUID,
		Revision:    t1.Revision + 1,
		Meta:        *newMeta,
		Spec:        *newSpec,
	}
}

func EqualResources(r1 *common.Resources, r2 *common.Resources) bool {
	if r1.MaxCPUUnits == r2.MaxCPUUnits &&
		r1.ReserveCPUUnits == r2.ReserveCPUUnits &&
		r1.MaxMemMB == r2.MaxMemMB &&
		r1.ReserveMemMB == r2.ReserveMemMB {
		return true
	}
	return false
}

func CopyResources(r *common.Resources) *common.Resources {
	return &common.Resources{
		MaxCPUUnits:     r.MaxCPUUnits,
		ReserveCPUUnits: r.ReserveCPUUnits,
		MaxMemMB:        r.MaxMemMB,
		ReserveMemMB:    r.ReserveMemMB,
	}
}

func EqualServiceVolumes(v1 *common.ServiceVolumes, v2 *common.ServiceVolumes) bool {
	return v1.PrimaryDeviceName == v2.PrimaryDeviceName &&
		EqualServiceVolume(&(v1.PrimaryVolume), &(v2.PrimaryVolume)) &&
		v1.JournalDeviceName == v2.JournalDeviceName &&
		EqualServiceVolume(&(v1.JournalVolume), &(v2.JournalVolume))
}

func CopyServiceVolumes(v *common.ServiceVolumes) *common.ServiceVolumes {
	return &common.ServiceVolumes{
		PrimaryDeviceName: v.PrimaryDeviceName,
		PrimaryVolume:     *CopyServiceVolume(&v.PrimaryVolume),
		JournalDeviceName: v.JournalDeviceName,
		JournalVolume:     *CopyServiceVolume(&v.JournalVolume),
	}
}

func EqualServiceVolume(v1 *common.ServiceVolume, v2 *common.ServiceVolume) bool {
	return v1.VolumeType == v2.VolumeType &&
		v1.Iops == v2.Iops &&
		v1.VolumeSizeGB == v2.VolumeSizeGB &&
		v1.Encrypted == v2.Encrypted
}

func CopyServiceVolume(v *common.ServiceVolume) *common.ServiceVolume {
	return &common.ServiceVolume{
		VolumeType:   v.VolumeType,
		Iops:         v.Iops,
		VolumeSizeGB: v.VolumeSizeGB,
		Encrypted:    v.Encrypted,
	}
}

func CreateMemberMeta(memberName string, mtime int64, status string) *common.MemberMeta {
	return &common.MemberMeta{
		MemberName:   memberName,
		LastModified: mtime,
		Status:       status,
	}
}

func EqualMemberMeta(m1 *common.MemberMeta, m2 *common.MemberMeta, skipMtime bool) bool {
	return m1.MemberName == m2.MemberName &&
		(skipMtime || m1.LastModified == m2.LastModified) &&
		m1.Status == m2.Status
}

func CopyMemberMeta(m *common.MemberMeta) *common.MemberMeta {
	return &common.MemberMeta{
		MemberName:   m.MemberName,
		LastModified: m.LastModified,
		Status:       m.Status,
	}
}

func CreateInitialMemberSpec(az string, vols *common.MemberVolumes, staticIP string, configs []common.ConfigID) *common.MemberSpec {
	return &common.MemberSpec{
		AvailableZone:       az,
		TaskID:              DefaultTaskID,
		ContainerInstanceID: DefaultContainerInstanceID,
		ServerInstanceID:    DefaultServerInstanceID,
		Volumes:             *vols,
		StaticIP:            staticIP,
		Configs:             configs,
	}
}

func CreateMemberSpec(az string, taskID string, containerInstanceID string, serverInstanceID string,
	vols *common.MemberVolumes, staticIP string, configs []common.ConfigID) *common.MemberSpec {
	return &common.MemberSpec{
		AvailableZone:       az,
		TaskID:              taskID,
		ContainerInstanceID: containerInstanceID,
		ServerInstanceID:    serverInstanceID,
		Volumes:             *vols,
		StaticIP:            staticIP,
		Configs:             configs,
	}
}

func EqualMemberSpec(m1 *common.MemberSpec, m2 *common.MemberSpec) bool {
	return m1.AvailableZone == m2.AvailableZone &&
		m1.TaskID == m2.TaskID &&
		m1.ContainerInstanceID == m2.ContainerInstanceID &&
		m1.ServerInstanceID == m2.ServerInstanceID &&
		EqualMemberVolumes(&m1.Volumes, &m2.Volumes) &&
		m1.StaticIP == m2.StaticIP &&
		EqualConfigs(m1.Configs, m2.Configs)
}

func CopyMemberSpec(m *common.MemberSpec) *common.MemberSpec {
	return &common.MemberSpec{
		AvailableZone:       m.AvailableZone,
		TaskID:              m.TaskID,
		ContainerInstanceID: m.ContainerInstanceID,
		ServerInstanceID:    m.ServerInstanceID,
		Volumes:             *CopyMemberVolumes(&m.Volumes),
		StaticIP:            m.StaticIP,
		Configs:             CopyConfigs(m.Configs),
	}
}

func CreateServiceMember(serviceUUID string, memberIndex int64, revision int64, meta *common.MemberMeta, spec *common.MemberSpec) *common.ServiceMember {
	return &common.ServiceMember{
		ServiceUUID: serviceUUID,
		MemberIndex: memberIndex,
		Revision:    revision,
		Meta:        *meta,
		Spec:        *spec,
	}
}

func EqualServiceMember(t1 *common.ServiceMember, t2 *common.ServiceMember, skipMtime bool) bool {
	return t1.ServiceUUID == t2.ServiceUUID &&
		t1.MemberIndex == t2.MemberIndex &&
		t1.Revision == t2.Revision &&
		EqualMemberMeta(&t1.Meta, &t2.Meta, skipMtime) &&
		EqualMemberSpec(&t1.Spec, &t2.Spec)
}

func EqualServiceMemberImmutableFields(m1 *common.ServiceMember, m2 *common.ServiceMember) bool {
	// Only Revision, Meta.LastModified, Meta.Status, Spec.TaskID, Spec.ContainerInstanceID,
	// Spec.ServerInstanceID, Spec.Volumes, Spec.Configs are allowed to change
	return m1.ServiceUUID == m2.ServiceUUID &&
		m1.MemberIndex == m2.MemberIndex &&
		m1.Meta.MemberName == m2.Meta.MemberName &&
		m1.Spec.AvailableZone == m2.Spec.AvailableZone &&
		m1.Spec.StaticIP == m2.Spec.StaticIP
}

func CopyServiceMember(m *common.ServiceMember) *common.ServiceMember {
	return &common.ServiceMember{
		ServiceUUID: m.ServiceUUID,
		MemberIndex: m.MemberIndex,
		Revision:    m.Revision,
		Meta:        *CopyMemberMeta(&m.Meta),
		Spec:        *CopyMemberSpec(&m.Spec),
	}
}

func UpdateServiceMemberConfigs(t1 *common.ServiceMember, c []common.ConfigID) *common.ServiceMember {
	newMeta := CopyMemberMeta(&t1.Meta)
	newMeta.LastModified = time.Now().UnixNano()

	newSpec := CopyMemberSpec(&t1.Spec)
	newSpec.Configs = c

	return &common.ServiceMember{
		ServiceUUID: t1.ServiceUUID,
		MemberIndex: t1.MemberIndex,
		Revision:    t1.Revision + 1,
		Meta:        *newMeta,
		Spec:        *newSpec,
	}
}

func UpdateServiceMemberOwner(t1 *common.ServiceMember, taskID string, containerInstanceID string, serverInstanceID string) *common.ServiceMember {
	newMeta := CopyMemberMeta(&t1.Meta)
	newMeta.LastModified = time.Now().UnixNano()

	newSpec := CopyMemberSpec(&t1.Spec)
	newSpec.TaskID = taskID
	newSpec.ContainerInstanceID = containerInstanceID
	newSpec.ServerInstanceID = serverInstanceID

	return &common.ServiceMember{
		ServiceUUID: t1.ServiceUUID,
		MemberIndex: t1.MemberIndex,
		Revision:    t1.Revision + 1,
		Meta:        *newMeta,
		Spec:        *newSpec,
	}
}

func EqualMemberVolumes(v1 *common.MemberVolumes, v2 *common.MemberVolumes) bool {
	if v1.PrimaryVolumeID == v2.PrimaryVolumeID &&
		v1.PrimaryDeviceName == v2.PrimaryDeviceName &&
		v1.JournalVolumeID == v2.JournalVolumeID &&
		v1.JournalDeviceName == v2.JournalDeviceName {
		return true
	}
	return false
}

func CopyMemberVolumes(v *common.MemberVolumes) *common.MemberVolumes {
	return &common.MemberVolumes{
		PrimaryVolumeID:   v.PrimaryVolumeID,
		PrimaryDeviceName: v.PrimaryDeviceName,
		JournalVolumeID:   v.JournalVolumeID,
		JournalDeviceName: v.JournalDeviceName,
	}
}

func EqualConfigs(c1 []common.ConfigID, c2 []common.ConfigID) bool {
	if len(c1) != len(c2) {
		return false
	}
	for i := 0; i < len(c1); i++ {
		if c1[i].FileName != c2[i].FileName ||
			c1[i].FileID != c2[i].FileID ||
			c1[i].FileMD5 != c2[i].FileMD5 {
			return false
		}
	}
	return true
}

func CopyConfigs(c1 []common.ConfigID) []common.ConfigID {
	c2 := make([]common.ConfigID, len(c1))
	for i, c := range c1 {
		c2[i] = common.ConfigID{
			FileName: c.FileName,
			FileID:   c.FileID,
			FileMD5:  c.FileMD5,
		}
	}
	return c2
}

func CreateInitialConfigFile(serviceUUID string, fileID string, fileName string, fileMode uint32, content string) *common.ConfigFile {
	chksum := utils.GenMD5(content)
	return &common.ConfigFile{
		ServiceUUID:  serviceUUID,
		FileID:       fileID,
		FileMD5:      chksum,
		FileName:     fileName,
		FileMode:     fileMode,
		LastModified: time.Now().UnixNano(),
		Content:      content,
	}
}

func CreateConfigFile(serviceUUID string, fileID string, fileMD5 string,
	fileName string, fileMode uint32, mtime int64, content string) (*common.ConfigFile, error) {
	// double check config file
	chksum := utils.GenMD5(content)
	if chksum != fileMD5 {
		errmsg := fmt.Sprintf("internal error, file %s content corrupted, expect md5 %s content md5 %s",
			fileID, fileMD5, chksum)
		return nil, errors.New(errmsg)
	}

	cfg := &common.ConfigFile{
		ServiceUUID:  serviceUUID,
		FileID:       fileID,
		FileMD5:      fileMD5,
		FileName:     fileName,
		FileMode:     fileMode,
		LastModified: mtime,
		Content:      content,
	}
	return cfg, nil
}

func UpdateConfigFile(c *common.ConfigFile, newFileID string, newContent string) *common.ConfigFile {
	newMD5 := utils.GenMD5(newContent)
	return &common.ConfigFile{
		ServiceUUID:  c.ServiceUUID,
		FileID:       newFileID,
		FileMD5:      newMD5,
		FileName:     c.FileName,
		FileMode:     c.FileMode,
		LastModified: time.Now().UnixNano(),
		Content:      newContent,
	}
}

func EqualConfigFile(c1 *common.ConfigFile, c2 *common.ConfigFile, skipMtime bool, skipContent bool) bool {
	if c1.ServiceUUID == c2.ServiceUUID &&
		c1.FileID == c2.FileID &&
		c1.FileMD5 == c2.FileMD5 &&
		c1.FileName == c2.FileName &&
		c1.FileMode == c2.FileMode &&
		(skipMtime || c1.LastModified == c2.LastModified) &&
		(skipContent || c1.Content == c2.Content) {
		return true
	}
	return false
}

func CopyConfigFile(t *common.ConfigFile) *common.ConfigFile {
	return &common.ConfigFile{
		FileID:       t.FileID,
		FileMD5:      t.FileMD5,
		FileName:     t.FileName,
		FileMode:     t.FileMode,
		ServiceUUID:  t.ServiceUUID,
		LastModified: t.LastModified,
		Content:      t.Content,
	}
}

func PrintConfigFile(cfg *common.ConfigFile) string {
	return fmt.Sprintf("serviceUUID %s fileID %s fileName %s fileMD5 %s fileMode %d LastModified %d",
		cfg.ServiceUUID, cfg.FileID, cfg.FileName, cfg.FileMD5, cfg.FileMode, cfg.LastModified)
}

func CreateStaticIPSpec(serviceUUID string, az string, serverInstanceID string, netInterfaceID string) *common.StaticIPSpec {
	return &common.StaticIPSpec{
		ServiceUUID:        serviceUUID,
		AvailableZone:      az,
		ServerInstanceID:   serverInstanceID,
		NetworkInterfaceID: netInterfaceID,
	}
}

func EqualStaticIPSpec(t1 *common.StaticIPSpec, t2 *common.StaticIPSpec) bool {
	return t1.ServiceUUID == t2.ServiceUUID &&
		t1.AvailableZone == t2.AvailableZone &&
		t1.ServerInstanceID == t2.ServerInstanceID &&
		t1.NetworkInterfaceID == t2.NetworkInterfaceID
}

func CopyStaticIPSpec(s *common.StaticIPSpec) *common.StaticIPSpec {
	return &common.StaticIPSpec{
		ServiceUUID:        s.ServiceUUID,
		AvailableZone:      s.AvailableZone,
		ServerInstanceID:   s.ServerInstanceID,
		NetworkInterfaceID: s.NetworkInterfaceID,
	}
}

func CreateServiceStaticIP(staticIP string, revision int64, spec *common.StaticIPSpec) *common.ServiceStaticIP {
	return &common.ServiceStaticIP{
		StaticIP: staticIP,
		Revision: revision,
		Spec:     *spec,
	}
}

func EqualServiceStaticIP(t1 *common.ServiceStaticIP, t2 *common.ServiceStaticIP) bool {
	return t1.StaticIP == t2.StaticIP &&
		t1.Revision == t2.Revision &&
		EqualStaticIPSpec(&t1.Spec, &t2.Spec)
}

func EqualServiceStaticIPImmutableFields(t1 *common.ServiceStaticIP, t2 *common.ServiceStaticIP) bool {
	return t1.StaticIP == t2.StaticIP &&
		t1.Spec.ServiceUUID == t2.Spec.ServiceUUID &&
		t1.Spec.AvailableZone == t2.Spec.AvailableZone
}

func UpdateServiceStaticIP(t1 *common.ServiceStaticIP, serverInstanceID string, netInterfaceID string) *common.ServiceStaticIP {
	newSpec := CreateStaticIPSpec(t1.Spec.ServiceUUID, t1.Spec.AvailableZone, serverInstanceID, netInterfaceID)
	return &common.ServiceStaticIP{
		StaticIP: t1.StaticIP,
		Revision: t1.Revision + 1,
		Spec:     *newSpec,
	}
}

func CopyServiceStaticIP(s *common.ServiceStaticIP) *common.ServiceStaticIP {
	return &common.ServiceStaticIP{
		StaticIP: s.StaticIP,
		Revision: s.Revision,
		Spec:     *CopyStaticIPSpec(&s.Spec),
	}
}
