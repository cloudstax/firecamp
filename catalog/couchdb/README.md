The FireCamp CouchDB container is based on the [klaemo couchdb image](https://hub.docker.com/r/klaemo/couchdb/). The data volume will be mounted to the /data directory inside container. The CouchDB data will be stored at the /data/couchdb directory, and the config files are at the /data/conf directory.

**Topology**

By default, database will have 3 replicas and the replicas will be evenly distributed to the availability zones in the cluster. If the cluster has 3 availability zones, each zone will have one replica. The production cluster should have 3 availability zones, to tolerate the availability zone failure.

**Security**

The [Authentication and Authorization](http://docs.couchdb.org/en/2.1.0/config/auth.html) are enabled by default. The admin account is created. The require_valid_user is set to true for both cluster port and node-local port, to reject the requests from the anonymous users. The default admin user is "admin", and password "changeme". You could set the admin user and password to firecamp-service-cli when creating the CouchDB service.

The peruser option is broken in 2.1.0, see [CouchDB issue 749](https://github.com/apache/couchdb/issues/749). Once CouchDB fixes it, peruser will be supported.

The [CouchDB SSL](http://docs.couchdb.org/en/3.1.0/config/http.html#secure-socket-level-options) will be supported in the future.

The [Cross-Region Resource Sharing (cors)](http://docs.couchdb.org/en/2.1.0/config/http.html#cross-origin-resource-sharing) is disabled by default. If you want to enable it, simply set couchdb-enable-cors to true and set credentials, origins, headers and methods when creating the CouchDB service.

The [httpd config whitelist](http://docs.couchdb.org/en/2.1.0/config/http.html#httpd/config_whitelist) is disabled to avoid updating the configuration by mistake via the HTTP API.

**Logging**

The logs are sent to the Cloud Logs, such as AWS CloudWatch logs.

