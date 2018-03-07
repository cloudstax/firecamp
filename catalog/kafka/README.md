* [FireCamp Kafka Internals](https://github.com/cloudstax/firecamp/tree/master/catalog/kafka#firecamp-kafka-internals)
* [Tutorials](https://github.com/cloudstax/firecamp/tree/master/catalog/kafka#tutorials)

# FireCamp Kafka Internals

The FireCamp Kafka container is based on openjdk. The data volume will be mounted to the /data directory inside container. The Kafka logs will be stored under /data/kafka.

## Configs

The FireCamp Kafka configs follows [Kafka Configs for the Production Environment](http://docs.confluent.io/current/kafka/deployment.html).

**Replication Factor**
The default Replication Factor for Kafka is set to 3. The production environment should have at least 3 nodes across 3 availability zones. If the number of nodes is less than 3, the replication factor will be automatically adjusted to the number of nodes. This is for the developing and testing environment with 1 or 2 nodes.

**Topology**
The replicas of one topic are automatically distributed to the availability zones in the cluster. If the cluster has 3 availability zones, each zone will have one replica. The production cluster should have 3 availability zones, to tolerate the availability zone failure.

**Minimum In-sync Replica**
The Minimum In-sync Replica is set to 2 by default. Please set the "acks" to "all" or "-1" in the [producer configs](https://kafka.apache.org/documentation/#producerconfigs). When a producer sets acks to "all" (or "-1"), the min.insync.replicas specifies the minimum number of replicas that must acknowledge a write for the write to be considered successful. This guarantees that the message is not lost unless both hosts crash. If the cluster has only 1 node for developing and testing, the min.insync.replicas will be automatically adjusted to 1.

**Unclean Leader Election**
The Unclean Leader Election is disabled by default. If the unclean leader election is enabled, the out-of-sync replica is allowed to become the leader and messages that were not synced to the new leader are lost.

**Auto Topic Creation**
The auto topic creation is enabled by default. The default max number of partitions is 8. If the cluster has more than 8 nodes, the auto-created topic will have 8 partitions. If the total nodes are less than 8, the partitions of the auto-created topic will be the same with the number of nodes.

**Max Open Files**
The node's file descriptor count is increased to very large, 100,000, as Kafka uses a very large number of files.

**[JVM Configs](http://docs.confluent.io/current/kafka/deployment.html#jvm)**
Java 1.8 with G1 collector is used. The default Java heap size, both Xmx and Xms, are set to 6GB. If your Kafka wants other memory, you could specify the "kafka-heap-size" when creating the Kafka service by the firecamp-service-cli. The Java GC tuning also follow the recommendation.

**Set JVM TTL for Kafka Java client**
By default, JVM caches a successful DNS lookup forever. Kafka Java client should [set JVM TTL](http://docs.aws.amazon.com/AWSSdkDocsJava/latest/DeveloperGuide/java-dg-jvm-ttl.html) to a reasonable value such as 60 seconds. So when Kafka container moves to another node, JVM could lookup the new address.

**JMX**

By default, JMX is enabled to collect Kafka metrics. The JMX default listen port is 9093. You could specify the JMX user and password when creating the service. If you do not specify the JMX user and password, the default user is "jmxuser" and an UUID will be generated as the password.

The Kafka Manager could get and show Kafka metrics using the JMX user and password.

## Update the Service

If you want to increase the memory size, change the log retention hours, etc, you could update Kafka config via the cli. For example, to change the log retention hours, run `firecamp-service-cli -op=update-service -service-type=kafka -region=us-east-1 -cluster=t1 -service-name=mykafka -kafka-retention-hours=240`. See the firecamp cli help for the detail options, `firecamp-service-cli -op=update-service -service-type=kafka --help`.

The config changes will only be effective after the service is restarted. You could run `firecamp-service-cli -op=restart-service -service-name=mykafka` to rolling restart the service members when the system is not busy.

## Logging

The Kafka logs are sent to the Cloud Logs, such as AWS CloudWatch logs.

## Security

The [Kafka Security](http://docs.confluent.io/current/kafka/security.html) will be enabled in the coming releases.


Refs

1. [Design and Deployment Considerations for Deploying Apache Kafka on AWS](https://www.confluent.io/blog/design-and-deployment-considerations-for-deploying-apache-kafka-on-aws/)


# Tutorials

This is a simple tutorial about how to create a Kafka service and how to use it. This tutorial assumes the cluster name is "t1", the AWS Region is "us-east-1", and the Kafka service name is "mykafka".

## Create a Kafka ZooKeeper service
Follow the [Installation](https://github.com/cloudstax/firecamp/tree/master/docs/installation) guide to create a 3 nodes cluster across 3 availability zones. Could create a ZooKeeper cluster by:
```
firecamp-service-cli -op=create-service -service-type=zookeeper -region=us-east-1 -cluster=t1 -service-name=myzoo -replicas=3 -volume-size=20
```

This creates a 3 replicas ZooKeeper on 3 availability zones. Each replica has 20GB volume. The DNS names of the replicas would be: myzoo-0.t1-firecamp.com, myzoo-1.t1-firecamp.com, myzoo-2.t1-firecamp.com.

## Create a Kafka service
Create a Kafka cluster by:
```
firecamp-service-cli -op=create-service -service-type=kafka -region=us-east-1 -cluster=t1  -replicas=3 -volume-size=100 -service-name=mykafka -kafka-zk-service=myzoo -kafka-heap-size=6144
```

This creates a 3 replicas Kafka on 3 availability zones. The default heap size is 6g. If you want to reduce it for test such as 512MB, set -kafka-heap-size=512. Each replica has 100GB volume. The DNS names of the replicas would be: mykafka-0.t1-firecamp.com, mykafka-1.t1-firecamp.com, mykafka-2.t1-firecamp.com.

## Create Topic, Produce and Consume Messages
1. Create a test topic with 1 partition: `kafka-topics.sh --create --zookeeper myzoo-0.t1-firecamp.com --replication-factor 3 --partitions 1 --topic testtopic`
2. Describe the topic: `kafka-topics.sh --describe --zookeeper myzoo-0.t1-firecamp.com --topic testtopic`.
The output shows the partition leader is the first member, mykafka-0.t1-firecamp.com.
```
Topic:testtopic PartitionCount:1  ReplicationFactor:3 Configs:
  Topic: testtopic  Partition: 0  Leader: 0 Replicas: 0,1,2 Isr: 0,1,2
```
3. Produce 2 messages to the partition leader.
```
kafka-console-producer.sh --broker-list mykafka-0.t1-firecamp.com:9092 --topic testtopic
>this is message1
>this is message2
```
4. Consume the messages
```
kafka-console-consumer.sh --bootstrap-server mykafka-0.t1-firecamp.com:9092 --from-beginning --topic testtopic
this is message1
this is message2
```
