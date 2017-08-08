package catalog

const (
	// The supported catalog services
	CatalogService_MongoDB    = "mongodb"
	CatalogService_PostgreSQL = "postgresql"
	CatalogService_Cassandra  = "cassandra"
	CatalogService_ZooKeeper  = "zookeeper"
	CatalogService_Kafka      = "kafka"
	CatalogService_Redis      = "redis"

	// The system variables in the sys.conf file
	SYS_FILE_NAME      = "sys.conf"
	SYS_SERVICE_MEMBER = "SERVICE_MEMBER"
)
