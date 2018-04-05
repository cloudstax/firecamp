* [FireCamp Kafka Connect Internals](https://github.com/cloudstax/firecamp/tree/master/catalog/kafkaconnect#firecamp-kafka-connect-internals)
* [Tutorials](https://github.com/cloudstax/firecamp/tree/master/catalog/kafkaconnect#tutorials)

# FireCamp Kafka Connect Internals

The FireCamp Kafka Connect container is based on [confluentinc/cp-kafka-connect](https://hub.docker.com/r/confluentinc/cp-kafka-connect/). Each Kafka Connect container will only run one task by default. For different Kafka services, or different topics of one Kafka service, you could create multiple Kafka Connect services.

By default, FireCamp Kafka Connect works on the distributed mode. You could still create a connector service with 1 replica, and scale it later. In the future, we will add the monitoring ability and support scaling the connector replicas up and down automatically.

## Topic Auto Creation

There are multiple topics for the Connector. The data streaming topic, this is the actual topic that connector will get data from or send data to. You should create the topic with the expected partitions and replication factors before creating the connect service.

The connector also has 3 topics for the internal connector usage, config.storage.topic, offset.storage.topic, and status.storage.topic. The default Replication Factor for the config, offset and status topics is set to 3. The production Kafka environment should have at least 3 nodes across 3 availability zones. If you want higher replication factors, you could set such as -kc-replfactor=5 at the service creation. Or if you want to simply test with 1 Kafka replica, the topic replication factor of Kafka Connect should be set to 1 as well.

By default, the config topic has a single partition, and this should not be changed. The offset topic has 25 partitions. The status topic has 5 partitions. If you want different number of partitions for the offset and status topics, you could manually create the topics first before creating the Connect service.

## Converters

Currently only Json converter is supported. More converters will be supported in the coming releases.

## Transformations

[Kafka Connect provides the convenient tranformations to make lightweight message-at-a-time modifications](http://kafka.apache.org/documentation/#connect_transforms). For example, you could insert a new field into every record or replace some field in the record.

In the long term, we will support the custom function to handle the streaming data. The custom function could be ingested into the source/sink connector. For example, you could develop your own function to do the custom tranformation in the Sink ElasticSearch connector before ingesting data into ElasticSearch. The function could also be a AWS Lambda function. We will send the event to Lambda.

## Kafka Connect Configs

**GroupID**
The connector's group ID is formatted as "ClusterName-ServiceName".

**JVM Heap Size**
By default, FireCamp sets Kafka Connector JVM Xmx to 1GB. If your Kafka wants other memory, you could specify the "kc-heap-size" when creating the Kafka Connect service by the firecamp-service-cli.

**JVM TTL**
By default, JVM caches a successful DNS lookup forever. Kafka Connect Java client should [set JVM TTL](http://docs.aws.amazon.com/AWSSdkDocsJava/latest/DeveloperGuide/java-dg-jvm-ttl.html) to a reasonable value such as 60 seconds. So when Kafka container, ElasticSearch container or Kafka Connect container moves to another node, Kafka Connect JVM could lookup the new address.

## Sink ElasticSearch Connector

### [Configs](https://docs.confluent.io/3.1.0/connect/connect-elasticsearch/docs/configuration_options.html)

**max.buffer.records & batch.size**
If the connector hits OOM issue, you could increase JVM heap size or lower the max.buffer.records and batch.size to reduce the memory pressure. The default value is 20000 for max.buffer.records, and 2000 for batch.size. So if one record is 1KB, buffer size is 20MB, and one batch size is 2MB.

### [Managing Connectors](https://docs.confluent.io/3.1.0/connect/managing.html)

The task running in the connector may fail and require manual restart to recover. We will automate the connector management in the future.

You could periodically check the task status. For example, a connector service "mysinkes" is created in cluster "t1" with 3 replicas. You could install curl and jq (Command-line JSON processor) on your application node, and check the tasks status from any connector, such as `curl mysinkes-0.t1-firecamp.com:8083/connectors/t1-mysinkes/status | jq`. The results are like:
```
{
  "name": "t1-mysinkes",
  "connector": {
    "state": "RUNNING",
    "worker_id": "mysinkes-0.t1-firecamp.com:8083"
  },
  "tasks": [
    {
      "state": "RUNNING",
      "id": 0,
      "worker_id": "mysinkes-0.t1-firecamp.com:8083"
    },
    {
      "state": "RUNNING",
      "id": 1,
      "worker_id": "mysinkes-1.t1-firecamp.com:8083"
    },
    {
      "state": "RUNNING",
      "id": 2,
      "worker_id": "mysinkes-2.t1-firecamp.com:8083"
    }
  ],
  "type": "sink"
}
```

If any task is down, such as task 0, could restart the task, `curl -X POST mysinkes-0.t1-firecamp.com:8083/connectors/t1-mysinkes/tasks/0/restart`. Or restart all tasks, `curl -X POST mysinkes-0.t1-firecamp.com:8083/connectors/t1-mysinkes/restart`.

## Logging

The Kafka Connect logs are sent to the Cloud Logs, such as AWS CloudWatch logs. The container log is not service member aware. The log stream simply has the service name as prefix. While, the container logs its service member at startup. You could check the begining of the log and find which member the container is for.


# Tutorials

This is a simple tutorial about how to create a Kafka Connect service that sinks the data of one Kafka topic to ElasticSearch service. This tutorial assumes the cluster name is "t1", the AWS Region is "us-east-1".

1. Follow the [Installation](https://github.com/cloudstax/firecamp/tree/master/docs/installation) guide to create a 3 nodes cluster across 3 availability zones. Please make sure the node has enough memory for hosting all services. Or create more nodes, such as 6 nodes, for the cluster.

2. Follow the [ZooKeeper tutorials](https://github.com/cloudstax/firecamp/tree/master/catalog/zookeeper#tutorials) to create a 3 nodes ZooKeeper service.
```
firecamp-service-cli -op=create-service -service-type=zookeeper -region=us-east-1 -cluster=t1 -service-name=myzoo -replicas=3 -volume-size=20 -zk-heap-size=1024
```

3. Follow the [Kafka tutorials](https://github.com/cloudstax/firecamp/tree/master/catalog/kafka#tutorials) to create a 3 nodes Kafka service.
```
firecamp-service-cli -op=create-service -service-type=kafka -region=us-east-1 -cluster=t1  -replicas=3 -volume-size=100 -service-name=mykafka -kafka-zk-service=myzoo -kafka-heap-size=1024
```

4. Follow the [ElasticSearch tutorials](https://github.com/cloudstax/firecamp/tree/master/catalog/elasticsearch#tutorials) to create a 3 data nodes ElasticSearch services without the dedicated masters.
```
firecamp-service-cli -op=create-service -service-type=elasticsearch -region=us-east-1 -cluster=t1 -replicas=3 -volume-size=10 -service-name=myes -es-heap-size=1024 -es-disable-dedicated-master=true
```

5. Create the Kafka Connect service
First, create the topic that the ElasticSearch connector will read data from, `kafka-topics.sh --create --zookeeper myzoo-0.t1-firecamp.com --replication-factor 3 --partitions 1 --topic sinkes`.

Second, create the connect service. If the creation fails for any reason, please simply retry it.
```
firecamp-service-cli -op=create-service -service-type=kafkasinkes -region=us-east-1 -cluster=t1 -service-name=mysinkes -replicas=3 -kc-heap-size=512 -kc-kafka-service=mykafka -kc-kafka-topic=sinkes -kc-replfactor=3 -kc-sink-es-service=myes
```

This creates a Kafka Connect service with 3 replicas. The connector name will be "ClusterName-ServiceName", here will be 't1-mysinkes". The DNS names of the replicas would be: mysinkes-0.t1-firecamp.com, mysinkes-1.t1-firecamp.com, mysinkes-2.t1-firecamp.com.

6. Check Kafka Connect status. `curl -X GET http://mysinkes-0.t1-firecamp.com:8083/connectors/t1-mysinkes/status`. You could install jq to better format the json output.

If any task is down, such as task 0, could restart the task, `curl -X POST mysinkes-0.t1-firecamp.com:8083/connectors/t1-mysinkes/tasks/0/restart`. Or restart all tasks, `curl -X POST mysinkes-0.t1-firecamp.com:8083/connectors/t1-mysinkes/restart`.

7. Send Json format message to Kafka
```
kafka-console-producer.sh --broker-list mykafka-0.t1-firecamp.com:9092 --topic sinkes
{"name":"Test log", "severity": "INFO"}
{"name":"Test log 2", "severity": "WARN"}
```

8. Check the records in ElasticSearch
```
curl http://myes-0.t1-firecamp.com:9200/_cat/indices?v
curl http://myes-0.t1-firecamp.com:9200/sinkes/_search?pretty
```
