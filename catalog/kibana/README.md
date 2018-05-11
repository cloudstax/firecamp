# FireCamp Kibana Internals

The FireCamp Kibana container is based on the offical elastic.co kibana image, docker.elastic.co/kibana/kibana:5.6.3. The data volume will be mounted to the /data directory inside container. The Kibana data will be stored under /data/kibana.

## Kibana Cluster

**Topology**

One Kibana member only runs in one availability zone. To tolerate one availability zone failure, the Kibana service could have 2 replicas. FireCamp will distribute one replica to one availability zone. When one availability zone goes down, you could direct the Kibana client to the member in the other availability zone.

**Balance load across multiple ElasticSearch nodes**

Currently Kibana simply connects to the first member of the ElasticSearch service. Usually the ElasticSearch service will have more than 1 data nodes. As Kibana suggested, to [balance load across the ElasticSearch nodes](https://www.elastic.co/guide/en/kibana/5.6/production.html#load-balancing), FireCamp Kibana service will run a local ElasticSearch instance in the coordinating only mode in the coming releases.

**Memory**

At startup, Kibana may use up to 1.5g memory, even when the ElasticSearch service does not have much data. By default, FireCamp reserves 2GB memory for Kibana container.

## Security

The Kibana service inherits the FireCamp general security. The Kibana cluster will run in the internal service security group, which are not exposed to the internet. The nodes in the application security group could only access the Kibana service port, 5601. The Bastion nodes are the only nodes that could ssh to the Kibana nodes.

**Proxy**

It is best to run Kibana inside the internal service group. For the Kibana client outside AWS such as your local laptop, you need to set up a proxy service in the application security group. The proxy service will forward the requests to Kibana. The Kibana client should connect to the proxy service.

The proxy service should set up the basic authentication to protect the access to Kibana. For example, Nginx should use the htpasswd.users file.

Enabling Kibana SSL is also suggested to have better protection.

**SSL**

SSL is supported to encrypt communications between the client such as browser and the Kibana server. You could provide the ssl key and certificate files when creating the Kibana service.

## Logging

The Kibana logs are sent to the Cloud Logs, such as AWS CloudWatch logs.


Refs:

1. [Using Kibana in a Production Environment](https://www.elastic.co/guide/en/kibana/5.6/production.html)


# Tutorials

This is a simple tutorial about how to create a Kibana service and how to use it. This tutorial assumes the cluster name is "t1", the AWS Region is "us-east-1", and the Kibana service name is "mykb".

Follow the [Installation Guide](https://github.com/cloudstax/firecamp/pkg/tree/master/docs/installation) guide to create a 3 nodes cluster across 3 availability zones.

1. Create an ElasticSearch service:
```
firecamp-service-cli -op=create-service -service-type=elasticsearch -region=us-east-1 -cluster=t1 -replicas=3 -volume-size=10 -service-name=myes -es-heap-size=2048
```

2. Create a Kibana service:
```
firecamp-service-cli -op=create-service -service-type=kibana -region=us-east-1 -cluster=t1 -replicas=1 -volume-size=10 -service-name=mykb -kb-es-service=myes -reserve-memory=2048
```

3. Then you could ingest some data to ElasticSearch and explore it from Kibana console.
