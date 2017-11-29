# FireCamp CouchDB Internals

The FireCamp CouchDB container is based on the [apache couchdb image](https://github.com/apache/couchdb-docker). The data volume will be mounted to the /data directory inside container. The CouchDB data will be stored at the /data/couchdb directory, and the config files are at the /data/conf directory.

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


# Tutorials

This is a simple tutorial about how to create a CouchDB service and how to use it. This tutorial assumes the cluster name is "t1", the AWS Region is "us-east-1", and the CouchDB service name is "mycouch".

## Create a CouchDB service
Follow the [Installation Guide](https://github.com/cloudstax/firecamp/tree/master/docs/installation) guide to create a 3 nodes cluster across 3 availability zones. Create a CouchDB cluster:
```
firecamp-service-cli -op=create-service -service-type=couchdb -region=us-east-1 -cluster=t1 -replicas=3 -volume-size=100 -service-name=mycouch -admin=admin -password=changeme
```

This creates a 3 replicas CouchDB on 3 availability zones. The database replicas will be distributed to different availability zone, to tolerate one availability zone failure. Every replica has 100GB volume. The DNS names of the members would be: mycouch-0.t1-firecamp.com, mycouch-1.t1-firecamp.com, mycouch-2.t1-firecamp.com.

## Create Database and Documents
1. Check CouchDB is running: `curl admin:changeme@mycouch-0.t1-firecamp.com:5984/`
```
{"couchdb":"Welcome","version":"2.1.0","features":["scheduler"],"vendor":{"name":"The Apache Software Foundation"}}
```
2. Check all dbs: `curl admin:changeme@mycouch-0.t1-firecamp.com:5984/_all_dbs`
```
["_global_changes","_replicator","_users"]
```
3. Create a db: `curl -X PUT admin:changeme@mycouch-0.t1-firecamp.com:5984/fruits`
```
{"ok":true}
```
4. List all dbs: `curl admin:changeme@mycouch-0.t1-firecamp.com:5984/_all_dbs`
```
["_global_changes","_replicator","_users","fruits"]
```
5. Create a doc: `curl -X PUT admin:changeme@mycouch-0.t1-firecamp.com:5984/fruits/001 -d '{ "item": "apple", "prices": 1.59 }'`
```
{"ok":true,"id":"001","rev":"1-c3e92e9cc218153b17972aff94f77475"}
```
6. Create another doc: `curl -X PUT admin:changeme@mycouch-0.t1-firecamp.com:5984/fruits/002 -d '{ "item": "orange", "prices": 1.99 }'`
```
{"ok":true,"id":"002","rev":"1-28e160edec1821bcfb2230dc7ca75a7d"}
```
7. List all docs: `curl admin:changeme@mycouch-0.t1-firecamp.com:5984/fruits/_all_docs`
```
{"total_rows":2,"offset":0,"rows":[
{"id":"001","key":"001","value":{"rev":"1-c3e92e9cc218153b17972aff94f77475"}},
{"id":"002","key":"002","value":{"rev":"1-28e160edec1821bcfb2230dc7ca75a7d"}}
]}
```
8. Get one doc: `curl admin:changeme@mycouch1-0.t1-firecamp.com:5984/fruits/001`
```
{"_id":"001","_rev":"1-c3e92e9cc218153b17972aff94f77475","item":"apple","prices":1.59}
```

