# FireCamp ElasticSearch Internals

The FireCamp ElasticSearch container is based on the offical elastic.co image, docker.elastic.co/elasticsearch/elasticsearch:5.6.3. The data volume will be mounted to the /data directory inside container. The ElasticSearch data will be stored under /data/es.

## ElasticSearch Cluster

**Dedicated master nodes**

See [ElasticSearch modules node](https://www.elastic.co/guide/en/elasticsearch/reference/current/modules-node.html). ElasticSearch supports 3 node types: master, data, and ingest. The [tribe node is deprecated in 5.4.0](https://www.elastic.co/guide/en/elasticsearch/reference/current/modules-tribe.html). By default, one node serves all 3 types. ElasticSearch suggests to have dedicated master nodes for the large cluster. "By default a node is a master-eligible node and a data node, plus it can pre-process documents through ingest pipelines. This is very convenient for small clusters but, as the cluster grows, it becomes important to consider separating dedicated master-eligible nodes from dedicated data nodes."

AWS ElasticSearch Service also suggests to [allocate 3 dedicated master nodes](http://docs.aws.amazon.com/elasticsearch-service/latest/developerguide/es-managedomains.html#es-managedomains-dedicatedmasternodes), and have suggestions about the instance type according to the cluster size.

By default, FireCamp ElasticSearch will allocate 3 dedicated master nodes, if the service has more than 1 data node. Assume the single node is used for the test purpose. Some cluster may only have light load and small amount of data. You could select to disable the dedicated master nodes when creating the service, set disable-dedicated-master=true. But if the cluster has more than 10 data nodes, FireCamp will ignore the disable-dedicated-master flag and enforce to allocate 3 dedicated master nodes.

The master nodes of FireCamp ElasticSearch share the same configs with the data nodes. Every data node will serve data and ingest. The first 3 members will be the dedicated master nodes. For example, you create an 6 replicas ElasticSearch service named myes on cluster t1. The service will have total 9 members. The first 3 members, myes-0.t1-firecamp.com, myes-1.t1-firecamp.com and myes-2.t1-firecamp.com, are the dedicated masters. The other 6 members, myes-3~8.t1-firecamp.com are data and ingest nodes.

The dedicated master nodes will be distributed to all availability zones, to tolerate the availability zone failure.

**Topology**

ElasticSearch is aware of the server's availability zone, and the awareness is forced. See the [allocation awareness](https://www.elastic.co/guide/en/elasticsearch/reference/current/allocation-awareness.html). For example, the cluster has 3 nodes on 3 availability zones, 1 node per zone. Create the index with 5 shards and 2 replicas. The 2 replicas of one shard will be allocated to the other 2 zones. If one zone goes down, the missing replicas will not be reassigned to the remaining zones. Usually the zone failure is temporary and AWS will usually fix it soon. The force awareness could avoid the possible churn introduced by the recovery of the missing replicas, which requires to copy data to recreate the replicas. If you want to recover the missing replicas, could set disable-force-awareness=true when creating the service.

## ElasticSearch Configs

**Network port**

[ElasticSearch network](https://www.elastic.co/guide/en/elasticsearch/reference/current/modules-network.html) suggests 9200-9300 for http.port and 9300-9400 for transport.tcp.port. FireCamp ElasticSearch publishes 9200 and 9300.

**[Important System Configs](https://www.elastic.co/guide/en/elasticsearch/reference/current/system-config.html)**

The nofile and nproc are increased. The vm.swappiness is set to 1. The vm.max_map_count is set to 262144.

**[JVM Heap](https://www.elastic.co/guide/en/elasticsearch/reference/current/heap-size.html)**

ElasticSearch suggests to set Xms and Xmx to the same. The default heap size is 2g. You should increase it for the production system, by setting the "es-heap-size" when creating the service.

It is better to not "Set Xmx to no more than 50% of your physical RAM, to ensure that there is enough physical RAM left for kernel file system caches." And should try to stay below the threshold, "26 GB is safe on most systems". FireCamp limits the Xmx under 26GB.

**Config update via API**

Currently the configs could be updated via ElasticSearch setting API. But if the container get restarted, the configs will be reset back. This will be enhanced in the coming release.

**Set JVM TTL for Java client**

By default, JVM caches a successful DNS lookup forever. The Java client should [set JVM TTL](http://docs.aws.amazon.com/AWSSdkDocsJava/latest/DeveloperGuide/java-dg-jvm-ttl.html) to a reasonable value such as 60 seconds. So when the ElasticSearch container moves to another node, JVM could lookup the new address.

## Security

The ElasticSearch cluster inherits the FireCamp general security. The ElasticSearch cluster will run in the internal service security group, which are not exposed to the internet. The nodes in the application security group could only access the ElasticSearch service port, 9200. The Bastion nodes are the only nodes that could ssh to the ElasticSearch nodes.

**X-Pack**

FireCamp ElasticSearch currently supports the open source version (xpack.security disabled). X-Pack will be supported in the future.

**Free Security Plugins**

There are multiple free security plugins for ElasticSearch, such as [HTTP Authentication plugin](https://github.com/elasticfence/elasticsearch-http-user-auth), [SearchGuard](https://github.com/floragunncom/search-guard/tree/5.0.0). We will consider to support them in the future.

## Logging

The ElasticSearch logs are sent to the Cloud Logs, such as AWS CloudWatch logs.


Refs:

1. [Install ElasticSearch with Docker](https://www.elastic.co/guide/en/elasticsearch/reference/current/docker.html)


# Tutorials

This is a simple tutorial about how to create a ElasticSearch service and how to use it. This tutorial assumes the cluster name is "t1", the AWS Region is "us-east-1", and the ElasticSearch service name is "myes".

## Create an ElasticSearch service
Follow the [Installation Guide](https://github.com/cloudstax/firecamp/tree/master/docs/installation) guide to create a 3 nodes cluster across 3 availability zones. Create a ElasticSearch cluster:
```
firecamp-service-cli -op=create-service -service-type=elasticsearch -region=us-east-1 -cluster=t1 -replicas=3 -volume-size=10 -service-name=myes
```

This creates a 3 dedicated master nodes and 3 data nodes ElasticSearch cluster on 3 availability zones. The master nodes and data nodes will be evenly distributed to different availability zone. So the ElasticSearch service could tolerate one availability zone failure. The default heap size is 2g. To change the heap size, such as 512MB for test, set -es-heap-size=512. Every node has 10GB volume. The master node DNS names are: myes-0.t1-firecamp.com, myes-1.t1-firecamp.com, myes-2.t1-firecamp.com. The data node DNS names are: myes-3.t1-firecamp.com, myes-4.t1-firecamp.com, myes-5.t1-firecamp.com.

## Check ElasticSearch Cluster
Launch a EC2 instance in the VPC and AppAccessSecurityGroup, which are in the output of the CloudFormation stack that launches the FireCamp cluster. Use [ElasticSearch Cluster API](https://www.elastic.co/guide/en/elasticsearch/reference/current/cluster.html) to check the cluster. The command could run against any master or data node.
1. Check cluster health: `curl -XGET 'myes-0.t1-firecamp.com:9200/_cluster/health?pretty'`
2. Check cluster state: `curl -XGET 'http://myes-3.t1-firecamp.com:9200/_cluster/state'`
3. Check cluster stats: `curl -XGET 'http://myes-5.t1-firecamp.com:9200/_cluster/stats?human&pretty'`

## Index and Search
1. Index some tweets:
```
curl -XPUT 'http://myes-3.t1-firecamp.com:9200/twitter/doc/1?pretty' -H 'Content-Type: application/json' -d '
{
    "user": "kimchy",
    "post_date": "2009-11-15T13:12:00",
    "message": "Trying out Elasticsearch, so far so good?"
}'

curl -XPUT 'http://myes-3.t1-firecamp.com:9200/twitter/doc/2?pretty' -H 'Content-Type: application/json' -d '
{
    "user": "kimchy",
    "post_date": "2009-11-15T14:12:12",
    "message": "Another tweet, will it be indexed?"
}'

curl -XPUT 'http://myes-3.t1-firecamp.com:9200/twitter/doc/3?pretty' -H 'Content-Type: application/json' -d '
{
    "user": "elastic",
    "post_date": "2010-01-15T01:46:38",
    "message": "Building the site, should be kewl"
}'
```
2. Get the tweets:
```
curl -XGET 'http://myes-3.t1-firecamp.com:9200/twitter/doc/1?pretty=true'
curl -XGET 'http://myes-3.t1-firecamp.com:9200/twitter/doc/2?pretty=true'
curl -XGET 'http://myes-3.t1-firecamp.com:9200/twitter/doc/3?pretty=true'
```
3. Find all the tweets that kimchy posted.

Using the query string: `curl -XGET 'http://myes-3.t1-firecamp.com:9200/twitter/_search?q=user:kimchy&pretty=true'`

Using the JSON query language Elasticsearch provides:
```
curl -XGET 'http://myes-3.t1-firecamp.com:9200/twitter/_search?pretty=true' -H 'Content-Type: application/json' -d '
{
    "query" : {
        "match" : { "user": "kimchy" }
    }
}'
```
