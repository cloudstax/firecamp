The OpenManage Cassandra container is based on Debian. The data volume will be mounted to the /data directory inside container. All Cassandra data, including data file, commit log, hints, and the key cache, will be stored under /data directory.

**Topology**

Cassandra Ec2Snitch could load the AWS region and availability zone from the EC2 API. But Ec2Snitch could not support other clouds, such as Google or Microsoft Cloud. So the GossipingPropertyFileSnitch is used to teach Cassandra about the topology. Every member will record the region as dc, and the availability zone as rack.

Only one region is supported currently. The multi-regions Cassandra will be supported in the future.

**Security**

Authentication is disabled currently.

**Cache**

The Cassandra key cache is enabled, and the row cache is disabled. The key cache size is automatically calculated based on the total memory of the node.

Note: the OpenManage Cassandra assumes the whole node is only used by Cassandra. We may consider to limit the max cpu and memory for Cassandra container in the future.


Refs
[1] [Cassandra on AWS White Paper](https://d0.awsstatic.com/whitepapers/Cassandra_on_AWS.pdf)
[2] [Planning a Cassandra cluster on Amazon EC2](http://docs.datastax.com/en/landing_page/doc/landing_page/planning/planningEC2.html)
