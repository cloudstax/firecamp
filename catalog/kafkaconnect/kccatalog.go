package kccatalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/golang/glog"

	"github.com/cloudstax/firecamp/catalog"
	"github.com/cloudstax/firecamp/catalog/kafka"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/containersvc"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/log"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
)

const (
	defaultVersion = "4.0"
	// ContainerImage is the main running container.
	ContainerImage           = common.ContainerNamePrefix + "kafka-connect:" + defaultVersion
	SinkESInitContainerImage = common.ContainerNamePrefix + "kafka-sink-elasticsearch-init:" + defaultVersion

	// connector listen port for managing the connector
	connectRestPort = 8083

	DefaultHeapMB = 1024

	JSON_CONVERTER             = "org.apache.kafka.connect.json.JsonConverter"
	CONFIG_NAME_SUFFIX         = "config"
	OFFSET_NAME_SUFFIX         = "offset"
	STATUS_NAME_SUFFIX         = "status"
	DEFAULT_REPLICATION_FACTOR = uint(3)
	MAX_REPLICATION_FACTOR     = uint(7)
	// by default, only one task per connector
	DEFAULT_MAX_TASKS = 1
	// The default ElasticSearch type.
	DEFAULT_TYPE_NAME = "kafka-connect"

	ENV_CONNECT_REST_PORT                               = "CONNECT_REST_PORT"
	ENV_CONNECT_BOOTSTRAP_SERVERS                       = "CONNECT_BOOTSTRAP_SERVERS"
	ENV_CONNECT_GROUP_ID                                = "CONNECT_GROUP_ID"
	ENV_CONNECT_CONFIG_STORAGE_TOPIC                    = "CONNECT_CONFIG_STORAGE_TOPIC"
	ENV_CONNECT_OFFSET_STORAGE_TOPIC                    = "CONNECT_OFFSET_STORAGE_TOPIC"
	ENV_CONNECT_STATUS_STORAGE_TOPIC                    = "CONNECT_STATUS_STORAGE_TOPIC"
	ENV_CONNECT_CONFIG_STORAGE_REPLICATION_FACTOR       = "CONNECT_CONFIG_STORAGE_REPLICATION_FACTOR"
	ENV_CONNECT_OFFSET_STORAGE_REPLICATION_FACTOR       = "CONNECT_OFFSET_STORAGE_REPLICATION_FACTOR"
	ENV_CONNECT_STATUS_STORAGE_REPLICATION_FACTOR       = "CONNECT_STATUS_STORAGE_REPLICATION_FACTOR"
	ENV_CONNECT_KEY_CONVERTER                           = "CONNECT_KEY_CONVERTER"
	ENV_CONNECT_VALUE_CONVERTER                         = "CONNECT_VALUE_CONVERTER"
	ENV_CONNECT_KEY_CONVERTER_SCHEMAS_ENABLE            = "CONNECT_KEY_CONVERTER_SCHEMAS_ENABLE"
	ENV_CONNECT_VALUE_CONVERTER_SCHEMAS_ENABLE          = "CONNECT_VALUE_CONVERTER_SCHEMAS_ENABLE"
	ENV_CONNECT_INTERNAL_KEY_CONVERTER                  = "CONNECT_INTERNAL_KEY_CONVERTER"
	ENV_CONNECT_INTERNAL_VALUE_CONVERTER                = "CONNECT_INTERNAL_VALUE_CONVERTER"
	ENV_CONNECT_INTERNAL_KEY_CONVERTER_SCHEMAS_ENABLE   = "CONNECT_INTERNAL_KEY_CONVERTER_SCHEMAS_ENABLE"
	ENV_CONNECT_INTERNAL_VALUE_CONVERTER_SCHEMAS_ENABLE = "CONNECT_INTERNAL_VALUE_CONVERTER_SCHEMAS_ENABLE"
	ENV_CONNECT_PLUGIN_PATH                             = "CONNECT_PLUGIN_PATH"
	defaultConnectPluginPath                            = "/usr/share/java"

	ENV_CONNECTOR_NAME  = "CONNECTOR_NAME"
	ENV_CONNECTOR_HOSTS = "CONNECTOR_HOSTS"
	ENV_CONNECTOR_PORT  = "CONNECTOR_PORT"

	ENV_ELASTICSEARCH_CONFIGS = "ELASTICSEARCH_CONFIGS"
)

// The Kafka Connect catalog service

// ValidateSinkESRequest checks if the request is valid
func ValidateSinkESRequest(req *manage.CatalogCreateKafkaSinkESRequest) error {
	opts := req.Options
	if opts.Replicas <= 0 {
		return errors.New("Please specify the valid replicas")
	}
	if opts.HeapSizeMB <= 0 {
		return errors.New("Please specify the valid heap size")
	}
	if (opts.ReplFactor % 2) == 0 {
		return errors.New("The replication factor should be odd")
	}
	if opts.ReplFactor > MAX_REPLICATION_FACTOR {
		return fmt.Errorf("The max replication factor is %d", MAX_REPLICATION_FACTOR)
	}
	if req.Resource.MaxMemMB != common.DefaultMaxMemoryMB &&
		(req.Resource.MaxMemMB < req.Resource.ReserveMemMB || req.Resource.MaxMemMB < opts.HeapSizeMB) {
		return errors.New("The ")
	}

	return nil
}

// GenCreateESSinkServiceRequest returns the creation request for the kafka elasticsearch sink service.
func GenCreateESSinkServiceRequest(platform string, region string, cluster string, service string,
	kafkaAttr *common.ServiceAttr, opts *manage.CatalogKafkaSinkESOptions, res *common.Resources) (*manage.CreateServiceRequest, *common.KCSinkESUserAttr, error) {

	kafkaServers := catalog.GenServiceMemberHostsWithPort(kafkaAttr.ClusterName, kafkaAttr.ServiceName, kafkaAttr.Replicas, kafkacatalog.ListenPort)

	groupID := fmt.Sprintf("%s-%s", cluster, service)
	configTopic := fmt.Sprintf("%s-%s", groupID, CONFIG_NAME_SUFFIX)
	offsetTopic := fmt.Sprintf("%s-%s", groupID, OFFSET_NAME_SUFFIX)
	statusTopic := fmt.Sprintf("%s-%s", groupID, STATUS_NAME_SUFFIX)

	replFactor := DEFAULT_REPLICATION_FACTOR
	if opts.ReplFactor > 0 {
		replFactor = opts.ReplFactor
	}

	// TODO set the connector heap size

	envkvs := []*common.EnvKeyValuePair{
		// general env variables
		&common.EnvKeyValuePair{Name: common.ENV_REGION, Value: region},
		&common.EnvKeyValuePair{Name: common.ENV_CLUSTER, Value: cluster},
		&common.EnvKeyValuePair{Name: common.ENV_SERVICE_NAME, Value: service},
		&common.EnvKeyValuePair{Name: common.ENV_CONTAINER_PLATFORM, Value: platform},
		&common.EnvKeyValuePair{Name: common.ENV_DB_TYPE, Value: common.DBTypeCloudDB},
		// kafka connect env configs
		&common.EnvKeyValuePair{Name: ENV_CONNECT_BOOTSTRAP_SERVERS, Value: kafkaServers},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_REST_PORT, Value: strconv.Itoa(connectRestPort)},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_GROUP_ID, Value: groupID},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_CONFIG_STORAGE_TOPIC, Value: configTopic},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_OFFSET_STORAGE_TOPIC, Value: offsetTopic},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_STATUS_STORAGE_TOPIC, Value: statusTopic},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_CONFIG_STORAGE_REPLICATION_FACTOR, Value: strconv.Itoa(int(replFactor))},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_OFFSET_STORAGE_REPLICATION_FACTOR, Value: strconv.Itoa(int(replFactor))},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_STATUS_STORAGE_REPLICATION_FACTOR, Value: strconv.Itoa(int(replFactor))},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_KEY_CONVERTER, Value: JSON_CONVERTER},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_VALUE_CONVERTER, Value: JSON_CONVERTER},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_KEY_CONVERTER_SCHEMAS_ENABLE, Value: "false"},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_VALUE_CONVERTER_SCHEMAS_ENABLE, Value: "false"},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_INTERNAL_KEY_CONVERTER, Value: JSON_CONVERTER},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_INTERNAL_VALUE_CONVERTER, Value: JSON_CONVERTER},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_INTERNAL_KEY_CONVERTER_SCHEMAS_ENABLE, Value: "false"},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_INTERNAL_VALUE_CONVERTER_SCHEMAS_ENABLE, Value: "false"},
		&common.EnvKeyValuePair{Name: ENV_CONNECT_PLUGIN_PATH, Value: defaultConnectPluginPath},
	}

	typeName := opts.TypeName
	if len(typeName) == 0 {
		typeName = DEFAULT_TYPE_NAME
	}
	userAttr := &common.KCSinkESUserAttr{
		HeapSizeMB:       opts.HeapSizeMB,
		KafkaServiceName: opts.KafkaServiceName,
		Topic:            opts.Topic,
		ESServiceName:    opts.ESServiceName,
		TypeName:         typeName,
		ConfigReplFactor: replFactor,
		OffsetReplFactor: replFactor,
		StatusReplFactor: replFactor,
	}
	b, err := json.Marshal(userAttr)
	if err != nil {
		glog.Errorln("Marshal UserAttr error", err, opts)
		return nil, nil, err
	}

	reserveMemMB := res.ReserveMemMB
	if res.ReserveMemMB < opts.HeapSizeMB {
		reserveMemMB = opts.HeapSizeMB
	}

	replicaCfgs := GenReplicaConfigs(platform, cluster, service, opts)

	req := &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     cluster,
			ServiceName: service,
			ServiceType: common.ServiceTypeStateless,
		},

		Resource: &common.Resources{
			MaxCPUUnits:     res.MaxCPUUnits,
			ReserveCPUUnits: res.ReserveCPUUnits,
			MaxMemMB:        res.MaxMemMB,
			ReserveMemMB:    reserveMemMB,
		},

		ContainerImage: ContainerImage,
		Replicas:       opts.Replicas,
		Envkvs:         envkvs,
		RegisterDNS:    true,
		ReplicaConfigs: replicaCfgs,

		UserAttr: &common.ServiceUserAttr{
			ServiceType: common.CatalogService_KafkaSinkES,
			AttrBytes:   b,
		},
	}
	return req, userAttr, nil
}

// GenReplicaConfigs generates the replica configs.
func GenReplicaConfigs(platform string, cluster string, service string, opts *manage.CatalogKafkaSinkESOptions) []*manage.ReplicaConfig {
	domain := dns.GenDefaultDomainName(cluster)

	replicaCfgs := make([]*manage.ReplicaConfig, opts.Replicas)
	for i := 0; i < int(opts.Replicas); i++ {
		// create the sys.conf file
		member := utils.GenServiceMemberName(service, int64(i))
		memberHost := dns.GenDNSName(member, domain)
		sysCfg := catalog.CreateSysConfigFile(platform, memberHost)

		configs := []*manage.ReplicaConfigFile{sysCfg}

		replicaCfg := &manage.ReplicaConfig{Zone: common.AnyAvailabilityZone, MemberName: member, Configs: configs}
		replicaCfgs[i] = replicaCfg
	}
	return replicaCfgs
}

// GenSinkESServiceInitRequest creates the init request for elasticsearch sink connector.
func GenSinkESServiceInitRequest(req *manage.ServiceCommonRequest, logConfig *cloudlog.LogConfig,
	serviceUUID string, replicas int64, ua *common.KCSinkESUserAttr, esURIs string, manageurl string) *containersvc.RunTaskOptions {

	kvregion := &common.EnvKeyValuePair{Name: common.ENV_REGION, Value: req.Region}
	kvcluster := &common.EnvKeyValuePair{Name: common.ENV_CLUSTER, Value: req.Cluster}
	kvmgtserver := &common.EnvKeyValuePair{Name: common.ENV_MANAGE_SERVER_URL, Value: manageurl}
	kvop := &common.EnvKeyValuePair{Name: common.ENV_OP, Value: manage.CatalogSetServiceInitOp}

	kvservice := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_NAME, Value: req.ServiceName}
	kvsvctype := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_TYPE, Value: common.CatalogService_KafkaSinkES}

	connectorHosts := catalog.GenServiceMemberHosts(req.Cluster, req.ServiceName, replicas)
	kvhosts := &common.EnvKeyValuePair{Name: ENV_CONNECTOR_HOSTS, Value: connectorHosts}
	kvport := &common.EnvKeyValuePair{Name: ENV_CONNECTOR_PORT, Value: strconv.Itoa(connectRestPort)}

	name := fmt.Sprintf("%s-%s", req.Cluster, req.ServiceName)
	configs := fmt.Sprintf(sinkESConfigs, name, DEFAULT_MAX_TASKS, ua.Topic, ua.TypeName, esURIs)
	kvconfigs := &common.EnvKeyValuePair{Name: ENV_ELASTICSEARCH_CONFIGS, Value: configs}
	kvname := &common.EnvKeyValuePair{Name: ENV_CONNECTOR_NAME, Value: name}

	envkvs := []*common.EnvKeyValuePair{kvregion, kvcluster, kvmgtserver, kvop, kvservice, kvsvctype, kvhosts, kvport, kvconfigs, kvname}

	commonOpts := &containersvc.CommonOptions{
		Cluster:        req.Cluster,
		ServiceName:    req.ServiceName,
		ServiceUUID:    serviceUUID,
		ContainerImage: SinkESInitContainerImage,
		Resource: &common.Resources{
			MaxCPUUnits:     common.DefaultMaxCPUUnits,
			ReserveCPUUnits: common.DefaultReserveCPUUnits,
			MaxMemMB:        common.DefaultMaxMemoryMB,
			ReserveMemMB:    common.DefaultReserveMemoryMB,
		},
		LogConfig: logConfig,
	}

	return &containersvc.RunTaskOptions{
		Common:   commonOpts,
		TaskType: common.TaskTypeInit,
		Envkvs:   envkvs,
	}
}

const (
	sinkESConfigs = `{"name": "%s", "config": {"connector.class":"io.confluent.connect.elasticsearch.ElasticsearchSinkConnector", "tasks.max":"%d", "topics":"%s", "schema.ignore":"true", "key.ignore":"true", "type.name":"%s", "connection.url":"%s"}}`
)
