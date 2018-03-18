package kafkamanagercatalog

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudstax/firecamp/catalog/zookeeper"
	"github.com/cloudstax/firecamp/common"
	"github.com/cloudstax/firecamp/dns"
	"github.com/cloudstax/firecamp/manage"
	"github.com/cloudstax/firecamp/utils"
	"github.com/golang/glog"
)

const (
	defaultVersion = "1.3.3"
	// ContainerImage is the main running container.
	ContainerImage = common.ContainerNamePrefix + "kafka-manager:" + defaultVersion

	listenPort = 9000

	// DefaultHeapMB is the default kafka manager java heap size.
	// TODO what is the best heap size for kafka manager?
	DefaultHeapMB = 4096

	ENV_ZKHOSTS     = "ZK_HOSTS"
	ENV_JAVA_OPTS   = "JAVA_OPTS"
	ENV_KM_USERNAME = "KM_USERNAME"
	ENV_KM_PASSWORD = "KM_PASSWORD"

	zkServerSep = ","
)

// The Yahoo Kafka Manager catalog service, https://github.com/yahoo/kafka-manager
// Kafka Manager only needs 1 instance running.
// Kafka Manager will store the data in ZooKeeper. So Kafka Manager itself is stateless.

// ValidateRequest checks if the request is valid
func ValidateRequest(req *manage.CatalogCreateKafkaManagerRequest) error {
	if len(req.Options.User) == 0 || len(req.Options.Password) == 0 {
		return errors.New("Please specify the user and password")
	}

	return nil
}

// GenDefaultCreateServiceRequest returns the default service creation request.
func GenDefaultCreateServiceRequest(platform string, region string, cluster string,
	service string, opts *manage.CatalogKafkaManagerOptions, res *common.Resources,
	zkattr *common.ServiceAttr) (*manage.CreateServiceRequest, error) {

	zkServers := genZkServerList(zkattr)

	envkvs := []*common.EnvKeyValuePair{
		&common.EnvKeyValuePair{Name: common.ENV_CLUSTER, Value: cluster},
		&common.EnvKeyValuePair{Name: common.ENV_SERVICE_NAME, Value: service},
		&common.EnvKeyValuePair{Name: ENV_ZKHOSTS, Value: zkServers},
		&common.EnvKeyValuePair{Name: ENV_JAVA_OPTS, Value: fmt.Sprintf("-Xms%dM -Xmx%dM", opts.HeapSizeMB, opts.HeapSizeMB)},
		// TODO it is not best to put user & password directly in container environment.
		// consider to add a record to DB and fetch from DB?
		&common.EnvKeyValuePair{Name: ENV_KM_USERNAME, Value: opts.User},
		&common.EnvKeyValuePair{Name: ENV_KM_PASSWORD, Value: opts.Password},
	}

	portMappings := []common.PortMapping{
		{ContainerPort: listenPort, HostPort: listenPort, IsServicePort: true},
	}

	reserveMemMB := res.ReserveMemMB
	if res.ReserveMemMB < opts.HeapSizeMB {
		reserveMemMB = opts.HeapSizeMB
	}

	// TODO support upgrade
	userAttr := &common.KMUserAttr{
		HeapSizeMB:    opts.HeapSizeMB,
		ZkServiceName: opts.ZkServiceName,
		User:          opts.User,
		Password:      opts.Password,
	}
	b, err := json.Marshal(userAttr)
	if err != nil {
		glog.Errorln("Marshal UserAttr error", err, opts)
		return nil, err
	}

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
		// Kafka Manager only needs 1 container.
		Replicas:     1,
		PortMappings: portMappings,
		Envkvs:       envkvs,
		RegisterDNS:  true,

		UserAttr: &common.ServiceUserAttr{
			ServiceType: common.CatalogService_KafkaManager,
			AttrBytes:   b,
		},
	}
	return req, nil
}

// genZkServerList creates the zookeeper server list
func genZkServerList(zkattr *common.ServiceAttr) string {
	zkServers := ""
	for i := int64(0); i < zkattr.Replicas; i++ {
		member := utils.GenServiceMemberName(zkattr.ServiceName, i)
		dnsname := dns.GenDNSName(member, zkattr.DomainName)
		if len(zkServers) != 0 {
			zkServers += zkServerSep
		}
		zkServers += fmt.Sprintf("%s:%d", dnsname, zkcatalog.ClientPort)
	}
	return zkServers
}
