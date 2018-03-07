package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/utils"
	"github.com/golang/glog"
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

func CreateInitialServiceAttr(serviceUUID string, replicas int64, cluster string,
	service string, vols common.ServiceVolumes, registerDNS bool, domain string,
	hostedZoneID string, requireStaticIP bool, userAttr *common.ServiceUserAttr,
	res common.Resources, serviceType string) *common.ServiceAttr {
	return &common.ServiceAttr{
		ServiceUUID:     serviceUUID,
		ServiceStatus:   common.ServiceStatusCreating,
		LastModified:    time.Now().UnixNano(),
		Replicas:        replicas,
		ClusterName:     cluster,
		ServiceName:     service,
		Volumes:         vols,
		RegisterDNS:     registerDNS,
		DomainName:      domain,
		HostedZoneID:    hostedZoneID,
		RequireStaticIP: requireStaticIP,
		UserAttr:        userAttr,
		Resource:        res,
		ServiceType:     serviceType,
	}
}

func CreateServiceAttr(serviceUUID string, status string, mtime int64, replicas int64,
	cluster string, service string, vols common.ServiceVolumes, registerDNS bool, domain string,
	hostedZoneID string, requireStaticIP bool, userAttr *common.ServiceUserAttr,
	res common.Resources, serviceType string) *common.ServiceAttr {
	return &common.ServiceAttr{
		ServiceUUID:     serviceUUID,
		ServiceStatus:   status,
		LastModified:    mtime,
		Replicas:        replicas,
		ClusterName:     cluster,
		ServiceName:     service,
		Volumes:         vols,
		RegisterDNS:     registerDNS,
		DomainName:      domain,
		HostedZoneID:    hostedZoneID,
		RequireStaticIP: requireStaticIP,
		UserAttr:        userAttr,
		Resource:        res,
		ServiceType:     serviceType,
	}
}

func EqualServiceAttr(t1 *common.ServiceAttr, t2 *common.ServiceAttr, skipMtime bool) bool {
	if t1.ServiceUUID == t2.ServiceUUID &&
		t1.ServiceStatus == t2.ServiceStatus &&
		(skipMtime || t1.LastModified == t2.LastModified) &&
		t1.Replicas == t2.Replicas &&
		t1.ClusterName == t2.ClusterName &&
		t1.ServiceName == t2.ServiceName &&
		EqualServiceVolumes(&(t1.Volumes), &(t2.Volumes)) &&
		t1.RegisterDNS == t2.RegisterDNS &&
		t1.DomainName == t2.DomainName &&
		t1.HostedZoneID == t2.HostedZoneID &&
		t1.RequireStaticIP == t2.RequireStaticIP &&
		EqualServiceUserAttr(t1.UserAttr, t2.UserAttr) &&
		EqualResources(&(t1.Resource), &(t2.Resource)) &&
		t1.ServiceType == t2.ServiceType {
		return true
	}
	return false
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

func EqualServiceUserAttr(u1 *common.ServiceUserAttr, u2 *common.ServiceUserAttr) bool {
	if u1 == nil || u2 == nil {
		if u1 == nil && u2 == nil {
			return true
		}
		return false
	}

	if u1.ServiceType != u2.ServiceType {
		return false
	}

	switch u1.ServiceType {
	case common.CatalogService_MongoDB:
		userAttr1 := &common.MongoDBUserAttr{}
		err := json.Unmarshal(u1.AttrBytes, userAttr1)
		if err != nil {
			glog.Errorln("Unmarshal mongodb user attr error", err, u1)
			return false
		}
		userAttr2 := &common.MongoDBUserAttr{}
		err = json.Unmarshal(u2.AttrBytes, userAttr2)
		if err != nil {
			glog.Errorln("Unmarshal mongodb user attr error", err, u2)
			return false
		}
		if userAttr1.Shards == userAttr2.Shards &&
			userAttr1.ReplicasPerShard == userAttr2.ReplicasPerShard &&
			userAttr1.ReplicaSetOnly == userAttr2.ReplicaSetOnly &&
			userAttr1.ConfigServers == userAttr2.ConfigServers &&
			userAttr1.KeyFileContent == userAttr2.KeyFileContent {
			return true
		}
		glog.Errorln("MongoDBUserAttr mismatch, u1:", userAttr1, ", u2:", userAttr2)
		return false

	case common.CatalogService_Cassandra:
		userAttr1 := &common.CasUserAttr{}
		err := json.Unmarshal(u1.AttrBytes, userAttr1)
		if err != nil {
			glog.Errorln("Unmarshal cassandra user attr error", err, u1)
			return false
		}
		userAttr2 := &common.CasUserAttr{}
		err = json.Unmarshal(u2.AttrBytes, userAttr2)
		if err != nil {
			glog.Errorln("Unmarshal cassandra user attr error", err, u2)
			return false
		}
		if userAttr1.HeapSizeMB == userAttr2.HeapSizeMB &&
			userAttr1.JmxRemoteUser == userAttr2.JmxRemoteUser &&
			userAttr1.JmxRemotePasswd == userAttr2.JmxRemotePasswd {
			return true
		}
		glog.Errorln("CasUserAttr mismatch, u1:", userAttr1, ", u2:", userAttr2)
		return false

	case common.CatalogService_PostgreSQL:
		ua1 := &common.PostgresUserAttr{}
		err := json.Unmarshal(u1.AttrBytes, ua1)
		if err != nil {
			glog.Errorln("Unmarshal user attr error", err, u1)
			return false
		}
		ua2 := &common.PostgresUserAttr{}
		err = json.Unmarshal(u2.AttrBytes, ua2)
		if err != nil {
			glog.Errorln("Unmarshal user attr error", err, u2)
			return false
		}
		if ua1.ContainerImage == ua2.ContainerImage {
			return true
		}
		glog.Errorln("PostgresUserAttr mismatch, u1:", ua1, ", u2:", ua2)
		return false

	case common.CatalogService_Redis:
		userAttr1 := &common.RedisUserAttr{}
		err := json.Unmarshal(u1.AttrBytes, userAttr1)
		if err != nil {
			glog.Errorln("Unmarshal redis user attr error", err, u1)
			return false
		}
		userAttr2 := &common.RedisUserAttr{}
		err = json.Unmarshal(u2.AttrBytes, userAttr2)
		if err != nil {
			glog.Errorln("Unmarshal redis user attr error", err, u2)
			return false
		}
		if userAttr1.Shards == userAttr2.Shards &&
			userAttr1.ReplicasPerShard == userAttr2.ReplicasPerShard &&
			userAttr1.MemoryCacheSizeMB == userAttr2.MemoryCacheSizeMB &&
			userAttr1.DisableAOF == userAttr2.DisableAOF &&
			userAttr1.AuthPass == userAttr2.AuthPass &&
			userAttr1.ReplTimeoutSecs == userAttr2.ReplTimeoutSecs &&
			userAttr1.MaxMemPolicy == userAttr2.MaxMemPolicy &&
			userAttr1.ConfigCmdName == userAttr2.ConfigCmdName {
			return true
		}
		glog.Errorln("RedisUserAttr mismatch, u1:", userAttr1, ", u2:", userAttr2)
		return false

	case common.CatalogService_ZooKeeper:
		ua1 := &common.ZKUserAttr{}
		err := json.Unmarshal(u1.AttrBytes, ua1)
		if err != nil {
			glog.Errorln("Unmarshal user attr error", err, u1)
			return false
		}
		ua2 := &common.ZKUserAttr{}
		err = json.Unmarshal(u2.AttrBytes, ua2)
		if err != nil {
			glog.Errorln("Unmarshal user attr error", err, u2)
			return false
		}
		if ua1.HeapSizeMB == ua2.HeapSizeMB &&
			ua1.JmxRemoteUser == ua2.JmxRemoteUser &&
			ua1.JmxRemotePasswd == ua2.JmxRemotePasswd {
			return true
		}
		glog.Errorln("ZKUserAttr mismatch, u1:", ua1, ", u2:", ua2)
		return false

	case common.CatalogService_Kafka:
		ua1 := &common.KafkaUserAttr{}
		err := json.Unmarshal(u1.AttrBytes, ua1)
		if err != nil {
			glog.Errorln("Unmarshal user attr error", err, u1)
			return false
		}
		ua2 := &common.KafkaUserAttr{}
		err = json.Unmarshal(u2.AttrBytes, ua2)
		if err != nil {
			glog.Errorln("Unmarshal user attr error", err, u2)
			return false
		}
		if ua1.HeapSizeMB == ua2.HeapSizeMB &&
			ua1.AllowTopicDel == ua2.AllowTopicDel &&
			ua1.RetentionHours == ua2.RetentionHours &&
			ua1.ZkServiceName == ua2.ZkServiceName &&
			ua1.JmxRemoteUser == ua2.JmxRemoteUser &&
			ua1.JmxRemotePasswd == ua2.JmxRemotePasswd {
			return true
		}
		glog.Errorln("KafkaUserAttr mismatch, u1:", ua1, ", u2:", ua2)
		return false

	case common.CatalogService_CouchDB:
		ua1 := &common.CouchDBUserAttr{}
		err := json.Unmarshal(u1.AttrBytes, ua1)
		if err != nil {
			glog.Errorln("Unmarshal user attr error", err, u1)
			return false
		}
		ua2 := &common.CouchDBUserAttr{}
		err = json.Unmarshal(u2.AttrBytes, ua2)
		if err != nil {
			glog.Errorln("Unmarshal user attr error", err, u2)
			return false
		}
		if ua1.Admin == ua2.Admin &&
			ua1.EncryptedPasswd == ua2.EncryptedPasswd &&
			ua1.EnableCors == ua2.EnableCors &&
			ua1.Credentials == ua2.Credentials &&
			ua1.Origins == ua2.Origins &&
			ua1.Headers == ua2.Headers &&
			ua1.Methods == ua2.Methods &&
			ua1.EnableSSL == ua2.EnableSSL {
			return true
		}
		glog.Errorln("CouchDBUserAttr mismatch, u1:", ua1, ", u2:", ua2)
		return false

	case common.CatalogService_Consul:
		ua1 := &common.ConsulUserAttr{}
		err := json.Unmarshal(u1.AttrBytes, ua1)
		if err != nil {
			glog.Errorln("Unmarshal user attr error", err, u1)
			return false
		}
		ua2 := &common.ConsulUserAttr{}
		err = json.Unmarshal(u2.AttrBytes, ua2)
		if err != nil {
			glog.Errorln("Unmarshal user attr error", err, u2)
			return false
		}
		if ua1.Datacenter == ua2.Datacenter &&
			ua1.Domain == ua2.Domain &&
			ua1.Encrypt == ua2.Encrypt &&
			ua1.EnableTLS == ua2.EnableTLS &&
			ua1.HTTPSPort == ua2.HTTPSPort {
			return true
		}
		glog.Errorln("ConsulUserAttr mismatch, u1:", ua1, ", u2:", ua2)
		return false

	case common.CatalogService_ElasticSearch:
		ua1 := &common.ESUserAttr{}
		err := json.Unmarshal(u1.AttrBytes, ua1)
		if err != nil {
			glog.Errorln("Unmarshal user attr error", err, u1)
			return false
		}
		ua2 := &common.ESUserAttr{}
		err = json.Unmarshal(u2.AttrBytes, ua2)
		if err != nil {
			glog.Errorln("Unmarshal user attr error", err, u2)
			return false
		}
		if ua1.HeapSizeMB == ua2.HeapSizeMB &&
			ua1.DedicatedMasters == ua2.DedicatedMasters &&
			ua1.DisableDedicatedMaster == ua2.DisableDedicatedMaster &&
			ua1.DisableForceAwareness == ua2.DisableForceAwareness {
			return true
		}
		glog.Errorln("ESUserAttr mismatch, u1:", ua1, ", u2:", ua2)
		return false

	case common.CatalogService_Kibana:
		ua1 := &common.KibanaUserAttr{}
		err := json.Unmarshal(u1.AttrBytes, ua1)
		if err != nil {
			glog.Errorln("Unmarshal user attr error", err, u1)
			return false
		}
		ua2 := &common.KibanaUserAttr{}
		err = json.Unmarshal(u2.AttrBytes, ua2)
		if err != nil {
			glog.Errorln("Unmarshal user attr error", err, u2)
			return false
		}
		if ua1.ESServiceName == ua2.ESServiceName &&
			ua1.ProxyBasePath == ua2.ProxyBasePath &&
			ua1.EnableSSL == ua2.EnableSSL {
			return true
		}
		glog.Errorln("KibanaUserAttr mismatch, u1:", ua1, ", u2:", ua2)
		return false

	case common.CatalogService_Logstash:
		ua1 := &common.LSUserAttr{}
		err := json.Unmarshal(u1.AttrBytes, ua1)
		if err != nil {
			glog.Errorln("Unmarshal user attr error", err, u1)
			return false
		}
		ua2 := &common.LSUserAttr{}
		err = json.Unmarshal(u2.AttrBytes, ua2)
		if err != nil {
			glog.Errorln("Unmarshal user attr error", err, u2)
			return false
		}
		if ua1.HeapSizeMB == ua2.HeapSizeMB &&
			ua1.ContainerImage == ua2.ContainerImage &&
			ua1.QueueType == ua2.QueueType &&
			ua1.EnableDeadLetterQueue == ua2.EnableDeadLetterQueue &&
			ua1.PipelineConfigs == ua2.PipelineConfigs &&
			ua1.PipelineWorkers == ua2.PipelineWorkers &&
			ua1.PipelineBatchSize == ua2.PipelineBatchSize &&
			ua1.PipelineBatchDelay == ua2.PipelineBatchDelay &&
			ua1.PipelineOutputWorkers == ua2.PipelineOutputWorkers {
			return true
		}
		glog.Errorln("LSUserAttr mismatch, u1:", ua1, ", u2:", ua2)
		return false

	default:
		return true
	}
}

func EqualServiceVolumes(v1 *common.ServiceVolumes, v2 *common.ServiceVolumes) bool {
	if v1.PrimaryDeviceName == v2.PrimaryDeviceName &&
		EqualServiceVolume(&(v1.PrimaryVolume), &(v2.PrimaryVolume)) &&
		v1.JournalDeviceName == v2.JournalDeviceName &&
		EqualServiceVolume(&(v1.JournalVolume), &(v2.JournalVolume)) {
		return true
	}
	return false
}

func EqualServiceVolume(v1 *common.ServiceVolume, v2 *common.ServiceVolume) bool {
	if v1.VolumeType == v2.VolumeType &&
		v1.Iops == v2.Iops &&
		v1.VolumeSizeGB == v2.VolumeSizeGB &&
		v1.Encrypted == v2.Encrypted {
		return true
	}
	return false
}

func CopyCasUserAttr(u1 *common.CasUserAttr) *common.CasUserAttr {
	return &common.CasUserAttr{
		HeapSizeMB:      u1.HeapSizeMB,
		JmxRemoteUser:   u1.JmxRemoteUser,
		JmxRemotePasswd: u1.JmxRemotePasswd,
	}
}

func CopyRedisUserAttr(u1 *common.RedisUserAttr) *common.RedisUserAttr {
	return &common.RedisUserAttr{
		Shards:            u1.Shards,
		ReplicasPerShard:  u1.ReplicasPerShard,
		MemoryCacheSizeMB: u1.MemoryCacheSizeMB,
		DisableAOF:        u1.DisableAOF,
		AuthPass:          u1.AuthPass,
		ReplTimeoutSecs:   u1.ReplTimeoutSecs,
		MaxMemPolicy:      u1.MaxMemPolicy,
		ConfigCmdName:     u1.ConfigCmdName,
	}
}

func CopyKafkaUserAttr(u1 *common.KafkaUserAttr) *common.KafkaUserAttr {
	return &common.KafkaUserAttr{
		HeapSizeMB:      u1.HeapSizeMB,
		AllowTopicDel:   u1.AllowTopicDel,
		RetentionHours:  u1.RetentionHours,
		ZkServiceName:   u1.ZkServiceName,
		JmxRemoteUser:   u1.JmxRemoteUser,
		JmxRemotePasswd: u1.JmxRemotePasswd,
	}
}

func UpdateServiceStatus(t1 *common.ServiceAttr, status string) *common.ServiceAttr {
	return &common.ServiceAttr{
		ServiceUUID:     t1.ServiceUUID,
		ServiceStatus:   status,
		LastModified:    time.Now().UnixNano(),
		Replicas:        t1.Replicas,
		ClusterName:     t1.ClusterName,
		ServiceName:     t1.ServiceName,
		Volumes:         t1.Volumes,
		RegisterDNS:     t1.RegisterDNS,
		DomainName:      t1.DomainName,
		HostedZoneID:    t1.HostedZoneID,
		RequireStaticIP: t1.RequireStaticIP,
		UserAttr:        t1.UserAttr,
		Resource:        t1.Resource,
		ServiceType:     t1.ServiceType,
	}
}

func UpdateServiceReplicas(t1 *common.ServiceAttr, replicas int64) *common.ServiceAttr {
	return &common.ServiceAttr{
		ServiceUUID:     t1.ServiceUUID,
		ServiceStatus:   t1.ServiceStatus,
		LastModified:    time.Now().UnixNano(),
		Replicas:        replicas,
		ClusterName:     t1.ClusterName,
		ServiceName:     t1.ServiceName,
		Volumes:         t1.Volumes,
		RegisterDNS:     t1.RegisterDNS,
		DomainName:      t1.DomainName,
		HostedZoneID:    t1.HostedZoneID,
		RequireStaticIP: t1.RequireStaticIP,
		UserAttr:        t1.UserAttr,
		Resource:        t1.Resource,
		ServiceType:     t1.ServiceType,
	}
}

func UpdateServiceUserAttr(t1 *common.ServiceAttr, ua *common.ServiceUserAttr) *common.ServiceAttr {
	return &common.ServiceAttr{
		ServiceUUID:     t1.ServiceUUID,
		ServiceStatus:   t1.ServiceStatus,
		LastModified:    time.Now().UnixNano(),
		Replicas:        t1.Replicas,
		ClusterName:     t1.ClusterName,
		ServiceName:     t1.ServiceName,
		Volumes:         t1.Volumes,
		RegisterDNS:     t1.RegisterDNS,
		DomainName:      t1.DomainName,
		HostedZoneID:    t1.HostedZoneID,
		RequireStaticIP: t1.RequireStaticIP,
		UserAttr:        ua,
		Resource:        t1.Resource,
		ServiceType:     t1.ServiceType,
	}
}

func CreateInitialServiceMember(serviceUUID string, memberIndex int64, memberName string, az string,
	vols common.MemberVolumes, staticIP string, configs []*common.MemberConfig) *common.ServiceMember {
	return &common.ServiceMember{
		ServiceUUID:         serviceUUID,
		MemberIndex:         memberIndex,
		Status:              common.ServiceMemberStatusActive,
		MemberName:          memberName,
		AvailableZone:       az,
		TaskID:              DefaultTaskID,
		ContainerInstanceID: DefaultContainerInstanceID,
		ServerInstanceID:    DefaultServerInstanceID,
		LastModified:        time.Now().UnixNano(),
		Volumes:             vols,
		StaticIP:            staticIP,
		Configs:             configs,
	}
}

func CreateServiceMember(serviceUUID string, memberIndex int64, status string, memberName string,
	az string, taskID string, containerInstanceID string, ec2InstanceID string, mtime int64,
	vols common.MemberVolumes, staticIP string, configs []*common.MemberConfig) *common.ServiceMember {
	return &common.ServiceMember{
		ServiceUUID:         serviceUUID,
		MemberIndex:         memberIndex,
		Status:              status,
		MemberName:          memberName,
		AvailableZone:       az,
		TaskID:              taskID,
		ContainerInstanceID: containerInstanceID,
		ServerInstanceID:    ec2InstanceID,
		LastModified:        mtime,
		Volumes:             vols,
		StaticIP:            staticIP,
		Configs:             configs,
	}
}

func EqualServiceMember(t1 *common.ServiceMember, t2 *common.ServiceMember, skipMtime bool) bool {
	if t1.ServiceUUID == t2.ServiceUUID &&
		t1.MemberIndex == t2.MemberIndex &&
		t1.Status == t2.Status &&
		t1.MemberName == t2.MemberName &&
		t1.AvailableZone == t2.AvailableZone &&
		t1.TaskID == t2.TaskID &&
		t1.ContainerInstanceID == t2.ContainerInstanceID &&
		t1.ServerInstanceID == t2.ServerInstanceID &&
		(skipMtime || t1.LastModified == t2.LastModified) &&
		EqualMemberVolumes(&(t1.Volumes), &(t2.Volumes)) &&
		t1.StaticIP == t2.StaticIP &&
		equalConfigs(t1.Configs, t2.Configs) {
		return true
	}
	return false
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

func equalConfigs(c1 []*common.MemberConfig, c2 []*common.MemberConfig) bool {
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

func CopyMemberConfigs(c1 []*common.MemberConfig) []*common.MemberConfig {
	c2 := make([]*common.MemberConfig, len(c1))
	for i, c := range c1 {
		c2[i] = &common.MemberConfig{
			FileName: c.FileName,
			FileID:   c.FileID,
			FileMD5:  c.FileMD5,
		}
	}
	return c2
}

func UpdateServiceMemberConfigs(t1 *common.ServiceMember, c []*common.MemberConfig) *common.ServiceMember {
	return &common.ServiceMember{
		ServiceUUID:         t1.ServiceUUID,
		MemberIndex:         t1.MemberIndex,
		Status:              t1.Status,
		MemberName:          t1.MemberName,
		AvailableZone:       t1.AvailableZone,
		TaskID:              t1.TaskID,
		ContainerInstanceID: t1.ContainerInstanceID,
		ServerInstanceID:    t1.ServerInstanceID,
		LastModified:        time.Now().UnixNano(),
		Volumes:             t1.Volumes,
		StaticIP:            t1.StaticIP,
		Configs:             c,
	}
}

func UpdateServiceMemberOwner(t1 *common.ServiceMember, taskID string, containerInstanceID string, ec2InstanceID string) *common.ServiceMember {
	return &common.ServiceMember{
		ServiceUUID:         t1.ServiceUUID,
		MemberIndex:         t1.MemberIndex,
		Status:              t1.Status,
		MemberName:          t1.MemberName,
		AvailableZone:       t1.AvailableZone,
		TaskID:              taskID,
		ContainerInstanceID: containerInstanceID,
		ServerInstanceID:    ec2InstanceID,
		LastModified:        time.Now().UnixNano(),
		Volumes:             t1.Volumes,
		StaticIP:            t1.StaticIP,
		Configs:             t1.Configs,
	}
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

func PrintConfigFile(cfg *common.ConfigFile) string {
	return fmt.Sprintf("serviceUUID %s fileID %s fileName %s fileMD5 %s fileMode %d LastModified %d",
		cfg.ServiceUUID, cfg.FileID, cfg.FileName, cfg.FileMD5, cfg.FileMode, cfg.LastModified)
}

func CreateServiceStaticIP(staticIP string, serviceUUID string,
	az string, serverInstanceID string, netInterfaceID string) *common.ServiceStaticIP {
	return &common.ServiceStaticIP{
		StaticIP:           staticIP,
		ServiceUUID:        serviceUUID,
		AvailableZone:      az,
		ServerInstanceID:   serverInstanceID,
		NetworkInterfaceID: netInterfaceID,
	}
}

func EqualServiceStaticIP(t1 *common.ServiceStaticIP, t2 *common.ServiceStaticIP) bool {
	if t1.StaticIP == t2.StaticIP &&
		t1.ServiceUUID == t2.ServiceUUID &&
		t1.AvailableZone == t2.AvailableZone &&
		t1.ServerInstanceID == t2.ServerInstanceID &&
		t1.NetworkInterfaceID == t2.NetworkInterfaceID {
		return true
	}
	return false
}

func UpdateServiceStaticIP(t1 *common.ServiceStaticIP, serverInstanceID string, netInterfaceID string) *common.ServiceStaticIP {
	return &common.ServiceStaticIP{
		StaticIP:           t1.StaticIP,
		ServiceUUID:        t1.ServiceUUID,
		AvailableZone:      t1.AvailableZone,
		ServerInstanceID:   serverInstanceID,
		NetworkInterfaceID: netInterfaceID,
	}
}
