The OpenManage ZooKeeper container is based on openjdk. The data volume will be mounted to the /data directory inside container. All ZooKeeper data will be stored under it.

By default, the Java heap size is set to 4GB. If your ZooKeeper wants other memory, you could specify the "max-memory" when creating the ZooKeeper service by the openmanage-service-cli.

**Topology**

The ZooKeeper cluster should have at least 3 nodes across 3 availability zones. This could tolerate 1 node or 1 availability zone failure. It is suggested to have 5 nodes across 3 or more availability zones, to tolerate 2 nodes or 1 (or more if the cluster has more availability zones) availability zone failure.

**Logging**

The ZooKeeper logs are sent to the Cloud Logs, such as AWS CloudWatch logs.

**Security**

By default, ZooKeeper could [control access using ACLs](http://zookeeper.apache.org/doc/r3.4.10/zookeeperProgrammers.html#sc_ZooKeeperAccessControl).

The simple example steps:

1. Login with zkCli.sh: `zkCli.sh -server host`

2. Create a znode: `create /zk_test mydata`

3. Add a new user: `addauth digest user1:password1`

4. Set acl for the znode: `setAcl /zk_test auth:user1:password1:crdwa`

In the future, [SASL](https://cwiki.apache.org/confluence/display/ZOOKEEPER/ZooKeeper+and+SASL) and [SSL](https://cwiki.apache.org/confluence/display/ZOOKEEPER/ZooKeeper+SSL+User+Guide) will be supported.
