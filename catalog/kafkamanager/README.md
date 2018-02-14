# FireCamp Kafka Manager Internals

[Kafka Manager](https://github.com/yahoo/kafka-manager) is the open source tool from Yahoo. The FireCamp Kafka Manager container is based on Debian Jessie.

Kafka Manager stores its data on ZooKeeper. There is only 1 replica of Kafka Manager running. Usually you will only need to have one Kafka Manager service in one cluster, as one Kafka Manager could manage multiple Kafka clusters. If you want to run multiple Kafka Manager services, please make sure they talk with different ZooKeeper services.

## Configs

**JVM Heap Size**
The default Java heap size, both Xmx and Xms, are set to 4GB. If your Kafka Manager wants other memory, you could specify the "km-heap-size" when creating the Kafka Manager service by the firecamp-service-cli.

**Set JVM TTL for Kafka Java client**
By default, JVM caches a successful DNS lookup forever. The [JVM TTL](http://docs.aws.amazon.com/AWSSdkDocsJava/latest/DeveloperGuide/java-dg-jvm-ttl.html) is set to 60 seconds. So when Kafka container or  moves to another node, JVM could lookup the new address.

**User and Password**
By default, user and password is not required. You could specify the Kafka Manager user and password when creating the service.

## Logging

The Kafka Manager logs are sent to the Cloud Logs, such as AWS CloudWatch logs.


# Tutorials

Assume the cluster name is "t1", the AWS Region is "us-east-1" and the existing ZooKeeper service name is "myzoo". You could create the kafka Manager service by:
```
firecamp-service-cli -op=create-service -service-type=kafkamanager -region=us-east-1 -cluster=t1 -service-name=mykm -km-zk-service=myzoo -km-heap-size=4096 -km-user=u1 -km-passwd=p1
```

By default, the FireCamp service is only accessible from the AppAccessSecurityGroup. To access the Kafka Manager service on the FireCamp cluster, you will need to set up an EC2 instance in the AppAccessSecurityGroup. The EC2 instance should have browser to access Kafka Manager service, or have the proxy server to proxy the access from the internet.
