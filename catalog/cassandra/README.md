* [FireCamp Cassandra Internals](https://github.com/jazzl0ver/firecamp/pkg/tree/master/catalog/cassandra#firecamp-cassandra-internals)
* [Tutorials](https://github.com/jazzl0ver/firecamp/pkg/tree/master/catalog/cassandra#tutorials)

# FireCamp Cassandra Internals

The FireCamp Cassandra container is based on the [official Cassandra image](https://hub.docker.com/_/cassandra/). Two volumes will be created, one for the commit log, the other for all other data. The commit log volume will be mounted to the /journal directory inside container. The data volume will be mounted to the /data directory inside container. Cassandra commit log is stored under /journal directory. All other Cassandra data, including data file, hints, and the key cache, will be stored under /data directory.

## Topology

Cassandra Ec2Snitch could load the AWS region and availability zone from the EC2 API. But Ec2Snitch could not support other clouds, such as Google or Microsoft Cloud. So the GossipingPropertyFileSnitch is used to teach Cassandra about the topology. Every member will record the region as dc, and the availability zone as rack.

Only one region is supported currently. The multi-regions Cassandra will be supported in the future.

## Monitor

FireCamp supports to monitor Cassandra service using Telegraf. Please refer to [FireCamp Telegraf Internals](https://github.com/jazzl0ver/firecamp/pkg/tree/master/catalog/telegraf#firecamp-telegraf-internals) for more details. If you customize the metrics you want to monitor, please pay attention to the data format in the metrics file. Each line includes one metric. Every metric should have the quotation marks and end with comma. The last metri should not end with comma. Example:
```
    "/org.apache.cassandra.metrics:type=ClientRequest,scope=Read,name=Latency",
    "/org.apache.cassandra.metrics:type=ClientRequest,scope=Write,name=Latency",
    "/org.apache.cassandra.metrics:type=Storage,name=Load"
```

## Scale

Scaling a FireCamp Cassandra service requires 4 steps. For example, scaling a 3 replicas Cassandra service to 6 replicas. Cassandra service name is "mycas". Cluster name is "t1". If you want to scale to a large number of replicas, please repeat below steps to scale 3 replicas at one time.

1. Scale the cluster nodes. For example, on AWS, go to the [EC2 AutoScalingGroup console](https://console.aws.amazon.com/ec2/autoscaling) and scale the FireCamp cluster nodes.

2. Scale the replicas using firecamp cli.
```
firecamp-service-cli -op=scale-service -service-type=cassandra -region=us-east-1 -cluster=t1 -service-name=mycas -replicas=6
```

3. Use [nodetool status](https://docs.datastax.com/en/cassandra/3.0/cassandra/tools/toolsStatus.html) to verify the new replicas are fully bootstrapped and all other nodes are up (UN).
```
nodetool -h mycas-3.t1-firecamp.com -u jmxuser -pw changeme status
nodetool -h mycas-4.t1-firecamp.com -u jmxuser -pw changeme status
nodetool -h mycas-5.t1-firecamp.com -u jmxuser -pw changeme status
```

4. Run [nodetool cleanup](https://docs.datastax.com/en/cassandra/3.0/cassandra/tools/toolsCleanup.html) on each of the previously existing nodes to remove the keys that no longer belong to those nodes. Wait for cleanup to complete on one node before running nodetool cleanup on the next node. Cleanup can be safely postponed for low-usage hours.
```
nodetool -h mycas-0.t1-firecamp.com -u jmxuser -pw changeme cleanup
nodetool -h mycas-1.t1-firecamp.com -u jmxuser -pw changeme cleanup
nodetool -h mycas-2.t1-firecamp.com -u jmxuser -pw changeme cleanup
```

Currently FireCamp only supports scaling from 3 replicas. Scaling from 1 replica is not supported yet. Scaling down the replicas are not supported as well. If you want to stop the service when idle, could run `firecamp-service-cli -op=stop-service -service-name=mycas`. This will stop the service containers, while the volumes are still there. Later could start the containers with `firecamp-service-cli -op=start-service -service-name=mycas`.

### Troubleshooting

If the new replicas do not work, probably the new node is not correctly added to the cluster.

1. Check if the new node is added to the container orchestration framework.

For ECS, go to [ECS console](https://console.aws.amazon.com/ecs), click the right cluster, and check if the new nodes show up in the container instances. If not, find the corresponding EC2 instance and terminate it. The AutoScalingGroup will create and initialize a new EC2 instance automatically.

For Docker Swarm, login to swarm manager node, run `docker node ls` and check if the new node is added.

2. Check the firecamp volume and log plugin are correctly installed.

Login to the EC2 instance, run `sudo docker plugin ls`. If firecamp volume or log plugin is not installed or not enabled, terminate the corresponding EC2 instance. The AutoScalingGroup will create and initialize a new EC2 instance automatically.

## Security

The FireCamp Cassandra follows the official [security guide](http://cassandra.apache.org/doc/latest/operating/security.html). The Authentication and Authorization are enabled by default. And the replication factor of the system_auth keyspace (default 1) is set to 3 to tolerate the node down.

The TLS/SSL encryption will be supported in the future. This would not be a big security risk. The Cassandra cluster on the FireCamp platform could only be accessed by the nodes running on the AppAccessSecurityGroup and the same VPC. And both Authentication and Authorization are enabled.

After the Cassandra cluster is initialized, please login to create a new superuser and disable the default "cassandra" superuser. For example, cluster name is t1, cassandra service name is mycas. The steps are:

1. Login to the cluster: cqlsh mycas-0.t1-firecamp.com -u cassandra -p cassandra

2. Create a new superuser: CREATE ROLE newsuperuser WITH SUPERUSER = true AND LOGIN = true AND PASSWORD = 'super';

3. Log out.

4. Login to the cluster with the new superuser: cqlsh mycas-0.t1-firecamp.com -u newsuperuser -p super

5. Disable the default superuser: ALTER ROLE cassandra WITH SUPERUSER = false AND LOGIN = false;

6. Finally, set up the roles and credentials for your application users with CREATE ROLE statements.


## Cache

The Cassandra key cache is enabled, and the row cache is disabled. The key cache size is automatically calculated based on the total memory of the node.

Note: the FireCamp Cassandra assumes the whole node is only used by Cassandra. We may consider to limit the max cpu and memory for Cassandra container in the future.


## Configs

**[JMX](https://docs.datastax.com/en/cassandra/3.0/cassandra/configuration/secureJmxAuthentication.html)**

By default, JMX is enabled to allow tools such as nodetool to access the Cassandra replica remotely. You could specify the JMX user and password when creating the service. If you do not specify the JMX user and password, the default user is "jmxuser" and an UUID will be generated as the password.

**[JVM Configs](https://docs.datastax.com/en/cassandra/2.1/cassandra/operations/ops_tune_jvm_c.html)**

The default Java heap size, both Xmx and Xms, are set to 8GB. The max heap size is 14GB, as Cassandra recommends. If your wants other memory, you could specify the "cas-heap-size" when creating the Cassandra service by the firecamp-service-cli. Note: if the heap size is less than 1GB and you run some load, JVM may stall long time at GC.

**Set JVM TTL for Java CQL driver**

By default, JVM caches a successful DNS lookup forever. If you use Cassandra Java CQL driver, please [set JVM TTL](http://docs.aws.amazon.com/AWSSdkDocsJava/latest/DeveloperGuide/java-dg-jvm-ttl.html) to a reasonable value such as 60 seconds. So when Cassandra container moves to another node, Java CQL driver could lookup the new address.

**System Configs**

Follow the [Cassandra Recommended Settings](https://docs.datastax.com/en/dse/5.1/dse-dev/datastax_enterprise/config/configRecommendedSettings.html). The number of file descriptors and threads are increased. The TCP tunings are applied. The memory swapping is disabled, memlock is set to unlimited, and vm_map_count is increased. Please refer to [FireCamp system configs](https://github.com/jazzl0ver/firecamp/pkg/tree/master/catalog/README.md) for the detail configs.

## Update the Service

If you want to increase the heap size, or change the jmx user/password, you could update the config via the cli. For example, to change the heap size, run `firecamp-service-cli -op=update-service -service-type=cassandra -region=us-east-1 -cluster=t1 -service-name=mycas -cas-heap-size=9216`. See the firecamp cli help for the detail options, `firecamp-service-cli -op=update-service -service-type=cassandra --help`.

The config changes will only be effective after the service is restarted. You could run `firecamp-service-cli -op=restart-service -service-name=mycas` to rolling restart the service members when the system is not busy.

## Logging

The Cassandra logs are sent to the Cloud Logs, such as AWS CloudWatch logs.


Refs:

[1] [Cassandra on AWS White Paper](https://d0.awsstatic.com/whitepapers/Cassandra_on_AWS.pdf)

[2] [Planning a Cassandra cluster on Amazon EC2](http://docs.datastax.com/en/landing_page/doc/landing_page/planning/planningEC2.html)

[3] [Cassandra Recommended Settings](https://docs.datastax.com/en/dse/5.1/dse-dev/datastax_enterprise/config/configRecommendedSettings.html)

# Tutorials

This is a simple tutorial about how to create a Cassandra service and how to use it. This tutorial assumes the cluster name is "t1", the AWS Region is "us-east-1", and the Cassandra service name is "mycas".

## Create a Cassandra service
Follow the [Installation](https://github.com/jazzl0ver/firecamp/pkg/tree/master/docs/installation) guide to create a 3 nodes cluster across 3 availability zones. Create a Cassandra cluster:
```
firecamp-service-cli -op=create-service -service-type=cassandra -region=us-east-1 -cluster=t1 -service-name=mycas -replicas=3 -volume-size=100 -journal-volume-size=10 -cas-heap-size=8192 -cas-audit-logging-enabled=true -cas-audit-included-keyspaces="ks1,ks2" -cas-audit-included-categories="QUERY,DDL,DCL" -cas-audit-included-users="myuser"
```

This creates a 3 replicas Cassandra on 3 availability zones. Each replica has 2 volumes, 10GB volume for journal and 100GB volume for data. The DNS names of the replicas would be: mycas-0.t1-firecamp.com, mycas-1.t1-firecamp.com, mycas-2.t1-firecamp.com. To reduce the heap size for the simple test, could set such as -cas-heap-size=512. Audit logging is enabled for certain keyspaces and users only (https://cassandra.apache.org/doc/4.1/cassandra/operating/audit_logging.html)

The Cassandra service creation steps:
1. Create the Volumes and persist the metadata to the FireCamp DB. This is usually very fast. But if AWS is slow at creating the Volume, this step will be slow as well.
2. Create the service on the container orchestration framework, such as AWS ECS, and wait for all service containers running. The speed depends on how fast the container orchestration framework could start all containers.
3. Wait some time (10 seconds) for Cassandra to stabilize.
4. Start the Cassandra initialization task. The speed also depends on how fast the container orchestration framework schedules the task.
5. The initialization task does:
   * Increase the replication factor for the system_auth keyspace to 3.
   * Notify the manage server to do the post initialization work.
6. The manage server will set the Cassandra service initialized.

In case the service creation fails, please simply retry it. Once the service is initialized, you could create a new super user.

## Check Cassandra service status

The Cassandra service creation will return the cassandra jmx password, such as `2018-02-11 21:13:10.524442554 +0000 UTC The catalog service is created, jmx user jmxuser password 9ba31d4787504894638611e0aeb91c93`.

To double check Cassandra is successfully initialized and includes all replicas, run `nodetool -h mycas-0.t1-firecamp.com -u jmxuser -pw 9ba31d4787504894638611e0aeb91c93 status`. It should include all replicas. In case, if some replica is not included, for example, you create a 6 replicas Cassandra service, but nodetool shows only 5 members, use firecamp-service-cli to stop the services, `firecamp-service-cli -region=us-east-1 -cluster=t1 -op=stop-service -service-name=mycas`, and start again with `firecamp-service-cli -region=us-east-1 -cluster=t1 -op=start-service -service-name=mycas`. Then check with nodetool again.

## Create the New Super User
Cassandra has a default super user, name "cassandra" and password "cassandra". After the Cassandra service is initialized, please login to create a new superuser and disable the default "cassandra" superuser. The steps are:

1. Login to the cluster: `cqlsh mycas-0.t1-firecamp.com -u cassandra -p cassandra`
2. Create a new superuser: `CREATE ROLE newsuperuser WITH SUPERUSER = true AND LOGIN = true AND PASSWORD = 'super';`
3. Log out.
4. Login to the cluster with the new superuser: `cqlsh mycas-0.t1-firecamp.com -u newsuperuser -p super`
5. Disable the default superuser: `ALTER ROLE cassandra WITH SUPERUSER = false AND LOGIN = false;`
6. Finally, set up the roles and credentials for your application users with CREATE ROLE statements.

## Create Keyspace and User
1. Login with the new superuser: `cqlsh mycas-0.t1-firecamp.com -u newsuperuser -p super`
2. Create a keyspace and table
```
CREATE KEYSPACE test WITH REPLICATION = { 'class' : 'NetworkTopologyStrategy', 'us-east-1' : 3 };
use test;
CREATE TABLE users (userid text PRIMARY KEY, first_name text, last_name text);
```
3. Create role for the keyspace and table
```
CREATE ROLE supervisor;
GRANT MODIFY ON test.users TO supervisor;
GRANT SELECT ON test.users TO supervisor;
CREATE ROLE pam WITH PASSWORD = 'password' AND LOGIN = true;
GRANT supervisor TO pam;
LIST ALL PERMISSIONS OF pam;
```
4. Login as the new "pam" role: `cqlsh mycas-2.t1-firecamp.com -u pam -p password`
5. Insert new records:
```
use test;
insert into users (userid, first_name, last_name) values('user1', 'a1', 'b1');
select * from users;
```

## Monitor Cassandra with AWS CloudWatch
Create a Telegraf service to collect Cassandra metrics and send to AWS CloudWatch. By default, the metrics is collected every 60s. If you want different collect interval, such as 90 seconds, could set "-tel-collect-interval=90s".
```
firecamp-service-cli -op=create-service -service-type=telegraf -region=us-east-1 -cluster=t1 -service-name=tel-mycas -tel-monitor-service-name=mycas -tel-monitor-service-type=cassandra
```

You could put all the custom metrics in one file, and pass the file "-tel-metrics-file=pathtofile" when creating the service. For example, you could create the file with a few metrics for Cassandra:
```
  "/org.apache.cassandra.metrics:type=ClientRequest,scope=Read,name=Latency",
  "/org.apache.cassandra.metrics:type=ClientRequest,scope=Write,name=Latency",
  "/org.apache.cassandra.metrics:type=Storage,name=Load"
```
