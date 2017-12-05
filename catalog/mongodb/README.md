* [FireCamp MongoDB Internals](https://github.com/cloudstax/firecamp/tree/master/catalog/cassandra#firecamp-mongodb-internals)
* [Tutorials](https://github.com/cloudstax/firecamp/tree/master/catalog/mongodb#tutorials)

# FireCamp MongoDB Internals

The FireCamp MongoDB container is based on the [official MongoDB image](https://hub.docker.com/_/mongo/). Two volumes will be created, one for journal, the other for data. The journal volume will be mounted to the /journal directory inside container. The data volume will be mounted to the /data directory inside container. The MongoDB data will be stored at the /data/db directory, and the config files are at the /data/conf directory.

## Cluster

**ReplicaSet**

One primary with multiple secondaries. For example, to create a 1 primary and 2 secondaries MongoDB ReplicaSet service, set "mongo-shards" to 1, "mongo-replicas-pershard" to 3 and "mongo-replicaset-only" as true. If the cluster has multiple zones, the primary and secondaries will be distributed to different availability zones.

When the primary node or availability zone goes down, MongoDB will automatically promote one of the secondaries to the new primary.

**Sharding**

MongoDB uses sharding to support deployments with very large data sets and high throughput operations. FireCamp will automatically set up the Config Server ReplicaSet, and the Shard ReplicaSets. The primary and secondaries of the ReplicaSet will be distributed to different availability zones.

[The application must connect to a mongos router](https://docs.mongodb.com/manual/sharding/#connecting-to-a-sharded-cluster) to interact with any collection in the sharded cluster. This includes sharded and unsharded collections. Clients should never connect to a single shard in order to perform read or write operations. By default, FireCamp sets the Config Server to listen on port 27019 and the Shard to listen on port 27018. This could help to prevent the possible misuse that directly connects to the Shard via the stardard 27017 port.

FireCamp assigns the DNS names for the Config ReplicaSet and Shard ReplicaSets. For example, create "mymongo" service on cluster "t1" with 3 config servers, 2 shards with 3 members per shard. The Config ReplicaSet name will be "mymongo-config". The Config ReplicaSet member names will be "mymongo-config-0.t1-firecamp.com", "mymongo-config-1.t1-firecamp.com" and "mymongo-config-2.t1-firecamp.com". The first shard ReplicaSet name will be "mymongo-shard0", member names "mymongo-shard0-0.t1-firecamp.com", "mymongo-shard0-1.t1-firecamp.com" and "mymongo-shard0-2.t1-firecamp.com". The second shard ReplicaSet name will be "mymongo-shard1", member names "mymongo-shard1-0.t1-firecamp.com", "mymongo-shard1-1.t1-firecamp.com" and "mymongo-shard1-2.t1-firecamp.com".

Currently FireCamp only deploys the Config Server and Shards. There is no mongos query router deployed. A common pattern is to place a mongos on each application server. Deploying one mongos router on each application server reduces network latency between the application and the router. If there are lots of application servers or the application is written as Lambda function, you could deploy a set of mongos routers. FireCamp MongoDB has the keyfile authentication enabled by default. FireCamp will return the keyfile and mongos router needs to start with the keyfile to access the sharded cluster. For example, run: `mongos --keyFile <pathToKeyfile> --configdb mymongo-config/mymongo-config-0.t1-firecamp.com:27019,mymongo-config-1.t1-firecamp.com:27019,mymongo-config-2.t1-firecamp.com:27019`.

In the later release, will support to place a mongos router on each shard primary.

## Security

The FireCamp MongoDB follows the offical [security checklist](https://docs.mongodb.com/manual/administration/security-checklist). The [Authentication](https://docs.mongodb.com/manual/tutorial/enable-authentication/) is enabled by default. The root user is created and the [Keyfile](https://docs.mongodb.com/manual/tutorial/enforce-keyfile-access-control-in-existing-replica-set/) is created for the access control between members of a Replica Set.

The default user is assigned the [superuser role](https://docs.mongodb.com/v3.2/reference/built-in-roles/#superuser-roles). You should login and create users with other roles, see [built in roles](https://docs.mongodb.com/v3.2/reference/built-in-roles/).

For the sharded cluster, every shard will have the same local root admin user. This is to protect the possible maintenance operations that directly connects to the shard, see [Shard Local User](https://docs.mongodb.com/v3.4/core/security-users/#shard-local-users). The key file authentication is also enabled for the Config Server ReplicaSet and Shard ReplicaSet.

## Logging

The MongoDB logs are sent to the Cloud Logs, such as AWS CloudWatch logs. Could easily check what happens to MongoDB through CloudWatch logs.

The current latest Amazon Linux AMI uses Docker 17.03, which does not support the custom logging driver. So the awslogs driver is directly used.

Every service will have its own log group. For example, create the MongoDB service, mymongo, on cluster t1. The log group t1-mymongo-uuid will be created for the service. A log stream is created for every container. The FireCamp MongoDB container will log its service member at startup. We could get the full logs of one service member by simply checking the server member log entry in the log streams.

The custom logging driver is supported from Docker 17.05. Once Amazone Linux AMI updates to use higher Docker version and supports the custom loggint driver, we will switch to the FireCamp CloudWatch log driver. The FireCamp CloudWatch log driver will send the logs of one service member to one log stream, no matter where the container runs on.

## Cache

By default, the MongoDB instance inside the container assumes the whole node's memory could be used, and calculate the cache size accordingly. In case you want to limit the memory size, could set the max-memory when creating the service.


# Tutorials

This is a simple tutorial about how to create a MongoDB service and how to use it. This tutorial assumes the cluster name is "t1", the AWS Region is "us-east-1", and the MongoDB service name is "mymongo".

## Create a MongoDB service
Follow the [Installation Guide](https://github.com/cloudstax/firecamp/tree/master/docs/installation) guide to create a 9 nodes cluster across 3 availability zones. Create a 2 shards MongoDB cluster:
```
firecamp-service-cli -op=create-service -service-type=mongodb -region=us-east-1 -cluster=t1 -service-name=mymongo -mongo-shards=2 -mongo-replicas-pershard=3 -mongo-replicaset-only=false -mongo-configservers=3 -volume-size=100 -journal-volume-size=10 -admin=admin -password=changeme
```

This creates a 3 replicas MongoDB ReplicaSet on 3 availability zones. Each replica has 2 volumes, 10GB volume for journal and 100GB volume for data. The MongoDB admin is "admin", password is "changeme". The Config ReplicaSet member names will be "mymongo-config-0.t1-firecamp.com", "mymongo-config-1.t1-firecamp.com" and "mymongo-config-2.t1-firecamp.com". The first shard ReplicaSet name will be "mymongo-shard0", member names "mymongo-shard0-0.t1-firecamp.com", "mymongo-shard0-1.t1-firecamp.com" and "mymongo-shard0-2.t1-firecamp.com". The second shard ReplicaSet name will be "mymongo-shard1", member names "mymongo-shard1-0.t1-firecamp.com", "mymongo-shard1-1.t1-firecamp.com" and "mymongo-shard1-2.t1-firecamp.com".

To create a single ReplicaSet, could set "mongo-replicaset-only" to true and "mongo-shards" to 1.
```
firecamp-service-cli -op=create-service -service-type=mongodb -region=us-east-1 -cluster=t1 -service-name=mymongo -mongo-shards=1 -mongo-replicas-pershard=3 -mongo-replicaset-only=true -volume-size=100 -journal-volume-size=10 -admin=admin -password=changeme
```

In release 0.9 and before, only a single ReplicaSet is supported. The parameters to create a MongoDB ReplicaSet is as below:
```
firecamp-service-cli -op=create-service -service-type=mongodb -region=us-east-1 -cluster=t1 -service-name=mymongo -replicas=3 -volume-size=100 -journal-volume-size=10 -admin=admin -password=changeme
```

The MongoDB service creation steps:
1. Create the Volumes and persist the metadata to the FireCamp DB. This is usually very fast. But if AWS is slow at creating the Volume, this step will be slow as well.
2. Create the service on the container orchestration framework, such as AWS ECS, and wait for all service containers running. The speed depends on how fast the container orchestration framework could start all containers.
3. Wait some time (10 seconds) for MongoDB to stabilize.
4. Start the MongoDB initialization task. The speed also depends on how fast the container orchestration framework schedules the task.
5. The initialization task does:
   * Initialize the MongoDB replication.
   * Wait some time (10 seconds) for MongoDB to stabilize.
   * Create the admin user.
   * Notify the manage server to do the post initialization work.
6. The manage server will do the post initialization works, which includes:
   * Update the mongod.conf of every replica to enable MongoDB Authentication.
   * Restart all MongoDB containers.

In case the service creation fails, please simply retry it.


## Sharded Cluster
### Setup the mongos query router
Create an EC2 instance in the AppAccessSecurityGroup and the same VPC. Run mongos with the authentication keyfile, `mongos --keyFile <pathToKeyfile> --configdb mymongo-config/mymongo-config-0.t1-firecamp.com:27019,mymongo-config-1.t1-firecamp.com:27019,mymongo-config-2.t1-firecamp.com:27019`

### Create the user and user db
1. Launch the mongo shell on the same node with mongos: `mongo --host localhost -u admin -p changeme --authenticationDatabase admin`
2. Check cluster status: `sh.status()`
3. Create a DB: `use memberdb`
4. Create a user for the memberdb:
```
db.createUser(
  {
    user: "u1",
    pwd: "u1",
    roles: [
      { role: "readWrite", db: "memberdb" }
    ]
  }
)
```

### Connect to the user db
1. Launch the mongo shell on the same node with mongos: `mongo --host localhost -u u1 -p u1 --authenticationDatabase memberdb`
2. Switch to memberdb: `use memberdb`
3. Insert a document:
```
db.members.insertOne(
   {
      name: "sue",
      age: 19,
      status: "P",
      likes : [ "golf", "football" ]
   }
)
```
4. Read the document: `db.members.find()`


## Single ReplicaSet
### Find out the current master
Connect to any replica and run "db.isMaster()": `mongo --host mymongo-0.t1-firecamp.com --eval "db.isMaster()"`

The output is like below. The "primary" is "mymongo-0.t1-firecamp.com".
```
MongoDB shell version v3.4.4
connecting to: mongodb://mymongo-0.t1-firecamp.com:27017/
MongoDB server version: 3.4.4
{
	"hosts" : [
		"mymongo-0.t1-firecamp.com:27017",
		"mymongo-1.t1-firecamp.com:27017",
		"mymongo-2.t1-firecamp.com:27017"
	],
	"setName" : "mymongo",
	"setVersion" : 1,
	"ismaster" : true,
	"secondary" : false,
	"primary" : "mymongo-0.t1-firecamp.com:27017",
	"me" : "mymongo-0.t1-firecamp.com:27017",
	"electionId" : ObjectId("7fffffff0000000000000002"),
	"lastWrite" : {
		"opTime" : {
			"ts" : Timestamp(1499295180, 1),
			"t" : NumberLong(2)
		},
		"lastWriteDate" : ISODate("2017-07-05T22:53:00Z")
	},
	"maxBsonObjectSize" : 16777216,
	"maxMessageSizeBytes" : 48000000,
	"maxWriteBatchSize" : 1000,
	"localTime" : ISODate("2017-07-05T22:53:01.115Z"),
	"maxWireVersion" : 5,
	"minWireVersion" : 0,
	"readOnly" : false,
	"ok" : 1
}
```

### Create the user and user db
1. Connect to the Primary from the application node: `mongo --host mymongo-0.t1-firecamp.com -u admin -p changeme --authenticationDatabase admin`
2. Create a DB: `use memberdb`
3. Create a user for the memberdb:
```
db.createUser(
  {
    user: "u1",
    pwd: "u1",
    roles: [
      { role: "readWrite", db: "memberdb" }
    ]
  }
)
```

### Connect to the user db
1. Connect to the primary from the application node: `mongo --host mymongo-0.t1-firecamp.com -u u1 -p u1 --authenticationDatabase memberdb`
2. Switch to memberdb: `use memberdb`
3. Insert a document:
```
db.members.insertOne(
   {
      name: "sue",
      age: 19,
      status: "P",
      likes : [ "golf", "football" ]
   }
)
```
4. Read the document: `db.members.find()`

