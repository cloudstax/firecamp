package controldb

import (
	"fmt"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/db"
	pb "github.com/cloudstax/firecamp/db/controldb/protocols"
)

func GenPbDevice(dev *common.Device) *pb.Device {
	pbdev := &pb.Device{
		ClusterName: dev.ClusterName,
		DeviceName:  dev.DeviceName,
		ServiceName: dev.ServiceName,
	}
	return pbdev
}

func GenDbDevice(dev *pb.Device) *common.Device {
	dbdev := db.CreateDevice(dev.ClusterName, dev.DeviceName, dev.ServiceName)
	return dbdev
}

func EqualDevice(a1 *pb.Device, a2 *pb.Device) bool {
	if a1.ClusterName == a2.ClusterName &&
		a1.DeviceName == a2.DeviceName &&
		a1.ServiceName == a2.ServiceName {
		return true
	}
	return false
}

func CopyDevice(a1 *pb.Device) *pb.Device {
	a2 := &pb.Device{
		ClusterName: a1.ClusterName,
		DeviceName:  a1.DeviceName,
		ServiceName: a1.ServiceName,
	}
	return a2
}

func GenPbService(service *common.Service) *pb.Service {
	pbservice := &pb.Service{
		ClusterName: service.ClusterName,
		ServiceName: service.ServiceName,
		ServiceUUID: service.ServiceUUID,
	}
	return pbservice
}

func GenDbService(service *pb.Service) *common.Service {
	dbservice := db.CreateService(service.ClusterName,
		service.ServiceName,
		service.ServiceUUID)
	return dbservice
}

func EqualService(a1 *pb.Service, a2 *pb.Service) bool {
	if a1.ClusterName == a2.ClusterName &&
		a1.ServiceName == a2.ServiceName &&
		a1.ServiceUUID == a2.ServiceUUID {
		return true
	}
	return false
}

func GenPbServiceAttr(attr *common.ServiceAttr) *pb.ServiceAttr {
	pbAttr := &pb.ServiceAttr{
		ServiceUUID:     attr.ServiceUUID,
		ServiceStatus:   attr.ServiceStatus,
		LastModified:    attr.LastModified,
		Replicas:        attr.Replicas,
		VolumeSizeGB:    attr.VolumeSizeGB,
		ClusterName:     attr.ClusterName,
		ServiceName:     attr.ServiceName,
		DeviceNames:     GenPbServiceDeviceNames(&(attr.DeviceNames)),
		RegisterDNS:     attr.RegisterDNS,
		DomainName:      attr.DomainName,
		HostedZoneID:    attr.HostedZoneID,
		RequireStaticIP: attr.RequireStaticIP,
	}
	return pbAttr
}

func GenDbServiceAttr(attr *pb.ServiceAttr) *common.ServiceAttr {
	dbAttr := db.CreateServiceAttr(attr.ServiceUUID,
		attr.ServiceStatus,
		attr.LastModified,
		attr.Replicas,
		attr.VolumeSizeGB,
		attr.ClusterName,
		attr.ServiceName,
		GenDbServiceDeviceNames(attr.DeviceNames),
		attr.RegisterDNS,
		attr.DomainName,
		attr.HostedZoneID,
		attr.RequireStaticIP)
	return dbAttr
}

func GenDbServiceDeviceNames(devNames *pb.ServiceDeviceNames) common.ServiceDeviceNames {
	return common.ServiceDeviceNames{
		PrimaryDeviceName: devNames.PrimaryDeviceName,
		LogDeviceName:     devNames.LogDeviceName,
	}
}

func GenPbServiceDeviceNames(devNames *common.ServiceDeviceNames) *pb.ServiceDeviceNames {
	return &pb.ServiceDeviceNames{
		PrimaryDeviceName: devNames.PrimaryDeviceName,
		LogDeviceName:     devNames.LogDeviceName,
	}
}

func EqualAttr(a1 *pb.ServiceAttr, a2 *pb.ServiceAttr, skipMtime bool) bool {
	if a1.ServiceUUID == a2.ServiceUUID &&
		a1.ServiceStatus == a2.ServiceStatus &&
		(skipMtime || a1.LastModified == a2.LastModified) &&
		a1.Replicas == a2.Replicas &&
		a1.VolumeSizeGB == a2.VolumeSizeGB &&
		a1.ClusterName == a2.ClusterName &&
		a1.ServiceName == a2.ServiceName &&
		a1.DeviceNames.PrimaryDeviceName == a2.DeviceNames.PrimaryDeviceName &&
		a1.DeviceNames.LogDeviceName == a2.DeviceNames.LogDeviceName &&
		a1.RegisterDNS == a2.RegisterDNS &&
		a1.DomainName == a2.DomainName &&
		a1.HostedZoneID == a2.HostedZoneID &&
		a1.RequireStaticIP == a2.RequireStaticIP {
		return true
	}
	return false
}

func GenPbMemberConfig(cfgs []*common.MemberConfig) []*pb.MemberConfig {
	if len(cfgs) == 0 {
		return nil
	}

	pbcfgs := make([]*pb.MemberConfig, len(cfgs))
	for i, cfg := range cfgs {
		pbcfgs[i] = &pb.MemberConfig{
			FileName: cfg.FileName,
			FileID:   cfg.FileID,
			FileMD5:  cfg.FileMD5,
		}
	}
	return pbcfgs
}

func GenPbServiceMember(member *common.ServiceMember) *pb.ServiceMember {
	pbmember := &pb.ServiceMember{
		ServiceUUID:         member.ServiceUUID,
		MemberName:          member.MemberName,
		AvailableZone:       member.AvailableZone,
		TaskID:              member.TaskID,
		ContainerInstanceID: member.ContainerInstanceID,
		ServerInstanceID:    member.ServerInstanceID,
		LastModified:        member.LastModified,
		VolumeID:            member.VolumeID,
		DeviceName:          member.DeviceName,
		StaticIP:            member.StaticIP,
		Configs:             GenPbMemberConfig(member.Configs),
	}
	return pbmember
}

func GenDbMemberConfig(cfgs []*pb.MemberConfig) []*common.MemberConfig {
	if len(cfgs) == 0 {
		return nil
	}

	dbcfgs := make([]*common.MemberConfig, len(cfgs))
	for i, cfg := range cfgs {
		dbcfgs[i] = &common.MemberConfig{
			FileName: cfg.FileName,
			FileID:   cfg.FileID,
			FileMD5:  cfg.FileMD5,
		}
	}
	return dbcfgs
}

func GenDbServiceMember(member *pb.ServiceMember) *common.ServiceMember {
	dbmember := db.CreateServiceMember(member.ServiceUUID,
		member.MemberName,
		member.AvailableZone,
		member.TaskID,
		member.ContainerInstanceID,
		member.ServerInstanceID,
		member.LastModified,
		member.VolumeID,
		member.DeviceName,
		member.StaticIP,
		GenDbMemberConfig(member.Configs))
	return dbmember
}

func EqualMemberConfig(c1 []*pb.MemberConfig, c2 []*pb.MemberConfig) bool {
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

func EqualServiceMember(a1 *pb.ServiceMember, a2 *pb.ServiceMember, skipMtime bool) bool {
	if a1.ServiceUUID == a2.ServiceUUID &&
		a1.MemberName == a2.MemberName &&
		a1.AvailableZone == a2.AvailableZone &&
		a1.TaskID == a2.TaskID &&
		a1.ContainerInstanceID == a2.ContainerInstanceID &&
		a1.ServerInstanceID == a2.ServerInstanceID &&
		(skipMtime || a1.LastModified == a2.LastModified) &&
		a1.VolumeID == a2.VolumeID &&
		a1.DeviceName == a2.DeviceName &&
		a1.StaticIP == a2.StaticIP &&
		EqualMemberConfig(a1.Configs, a2.Configs) {
		return true
	}
	return false
}

func GenPbConfigFile(cfg *common.ConfigFile) *pb.ConfigFile {
	return &pb.ConfigFile{
		ServiceUUID:  cfg.ServiceUUID,
		FileID:       cfg.FileID,
		FileMD5:      cfg.FileMD5,
		FileName:     cfg.FileName,
		FileMode:     cfg.FileMode,
		LastModified: cfg.LastModified,
		Content:      cfg.Content,
	}
}

func GenDbConfigFile(cfg *pb.ConfigFile) *common.ConfigFile {
	return &common.ConfigFile{
		ServiceUUID:  cfg.ServiceUUID,
		FileID:       cfg.FileID,
		FileMD5:      cfg.FileMD5,
		FileName:     cfg.FileName,
		FileMode:     cfg.FileMode,
		LastModified: cfg.LastModified,
		Content:      cfg.Content,
	}
}

func EqualConfigFile(a1 *pb.ConfigFile, a2 *pb.ConfigFile, skipMtime bool, skipContent bool) bool {
	if a1.ServiceUUID == a2.ServiceUUID &&
		a1.FileID == a2.FileID &&
		a1.FileMD5 == a2.FileMD5 &&
		a1.FileName == a2.FileName &&
		a1.FileMode == a2.FileMode &&
		(skipMtime || a1.LastModified == a2.LastModified) &&
		(skipContent || a1.Content == a2.Content) {
		return true
	}
	return false
}

func CopyMemberConfig(a1 []*pb.MemberConfig) []*pb.MemberConfig {
	if len(a1) == 0 {
		return nil
	}

	cfgs := make([]*pb.MemberConfig, len(a1))
	for i, cfg := range a1 {
		cfgs[i] = &pb.MemberConfig{
			FileName: cfg.FileName,
			FileID:   cfg.FileID,
			FileMD5:  cfg.FileMD5,
		}
	}
	return cfgs
}

func CopyServiceMember(a1 *pb.ServiceMember) *pb.ServiceMember {
	return &pb.ServiceMember{
		ServiceUUID:         a1.ServiceUUID,
		MemberName:          a1.MemberName,
		AvailableZone:       a1.AvailableZone,
		TaskID:              a1.TaskID,
		ContainerInstanceID: a1.ContainerInstanceID,
		ServerInstanceID:    a1.ServerInstanceID,
		LastModified:        a1.LastModified,
		VolumeID:            a1.VolumeID,
		DeviceName:          a1.DeviceName,
		StaticIP:            a1.StaticIP,
		Configs:             CopyMemberConfig(a1.Configs),
	}
}

func CopyConfigFile(cfg *pb.ConfigFile) *pb.ConfigFile {
	return &pb.ConfigFile{
		ServiceUUID:  cfg.ServiceUUID,
		FileID:       cfg.FileID,
		FileMD5:      cfg.FileMD5,
		FileName:     cfg.FileName,
		FileMode:     cfg.FileMode,
		LastModified: cfg.LastModified,
		Content:      cfg.Content,
	}
}

func PrintConfigFile(cfg *pb.ConfigFile) string {
	return fmt.Sprintf("serviceUUID %s fileID %s fileName %s fileMD5 %s fileMode %d LastModified %d",
		cfg.ServiceUUID, cfg.FileID, cfg.FileName, cfg.FileMD5, cfg.FileMode, cfg.LastModified)
}

func GenPbServiceStaticIP(serviceip *common.ServiceStaticIP) *pb.ServiceStaticIP {
	return &pb.ServiceStaticIP{
		StaticIP:           serviceip.StaticIP,
		ServiceUUID:        serviceip.ServiceUUID,
		AvailableZone:      serviceip.AvailableZone,
		ServerInstanceID:   serviceip.ServerInstanceID,
		NetworkInterfaceID: serviceip.NetworkInterfaceID,
	}
}

func GenDbServiceStaticIP(serviceip *pb.ServiceStaticIP) *common.ServiceStaticIP {
	return &common.ServiceStaticIP{
		StaticIP:           serviceip.StaticIP,
		ServiceUUID:        serviceip.ServiceUUID,
		AvailableZone:      serviceip.AvailableZone,
		ServerInstanceID:   serviceip.ServerInstanceID,
		NetworkInterfaceID: serviceip.NetworkInterfaceID,
	}
}

func EqualServiceStaticIP(a1 *pb.ServiceStaticIP, a2 *pb.ServiceStaticIP) bool {
	if a1.StaticIP == a2.StaticIP &&
		a1.ServiceUUID == a2.ServiceUUID &&
		a1.AvailableZone == a2.AvailableZone &&
		a1.ServerInstanceID == a2.ServerInstanceID &&
		a1.NetworkInterfaceID == a2.NetworkInterfaceID {
		return true
	}
	return false
}

func CopyServiceStaticIP(serviceip *pb.ServiceStaticIP) *pb.ServiceStaticIP {
	return &pb.ServiceStaticIP{
		StaticIP:           serviceip.StaticIP,
		ServiceUUID:        serviceip.ServiceUUID,
		AvailableZone:      serviceip.AvailableZone,
		ServerInstanceID:   serviceip.ServerInstanceID,
		NetworkInterfaceID: serviceip.NetworkInterfaceID,
	}
}
