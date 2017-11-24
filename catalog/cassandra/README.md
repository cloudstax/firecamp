The FireCamp Cassandra container is based on the [official Cassandra image](https://hub.docker.com/_/cassandra/). Two volumes will be created, one for the commit log, the other for all other data. The commit log volume will be mounted to the /journal directory inside container. The data volume will be mounted to the /data directory inside container. Cassandra commit log is stored under /journal directory. All other Cassandra data, including data file, hints, and the key cache, will be stored under /data directory.

**Topology**

Cassandra Ec2Snitch could load the AWS region and availability zone from the EC2 API. But Ec2Snitch could not support other clouds, such as Google or Microsoft Cloud. So the GossipingPropertyFileSnitch is used to teach Cassandra about the topology. Every member will record the region as dc, and the availability zone as rack.

Only one region is supported currently. The multi-regions Cassandra will be supported in the future.

**Security**

The FireCamp Cassandra follows the official [security guide](http://cassandra.apache.org/doc/latest/operating/security.html). The Authentication and Authorization are enabled by default.

The TLS/SSL encryption is not enabled to avoid the possible impact on performance. This would not be a big security risk. The Cassandra cluster on the FireCamp platform could only be accessed by the nodes running on the AppAccessSecurityGroup and the same VPC. And both Authentication and Authorization are enabled. Even if someone hacks to the application node, he/she will need to know the username and password to access the Cassandra cluster.

After the Cassandra cluster is initialized, please login to create a new superuser and disable the default "cassandra" superuser. For example, cluster name is t1, cassandra service name is mycas. The steps are:

1. Login to the cluster: cqlsh mycas-0.t1-firecamp.com -u cassandra -p cassandra

2. Create a new superuser: CREATE ROLE newsuperuser WITH SUPERUSER = true AND LOGIN = true AND PASSWORD = 'super';

3. Log out.

4. Login to the cluster with the new superuser: cqlsh mycas-0.t1-firecamp.com -u newsuperuser -p super

5. Disable the default superuser: ALTER ROLE cassandra WITH SUPERUSER = false AND LOGIN = false;

6. Finally, set up the roles and credentials for your application users with CREATE ROLE statements.


**Cache**

The Cassandra key cache is enabled, and the row cache is disabled. The key cache size is automatically calculated based on the total memory of the node.

Note: the FireCamp Cassandra assumes the whole node is only used by Cassandra. We may consider to limit the max cpu and memory for Cassandra container in the future.

**Logging**

The Cassandra logs are sent to the Cloud Logs, such as AWS CloudWatch logs.

**Set JVM TTL for Java CQL driver**

By default, JVM caches a successful DNS lookup forever. If you use Cassandra Java CQL driver, please [set JVM TTL](http://docs.aws.amazon.com/AWSSdkDocsJava/latest/DeveloperGuide/java-dg-jvm-ttl.html) to a reasonable value such as 60 seconds. So when Cassandra container moves to another node, Java CQL driver could lookup the new address.

Refs:

[1] [Cassandra on AWS White Paper](https://d0.awsstatic.com/whitepapers/Cassandra_on_AWS.pdf)

[2] [Planning a Cassandra cluster on Amazon EC2](http://docs.datastax.com/en/landing_page/doc/landing_page/planning/planningEC2.html)
