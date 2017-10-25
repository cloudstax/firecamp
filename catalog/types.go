package catalog

const (
	// The supported catalog services
	CatalogService_MongoDB       = "mongodb"
	CatalogService_PostgreSQL    = "postgresql"
	CatalogService_Cassandra     = "cassandra"
	CatalogService_ZooKeeper     = "zookeeper"
	CatalogService_Kafka         = "kafka"
	CatalogService_Redis         = "redis"
	CatalogService_CouchDB       = "couchdb"
	CatalogService_Consul        = "consul"
	CatalogService_ElasticSearch = "elasticsearch"
	CatalogService_Kibana        = "kibana"
	CatalogService_Logstash      = "logstash"

	// The system variables in the sys.conf file
	SYS_FILE_NAME = "sys.conf"

	BindAllIP = "0.0.0.0"
)
