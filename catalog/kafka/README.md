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

**[JVM Configs](http://docs.confluent.io/current/kafka/deployment.html#jvm)**
Java 1.8 with G1 collector is used. The default Java heap size, both Xmx and Xms, are set to 6GB. If your Kafka wants other memory, you could specify the "max-memory" when creating the Kafka service by the firecamp-service-cli. The Java GC tuning also follow the recommendation.

**Max Open Files**
The node's file descriptor count is increased to very large, 100,000, as Kafka uses a very large number of files.


## Logging

The Kafka logs are sent to the Cloud Logs, such as AWS CloudWatch logs.

## Security

The [Kafka Security](http://docs.confluent.io/current/kafka/security.html) will be enabled in the coming releases.

