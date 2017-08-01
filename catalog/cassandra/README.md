The OpenManage Cassandra container is based on Debian. The data volume will be mounted to the /data directory inside container. All Cassandra data, including data file, commit log, hints, and the key cache, will be stored under /data directory.

**Topology**

Cassandra Ec2Snitch could load the AWS region and availability zone from the EC2 API. But Ec2Snitch could not support other clouds, such as Google or Microsoft Cloud. So the GossipingPropertyFileSnitch is used to teach Cassandra about the topology. Every member will record the region as dc, and the availability zone as rack.

Only one region is supported currently. The multi-regions Cassandra will be supported in the future.

**Security**

The OpenManage Cassandra follows the official [security guide](http://cassandra.apache.org/doc/latest/operating/security.html). The Authentication and Authorization are enabled by default.

The TLS/SSL encryption is not enabled to avoid the possible impact on performance. This would not be a big security risk. The Cassandra cluster on the OpenManage platform could only be accessed by the nodes running on the AppAccessSecurityGroup and the same VPC. And both Authentication and Authorization are enabled. Even if someone hacks to the application node, he/she will need to know the username and password to access the Cassandra cluster.

After the Cassandra cluster is initialized, please login to create a new superuser and disable the default "cassandra" superuser. For example, cluster name is t1, cassandra service name is mycas. The steps are:

1. Login to the cluster: cqlsh mycas-0.t1-openmanage.com -u cassandra -p cassandra

2. Create a new superuser: CREATE ROLE newsuperuser WITH SUPERUSER = true AND LOGIN = true AND PASSWORD = 'super';

3. Log out.

4. Login to the cluster with the new superuser: cqlsh mycas-0.t1-openmanage.com -u newsuperuser -p super

5. Disable the default superuser: ALTER ROLE cassandra WITH SUPERUSER = false AND LOGIN = false;

6. Finally, set up the roles and credentials for your application users with CREATE ROLE statements.


**Cache**

The Cassandra key cache is enabled, and the row cache is disabled. The key cache size is automatically calculated based on the total memory of the node.

Note: the OpenManage Cassandra assumes the whole node is only used by Cassandra. We may consider to limit the max cpu and memory for Cassandra container in the future.

**Logging**

The Cassandra logs are sent to the Cloud Logs, such as AWS CloudWatch logs.


Refs:

[1] [Cassandra on AWS White Paper](https://d0.awsstatic.com/whitepapers/Cassandra_on_AWS.pdf)

[2] [Planning a Cassandra cluster on Amazon EC2](http://docs.datastax.com/en/landing_page/doc/landing_page/planning/planningEC2.html)
