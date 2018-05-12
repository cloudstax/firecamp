package kccatalog

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/cloudstax/firecamp/api/catalog"
	"github.com/cloudstax/firecamp/api/common"
	"github.com/cloudstax/firecamp/api/manage"
)

const (
	defaultVersion = "4.0"
	// ContainerImage is the main running container.
	ContainerImage           = common.ContainerNamePrefix + "kafka-connect:" + defaultVersion
	SinkESInitContainerImage = common.ContainerNamePrefix + "kafka-sink-elasticsearch-init:" + defaultVersion

	// connector listen port for managing the connector
	connectRestPort = 8083

	DefaultHeapMB = 512

	JSON_CONVERTER             = "org.apache.kafka.connect.json.JsonConverter"
	CONFIG_NAME_SUFFIX         = "config"
	OFFSET_NAME_SUFFIX         = "offset"
	STATUS_NAME_SUFFIX         = "status"
	DEFAULT_REPLICATION_FACTOR = uint(3)
	MAX_REPLICATION_FACTOR     = uint(7)

	// ElasticSearch default configs
	DefaultMaxBufferedRecords = 20000
	DefaultBatchSize          = 2000
	// The default ElasticSearch type.
	DEFAULT_TYPE_NAME = "kafka-connect"

	ENV_KAFKA_HEAP_OPTS                                 = "KAFKA_HEAP_OPTS"
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
	ENV_CONNECT_LOG4J_LOGGERS                           = "CONNECT_LOG4J_LOGGERS"
	reflectionLogger                                    = "org.reflections=ERROR"

	ENV_CONNECTOR_NAME  = "CONNECTOR_NAME"
	ENV_CONNECTOR_HOSTS = "CONNECTOR_HOSTS"
	ENV_CONNECTOR_PORT  = "CONNECTOR_PORT"

	ENV_ELASTICSEARCH_CONFIGS = "ELASTICSEARCH_CONFIGS"

	SinkESConfFileName = "sinkes.conf"
)

// The Kafka Connect catalog service

// ValidateSinkESRequest checks if the request is valid
func ValidateSinkESRequest(req *catalog.CatalogCreateKafkaSinkESRequest) error {
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
		return errors.New("The max memory should be larger than heap size and reserved memory")
	}

	return nil
}

// GenCreateESSinkServiceRequest returns the creation request for the kafka elasticsearch sink service.
func GenCreateESSinkServiceRequest(platform string, region string, cluster string, service string,
	kafkaServers string, esURIs string, req *catalog.CatalogCreateKafkaSinkESRequest) (crReq *manage.CreateServiceRequest, sinkESConfigs string) {
	// generate the container env variables
	envkvs := genEnvs(platform, region, cluster, service, kafkaServers, req.Options)

	portMappings := []common.PortMapping{
		{ContainerPort: connectRestPort, HostPort: connectRestPort, IsServicePort: true},
	}

	serviceCfgs, sinkESConfigs := genServiceConfigs(platform, cluster, service, esURIs, req.Options)

	replicaCfgs := catalog.GenStatelessServiceReplicaConfigs(cluster, service, int(req.Options.Replicas))

	reserveMemMB := req.Resource.ReserveMemMB
	if req.Resource.ReserveMemMB < req.Options.HeapSizeMB {
		reserveMemMB = req.Options.HeapSizeMB
	}

	// kafkaconnect docker-entrypoint script includes the firecamp-selectmember tool to select
	// the member and update dns. the firecamp-selectmember tool is different for different releases.
	// Every release has to use its own docker image.
	// TODO this is not good. we should consider to drop Docker Swarm. For AWS ECS, could use the
	// 			init container to do the work.
	// Note: scripts/builddocker.sh also builds the kafka connect image with the release version,
	// 			 the release upgrade needs to update the version as well.
	containerImage := ContainerImage
	if platform != common.ContainerPlatformK8s {
		containerImage = ContainerImage + common.NameSeparator + common.Version
	}

	crReq = &manage.CreateServiceRequest{
		Service: &manage.ServiceCommonRequest{
			Region:      region,
			Cluster:     cluster,
			ServiceName: service,
		},

		Resource: &common.Resources{
			MaxCPUUnits:     req.Resource.MaxCPUUnits,
			ReserveCPUUnits: req.Resource.ReserveCPUUnits,
			MaxMemMB:        req.Resource.MaxMemMB,
			ReserveMemMB:    reserveMemMB,
		},

		ServiceType:        common.ServiceTypeStateless,
		CatalogServiceType: common.CatalogService_KafkaSinkES,

		ContainerImage: containerImage,
		Replicas:       req.Options.Replicas,
		PortMappings:   portMappings,
		Envkvs:         envkvs,
		RegisterDNS:    true,

		ServiceConfigs: serviceCfgs,

		ReplicaConfigs: replicaCfgs,
	}
	return crReq, sinkESConfigs
}

func genEnvs(platform string, region string, cluster string, service string,
	kafkaServers string, opts *catalog.CatalogKafkaSinkESOptions) []*common.EnvKeyValuePair {

	replFactor := DEFAULT_REPLICATION_FACTOR
	if opts.ReplFactor > 0 {
		replFactor = opts.ReplFactor
	}

	groupID := fmt.Sprintf("%s-%s", cluster, service)
	configTopic := fmt.Sprintf("%s-%s", groupID, CONFIG_NAME_SUFFIX)
	offsetTopic := fmt.Sprintf("%s-%s", groupID, OFFSET_NAME_SUFFIX)
	statusTopic := fmt.Sprintf("%s-%s", groupID, STATUS_NAME_SUFFIX)

	envkvs := []*common.EnvKeyValuePair{
		// general env variables
		&common.EnvKeyValuePair{Name: common.ENV_REGION, Value: region},
		&common.EnvKeyValuePair{Name: common.ENV_CLUSTER, Value: cluster},
		&common.EnvKeyValuePair{Name: common.ENV_SERVICE_NAME, Value: service},
		&common.EnvKeyValuePair{Name: common.ENV_CONTAINER_PLATFORM, Value: platform},
		&common.EnvKeyValuePair{Name: common.ENV_DB_TYPE, Value: common.DBTypeCloudDB},
		// kafka connect env configs
		&common.EnvKeyValuePair{Name: ENV_KAFKA_HEAP_OPTS, Value: fmt.Sprintf("-Xmx%dm", opts.HeapSizeMB)},
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
		&common.EnvKeyValuePair{Name: ENV_CONNECT_LOG4J_LOGGERS, Value: reflectionLogger},
	}

	return envkvs
}

func genServiceConfigs(platform string, cluster string, service string, esURIs string,
	opts *catalog.CatalogKafkaSinkESOptions) (configs []*manage.ConfigFileContent, sinkESConfigs string) {

	replFactor := DEFAULT_REPLICATION_FACTOR
	if opts.ReplFactor > 0 {
		replFactor = opts.ReplFactor
	}

	bufferedRecords := DefaultMaxBufferedRecords
	if opts.MaxBufferedRecords > 0 {
		bufferedRecords = opts.MaxBufferedRecords
	}

	batchSize := DefaultBatchSize
	if opts.BatchSize > 0 {
		batchSize = opts.BatchSize
	}

	typeName := opts.TypeName
	if len(typeName) == 0 {
		typeName = DEFAULT_TYPE_NAME
	}

	// create the service.conf file
	content := fmt.Sprintf(servicefileContent, platform, opts.HeapSizeMB, opts.KafkaServiceName,
		opts.Topic, replFactor, opts.ESServiceName, bufferedRecords, batchSize, typeName)
	serviceCfg := &manage.ConfigFileContent{
		FileName: catalog.SERVICE_FILE_NAME,
		FileMode: common.DefaultConfigFileMode,
		Content:  content,
	}

	// create the sinkes.conf file
	name := genConnectorName(cluster, service)
	sinkESConfigs = fmt.Sprintf(sinkESConfigsContent, name, opts.Replicas,
		opts.Topic, typeName, bufferedRecords, batchSize, esURIs)

	esCfg := &manage.ConfigFileContent{
		FileName: SinkESConfFileName,
		FileMode: common.DefaultConfigFileMode,
		Content:  sinkESConfigs,
	}

	configs = []*manage.ConfigFileContent{serviceCfg, esCfg}
	return configs, sinkESConfigs
}

// GenSinkESServiceInitRequest creates the init request for elasticsearch sink connector.
func GenSinkESServiceInitRequest(req *manage.ServiceCommonRequest,
	replicas int64, manageurl string, sinkESConfigs string) *manage.RunTaskRequest {

	kvregion := &common.EnvKeyValuePair{Name: common.ENV_REGION, Value: req.Region}
	kvcluster := &common.EnvKeyValuePair{Name: common.ENV_CLUSTER, Value: req.Cluster}
	kvmgtserver := &common.EnvKeyValuePair{Name: common.ENV_MANAGE_SERVER_URL, Value: manageurl}
	kvop := &common.EnvKeyValuePair{Name: common.ENV_OP, Value: catalog.CatalogSetServiceInitOp}

	kvservice := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_NAME, Value: req.ServiceName}
	kvsvctype := &common.EnvKeyValuePair{Name: common.ENV_SERVICE_TYPE, Value: common.CatalogService_KafkaSinkES}

	connectorHosts := catalog.GenServiceMemberHosts(req.Cluster, req.ServiceName, replicas)
	kvhosts := &common.EnvKeyValuePair{Name: ENV_CONNECTOR_HOSTS, Value: connectorHosts}
	kvport := &common.EnvKeyValuePair{Name: ENV_CONNECTOR_PORT, Value: strconv.Itoa(connectRestPort)}

	kvconfigs := &common.EnvKeyValuePair{Name: ENV_ELASTICSEARCH_CONFIGS, Value: sinkESConfigs}
	name := genConnectorName(req.Cluster, req.ServiceName)
	kvname := &common.EnvKeyValuePair{Name: ENV_CONNECTOR_NAME, Value: name}

	envkvs := []*common.EnvKeyValuePair{kvregion, kvcluster, kvmgtserver, kvop, kvservice, kvsvctype, kvhosts, kvport, kvconfigs, kvname}

	return &manage.RunTaskRequest{
		Service: req,
		Resource: &common.Resources{
			MaxCPUUnits:     common.DefaultMaxCPUUnits,
			ReserveCPUUnits: common.DefaultReserveCPUUnits,
			MaxMemMB:        common.DefaultMaxMemoryMB,
			ReserveMemMB:    common.DefaultReserveMemoryMB,
		},
		ContainerImage: SinkESInitContainerImage,
		TaskType:       common.TaskTypeInit,
		Envkvs:         envkvs,
	}
}

func genConnectorName(cluster string, service string) string {
	return fmt.Sprintf("%s-%s", cluster, service)
}

const (
	servicefileContent = `
PLATFORM=%s
HEAP_SIZE_MB=%d
KAFKA_SERVICE_NAME=%s
TOPIC=%s
REPLICATION_FACTOR=%d
ES_SERVICE_NAME=%s
MAX_BUFFERED_RECORDS=%d
BATCH_SIZE=%d
TYPE_NAME=%s
`

	sinkESConfigsContent = `{"name": "%s", "config": {"connector.class":"io.confluent.connect.elasticsearch.ElasticsearchSinkConnector", "tasks.max":"%d", "topics":"%s", "schema.ignore":"true", "key.ignore":"true", "type.name":"%s", "max.buffered.records":"%d", "batch.size":"%d", "connection.url":"%s"}}`
)
