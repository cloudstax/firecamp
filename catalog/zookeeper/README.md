* [FireCamp ZooKeeper Internals](https://github.com/cloudstax/firecamp/tree/master/catalog/zookeeper#firecamp-zookeeper-internals)
* [Tutorials](https://github.com/cloudstax/firecamp/tree/master/catalog/zookeeper#tutorials)

# FireCamp ZooKeeper Internals

The FireCamp ZooKeeper container is based on the [official ZooKeeper image](https://hub.docker.com/_/zookeeper/). The data volume will be mounted to the /data directory inside container. All ZooKeeper data will be stored under it.

## Topology

The ZooKeeper cluster should have at least 3 nodes across 3 availability zones. This could tolerate 1 node or 1 availability zone failure. It is suggested to have 5 nodes across 3 or more availability zones, to tolerate 2 nodes or 1 (or more if the cluster has more availability zones) availability zone failure.

## Security

By default, ZooKeeper could [control access using ACLs](http://zookeeper.apache.org/doc/r3.4.10/zookeeperProgrammers.html#sc_ZooKeeperAccessControl).

The simple example steps:

1. Login with zkCli.sh: `zkCli.sh -server host`

2. Create a znode: `create /zk_test mydata`

3. Add a new user: `addauth digest user1:password1`

4. Set acl for the znode: `setAcl /zk_test auth:user1:password1:crdwa`

In the future, [SASL](https://cwiki.apache.org/confluence/display/ZOOKEEPER/ZooKeeper+and+SASL) and [SSL](https://cwiki.apache.org/confluence/display/ZOOKEEPER/ZooKeeper+SSL+User+Guide) will be supported.

## JVM Configs

By default, the Java heap size is set to 4GB. If your ZooKeeper wants other memory, you could specify the "zk-heap-size" when creating the ZooKeeper service by the firecamp-service-cli.

**Set JVM TTL for ZooKeeper Java client**

By default, JVM caches a successful DNS lookup forever. ZooKeeper Java client should [set JVM TTL](http://docs.aws.amazon.com/AWSSdkDocsJava/latest/DeveloperGuide/java-dg-jvm-ttl.html) to a reasonable value such as 60 seconds. So when ZooKeeper container moves to another node, JVM could lookup the new address.

## Logging

The ZooKeeper logs are sent to the Cloud Logs, such as AWS CloudWatch logs.


# Tutorials

This is a simple tutorial about how to create a ZooKeeper service and how to use it. This tutorial assumes the cluster name is "t1", the AWS Region is "us-east-1", and the ZooKeeper service name is "myzoo".

## Create a ZooKeeper service
Follow the [Installation](https://github.com/cloudstax/firecamp/tree/master/docs/installation) guide to create a 3 nodes cluster across 3 availability zones. Could create a ZooKeeper cluster by:
```
firecamp-service-cli -op=create-service -service-type=zookeeper -region=us-east-1 -cluster=t1 -service-name=myzoo -replicas=3 -volume-size=20 -zk-heap-size=4096
```

This creates a 3 replicas ZooKeeper on 3 availability zones. The default heap size is 4g. If you want to reduce it for test such as 256MB, set -zk-heap-size=256. Each replica has 20GB volume. The DNS names of the replicas would be: myzoo-0.t1-firecamp.com, myzoo-1.t1-firecamp.com, myzoo-2.t1-firecamp.com.

## Create znode, user and acl
1. Login with zkCli.sh: `zkCli.sh -server myzoo-0.t1-firecamp.com`

2. Create a znode: `create /zk_test mydata`

3. Add a new user: `addauth digest user1:password1`

4. Set acl for the znode: `setAcl /zk_test auth:user1:password1:crdwa`
