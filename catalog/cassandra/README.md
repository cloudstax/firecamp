# FireCamp Cassandra Internals

The FireCamp Cassandra container is based on the [official Cassandra image](https://hub.docker.com/_/cassandra/). Two volumes will be created, one for the commit log, the other for all other data. The commit log volume will be mounted to the /journal directory inside container. The data volume will be mounted to the /data directory inside container. Cassandra commit log is stored under /journal directory. All other Cassandra data, including data file, hints, and the key cache, will be stored under /data directory.

## Topology

Cassandra Ec2Snitch could load the AWS region and availability zone from the EC2 API. But Ec2Snitch could not support other clouds, such as Google or Microsoft Cloud. So the GossipingPropertyFileSnitch is used to teach Cassandra about the topology. Every member will record the region as dc, and the availability zone as rack.

Only one region is supported currently. The multi-regions Cassandra will be supported in the future.

## Security

The FireCamp Cassandra follows the official [security guide](http://cassandra.apache.org/doc/latest/operating/security.html). The Authentication and Authorization are enabled by default.

The TLS/SSL encryption will be supported in the future. This would not be a big security risk. The Cassandra cluster on the FireCamp platform could only be accessed by the nodes running on the AppAccessSecurityGroup and the same VPC. And both Authentication and Authorization are enabled.

After the Cassandra cluster is initialized, please login to create a new superuser and disable the default "cassandra" superuser. For example, cluster name is t1, cassandra service name is mycas. The steps are:

1. Login to the cluster: cqlsh mycas-0.t1-firecamp.com -u cassandra -p cassandra

2. Create a new superuser: CREATE ROLE newsuperuser WITH SUPERUSER = true AND LOGIN = true AND PASSWORD = 'super';

3. Log out.

4. Login to the cluster with the new superuser: cqlsh mycas-0.t1-firecamp.com -u newsuperuser -p super

5. Disable the default superuser: ALTER ROLE cassandra WITH SUPERUSER = false AND LOGIN = false;

6. Finally, set up the roles and credentials for your application users with CREATE ROLE statements.


## Cache

The Cassandra key cache is enabled, and the row cache is disabled. The key cache size is automatically calculated based on the total memory of the node.

Note: the FireCamp Cassandra assumes the whole node is only used by Cassandra. We may consider to limit the max cpu and memory for Cassandra container in the future.

## Logging

The Cassandra logs are sent to the Cloud Logs, such as AWS CloudWatch logs.

## Set JVM TTL for Java CQL driver

By default, JVM caches a successful DNS lookup forever. If you use Cassandra Java CQL driver, please [set JVM TTL](http://docs.aws.amazon.com/AWSSdkDocsJava/latest/DeveloperGuide/java-dg-jvm-ttl.html) to a reasonable value such as 60 seconds. So when Cassandra container moves to another node, Java CQL driver could lookup the new address.

Refs:

[1] [Cassandra on AWS White Paper](https://d0.awsstatic.com/whitepapers/Cassandra_on_AWS.pdf)

[2] [Planning a Cassandra cluster on Amazon EC2](http://docs.datastax.com/en/landing_page/doc/landing_page/planning/planningEC2.html)


# Tutorials

This is a simple tutorial about how to create a Cassandra service and how to use it. This tutorial assumes the cluster name is "t1", the AWS Region is "us-east-1", and the Cassandra service name is "mycas".

## Create a Cassandra service
Follow the [Installation](https://github.com/cloudstax/firecamp/tree/master/docs/installation) guide to create a 3 nodes cluster across 3 availability zones. Create a Cassandra cluster:
```
firecamp-service-cli -op=create-service -service-type=cassandra -region=us-east-1 -cluster=t1 -service-name=mycas -replicas=3 -volume-size=100 -journal-volume-size=10
```

This creates a 3 replicas Cassandra on 3 availability zones. Each replica has 2 volumes, 10GB volume for journal and 100GB volume for data. The DNS names of the replicas would be: mycas-0.t1-firecamp.com, mycas-1.t1-firecamp.com, mycas-2.t1-firecamp.com.

The Cassandra service creation steps:
1. Create the Volumes and persist the metadata to the FireCamp DB. This is usually very fast. But if AWS is slow at creating the Volume, this step will be slow as well.
2. Create the service on the container orchestration framework, such as AWS ECS, and wait for all service containers running. The speed depends on how fast the container orchestration framework could start all containers.
3. Wait some time (10 seconds) for Cassandra to stabilize.
4. Start the Cassandra initialization task. The speed also depends on how fast the container orchestration framework schedules the task.
5. The initialization task does:
   * Increase the replication factor for the system_auth keyspace to 3.
   * Notify the manage server to do the post initialization work.
6. The manage server will set the Cassandra service initialized.

In case the service creation fails, please simply retry it. Once the service is initialized, you could create a new super user.

## Create the New Super User
Cassandra has a default super user, name "cassandra" and password "cassandra". After the Cassandra service is initialized, please login to create a new superuser and disable the default "cassandra" superuser. The steps are:

1. Login to the cluster: `cqlsh mycas-0.t1-firecamp.com -u cassandra -p cassandra`
2. Create a new superuser: `CREATE ROLE newsuperuser WITH SUPERUSER = true AND LOGIN = true AND PASSWORD = 'super';`
3. Log out.
4. Login to the cluster with the new superuser: `cqlsh mycas-0.t1-firecamp.com -u newsuperuser -p super`
5. Disable the default superuser: `ALTER ROLE cassandra WITH SUPERUSER = false AND LOGIN = false;`
6. Finally, set up the roles and credentials for your application users with CREATE ROLE statements.

## Create Keyspace and User
1. Login with the new superuser: `cqlsh mycas-0.t1-firecamp.com -u newsuperuser -p super`
2. Create a keyspace and table
```
CREATE KEYSPACE test WITH REPLICATION = { 'class' : 'NetworkTopologyStrategy', 'us-east-1' : 3 };
use test;
CREATE TABLE users (userid text PRIMARY KEY, first_name text, last_name text);
```
3. Create role for the keyspace and table
```
CREATE ROLE supervisor;
GRANT MODIFY ON test.users TO supervisor;
GRANT SELECT ON test.users TO supervisor;
CREATE ROLE pam WITH PASSWORD = 'password' AND LOGIN = true;
GRANT supervisor TO pam;
LIST ALL PERMISSIONS OF pam;
```
4. Login as the new "pam" role: `cqlsh mycas-2.t1-firecamp.com -u pam -p password`
5. Insert new records:
```
use test;
insert into users (userid, first_name, last_name) values('user1', 'a1', 'b1');
select * from users;
```
