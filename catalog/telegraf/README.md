* [FireCamp Telegraf Internals](https://github.com/cloudstax/firecamp/tree/master/catalog/telegraf#firecamp-telegraf-internals)
* [Tutorials](https://github.com/cloudstax/firecamp/tree/master/catalog/telegraf#tutorials)

# FireCamp Telegraf Internals

The FireCamp Telegraf container is based on the [official Telegraf image](https://hub.docker.com/_/telegraf/).

One Telegraf service will monitor one stateful service and send the metrics to AWS CloudWatch. For example, you create a Redis service named "myredis" and want to see the Redis metrics. You could create a Telegraf service for the redis service, view the redis metrics and create the dashboard on AWS CloudWatch. By default, FireCamp Telegraf collects the metrics every 60 seconds. If you want to change the collection interval, you could specify "-tel-collect-interval" when creating the service.

You could limit the Telegraf service memory usage by setting the "max-memory" option when creates the service.

## Custom Metrics

You will pay for CloudWatch usage. If the service generates lots of metrics, the cost may not be low. Please refer to [CloudWatch Pricing](https://aws.amazon.com/cloudwatch/pricing/) for the details. If you want to reduce the cost, you could customize the metrics to monitor. You could put all the custom metrics in one file, and pass the file "-tel-metrics-file=pathtofile" when creating the service. For example, you could create the file with a few metrics for Cassandra:
```
    "/org.apache.cassandra.metrics:type=ClientRequest,scope=Read,name=Latency",
    "/org.apache.cassandra.metrics:type=ClientRequest,scope=Write,name=Latency",
    "/org.apache.cassandra.metrics:type=Storage,name=Load"
```

Customizing the metrics is the advanced configuration. Currently FireCamp does not check the format in the custom file. It is your responsibility to ensure the format and metrics are correct. For example, refer to "metrics" part in [Cassandra configs](https://github.com/cloudstax/firecamp/tree/master/catalog/telegraf/1.5/dockerfile/input_cas.conf) for the detail metrics format. FireCamp will simply replace the "metrics" part with the custom metrics in the file.

Currently not every service in Telegraf supports to customize the metrics. For example, Telegraf Redis gathers the results of the [INFO](https://redis.io/commands/info) redis command.

In the later releases, we will support Prometheus and Grafana to store and show the metrics.

## CloudWatch Limits

CloudWatch has a few limitations on the metrics retention. For example, "Data points with a period of 60 seconds (1 minute) are available for 15 days", and "After 15 days this data is still available, but is aggregated and is retrievable only with a resolution of 5 minutes. After 63 days, the data is further aggregated and is available with a resolution of 1 hour.". For more details, please refer to [CloudWatch Metrics](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/cloudwatch_concepts.html#Metric).

Note: CloudWatch does not support deleting metric, has to wait till it is automatically removed.

## Expose Ports

FireCamp Telegraf does not expose any port, and not listen on any port.

Telegraf Dockerfile expose 8125/udp 8092/udp 8094. 8125 is the port that the [statsD input plugin](https://github.com/influxdata/telegraf/tree/master/plugins/inputs/statsd) listens on. 8092 is use by the [udp listener plugin](https://github.com/influxdata/telegraf/tree/master/plugins/inputs/udp_listener), which is deprecated in favor of the socket listener plugin. 8094 the port that the [socket listener plugin](https://github.com/influxdata/telegraf/tree/master/plugins/inputs/socket_listener) listens on, to listens for messages from streaming (tcp, unix) or datagram (udp, unixgram) protocols.

Currently FireCamp Telegraf is only used to monitor the stateful service. So these ports are not exposed. Telegraf simply collects from the stateful service it monitors. And FireCamp does not assign the DNS name for the Telegraf service.


# Tutorials

This is a simple tutorial about how to create a Telegraf service and how to use it. This tutorial assumes the cluster name is "t1", the AWS Region is "us-east-1", and the Telegraf service name is "mytel-redis".

Firstly, create a Redis service:
```
firecamp-service-cli -op=create-service -service-type=redis -region=us-east-1 -cluster=t1 -redis-shards=3 -redis-replicas-pershard=2 -volume-size=20 -service-name=myredis -redis-memory-size=4096 -redis-auth-pass=changeme
```

Create the Telegraf service:
```
firecamp-service-cli -op=create-service -service-type=telegraf -region=us-east-1 -cluster=t1 -service-name=tel-myredis -tel-monitor-service-name=myredis
```

Then you could view the metrics on the [CloudWatch console](https://console.aws.amazon.com/cloudwatch), and create the dashboard.
