* [FireCamp Redis Internals](https://github.com/cloudstax/firecamp/tree/master/catalog/redis#firecamp-redis-internals)
* [Tutorials](https://github.com/cloudstax/firecamp/tree/master/catalog/redis#tutorials)

# FireCamp Redis Internals

The FireCamp Redis container is based on the [official Redis image](https://hub.docker.com/_/redis/). The data volume will be mounted to the /data directory inside container. The redis data will be stored under /data/redis.

## Redis Mode

### Single Instance Mode
A single node Redis service. This may be useful for developing and testing. Simply create a Redis service with 1 shard and 1 replica.

### Master-Slave Mode
One master with multiple read-only slaves. For example, to create a 1 master and 2 slaves Redis service, specify 1 shard with 3 replicas per shard. If the cluster has multiple zones, the master and slaves will be distributed to different availability zones.

Currently the slaves are read-only. If the master goes down, Redis will become read-only. The [Redis Sentinel](https://redis.io/topics/sentinel) will be supported in the coming release to enable the automatic failover, e.g. automatically promote a slave to master when the current master is down.

### Cluster Mode
The minimal Redis cluster should have [at least 3 shards](https://redis.io/topics/cluster-tutorial#creating-and-using-a-redis-cluster). Each shard could be single instance mode or master-slave mode. Redis cluster could handle the node failure when "[there are at least the majority of masters and a slave for every unreachable master](https://redis.io/topics/cluster-spec)". If the majority of masters are down, the slaves will not become master automatically. Need to manually run [cluster failover takeover command](https://redis.io/commands/cluster-failover) to promote the slave to become the new master. This will be further automated in the future.

By default, FireCamp distributes the masters to all availability zones. So when one availability zone goes down, Redis cluster will still have the majority of masters to promote the slave to master automatically. For example, in a 3 availability zones environment, create a 3 shards Redis cluster and each shard has 1 master and 1 slave. The 3 masters will be distributed to 3 availability zones, and the slave of one master will be distributed to a different availability zone for HA.

Currently Redis will not fail back automatically. See [Re-adding a failed over node](https://github.com/antirez/redis/issues/2462). For example, a Redis cluster has 3 shards on 3 availability zones, and each shard has 1 master and 1 slave. When master on zone1 goes down for a while, say the slave on zone2 becomes the new master. When the down node joins back, it will serve as slave. Then zone2 owns 2 masters. At this time, if zone2 goes down, 2 masters (majority) go down at the same time. Redis cluster will be down. This will be further enhanced in the future with the manual cluster takeover.

#### Concurrent Failure
Redis cluster is possible to reassign the slave internally, because of new failovers, new manual reshardings, and things like that. For example, one Redis cluster has 3 shards and each shard has 1 master and 2 slaves. If 2 slaves of shard1 go down at the same time, the shard1's master becomes the orphaned master, and Redis will migrate the slave of another shard to cover this orphaned master. This aims to provide more failure resistance in the future failure events. When the failed slaves come back, they will usually join back as the slaves of shard1. While, they may get reassigned to be the slave of another maser. See Redis issue [2462](https://github.com/antirez/redis/issues/2462).

Currently Redis is not aware of the availability zones. After a few rounds of concurrent slave failures, the master and slaves of one shard may be at the same availability zone. This will be further enhanced in the future releases.

This will not happen if not all slaves of one shard fail at the same time. The concurrent failure window is small. When one node goes down, FireCamp will start a new node at the same availability zone and join back the shard. The availability zone failure is also ok, as FireCamp initially distributes the members of one shard to all availability zones.

Note: [Redis Pack is rack-zone aware](https://redislabs.com/redis-enterprise-documentation/concepts-architecture/high-availability/rack-zone-awareness/). We will explore the opportunity to integrate with Redis Pack.

#### Additional Topology
The network inside one availability zone is faster than cross availability zones. Running all masters on a single availability zone would be useful for some cases. For example, Redis is used for cache. The application also runs in a single availability zone. Only when the availability zone fails, the application and Redis will run in another availability zone. We will support it in the future. When the master availability zone goes down, FireCamp will automatically run `cluster failover takeover` to promote the slave to master.


## Redis Static IP address
Both Redis Sentinel and Cluster require to use a static IP address to represent a Redis member, the hostname is not allowed. See Redis issues [2706](https://github.com/antirez/redis/issues/2410), [2410](https://github.com/antirez/redis/issues/2410), [2565](https://github.com/antirez/redis/issues/2565), [2323](https://github.com/antirez/redis/pull/2323).

If the containers fail over to other nodes, the containers' IPs will change. Redis is not able to handle it and will mark the cluster fail. For example, a 3 shards Redis cluster, each shard has 1 master and 2 slaves. If we stop all 9 containers around the same time, container framework will rescheudle all containers. Some containers may run on a different node and have a different ip, which could be used by another container before restart. Redis will be confused about the master-slave relationship and mark the cluster fail.

Basically this requires the ability to bind the static IP with one container. The FireCamp platform follows a simpler way that leverages the multiple IPs ability of one instance. The platform assigns one private IP for every Redis memeber. When container moves to a new node, FireCamp unassigns the secondary ip from the old node and assigns it to the new node. So all members could talk with each other via these assigned static IPs. Every member still gets a unique DNS name, which points to the member's static IP. The application could simply access Redis cluster using the member's DNS name, as long as the application (independent process or function) is running in the same VPC. Please refer to [Architecture wiki](https://github.com/cloudstax/firecamp/wiki/Architecture#service-member-static-ip) for more details.

## Data Persistence

Redis periodically saves snapshots of the dataset to the disk. The default save configs are used, e.g. DB will be saved:
- after 900 sec (15 min) if at least 1 key changed
- after 300 sec (5 min) if at least 10 keys changed
- after 60 sec if at least 10000 keys changed

If AOF (append only file) is enabled, the key change will always be written to the AOF file and Redis fsync the AOF file every second. By default, the AOF (append only file) persistence is enabled to minimize the chance of data loss in case Redis stops working.

If you are using Redis as cache, you could disable AOF when creating the Redis service. Each Redis member will still have one volume to store data. In case the node fails, the volume will be attached to a new node. Redis could quickly rebuild the cache by reading from the volume.

## Data Backup

If AOF is disabled, you could connect to the master or slave of all shards, run “BGSAVE”, wait till the bgsaves are done and then backup data by taking the snapshot for Redis EBS volumes manually. If AOF is enabled, you could directly take the snapshot for Redis EBS volumes.

The policy based auto backup will be supported in the future.

## Security

Redis AUTH is supported. To enable AUTH, please set "redis-auth-pass" with the password when creating the service. If AUTH is enabled, the clients MUST issue AUTH <PASSWORD> before processing any other commands. If you don't want to enable AUTH, simply don't set "redis-auth-pass" when creating the service.
Redis cluster setup tool, redis-trib.rb, does not support auth pass yet. See Redis issue [2866](https://github.com/antirez/redis/issues/2866) and [4288](https://github.com/antirez/redis/pull/4288). FireCamp firstly sets up the cluster with auth disabled. Once the cluster is initialized, the redis.conf is updated to enable auth for all members.

The possibly harmful commands are disabled or renamed. The commands, FLUSHALL (remove all keys from all databases), FLUSHDB (similar, but from the current database), and SHUTDOWN, are disabled. The CONFIG (reconfiguring server at runtime) command could be renamed when creating the service. It might be useful at some conditions. For example, if you hit latency issue, could enable latency monitor, "CONFIG SET latency-monitor-threshold <milliseconds>", to collect data. Setting the new name to the empty string will disable the CONFIG command.

## Configs

[**Memory size**](http://docs.aws.amazon.com/AmazonElastiCache/latest/UserGuide/CacheNodes.SelectSize.html#CacheNodes.SelectSize.Redis): if you estimate that the total size of all your items to be 12 GB in a 3 shards Redis cluster, each shard will serve 4GB data. The Redis replication buffer is set as 512MB, plus 1GB reserved for OS. The Redis node should have at least 5.5GB memory. When Redis persists the memory data to disk, it may take upto 4GB memory to serve the coming writes during the data persistence. If your application is write heavy, you should double the per node Redis memory to at least 8GB, so the node memory is at least 9.5GB.

The max memory size should always be set when creating the Redis service. If not set, Redis will allocate memory as long as OS allows. This may cause memory got swapped and slow down Redis unexpectedly. Please specify Redis memory size at the service creation, -redis-memory-size.

**Storage size**: If AOF is disabled, the storage size could be twice of the memory size. With AOF enabled, much more storage is required. For the [standard usage scenarios](https://redislabs.com/redis-enterprise-documentation/installing-and-upgrading/hardware-software-requirements), Redis enterprise version (Redis Pack) recommends the storage size to be 6x of node's RAM size. And even more storage is required for the heavy write scenarios. See [the detail disk sizing for heavy write scenarios](https://redislabs.com/redis-enterprise-documentation/cluster-administration/best-practices/disk-sizing-heavy-write-scenarios/).

[**Client Output Buffer for the slave clients**](https://redislabs.com/blog/top-redis-headaches-for-devops-replication-buffer): set both hard and soft limits to 512MB.

**Tcp backlog**: both /proc/sys/net/core/somaxconn and /proc/sys/net/ipv4/tcp_max_syn_backlog are increased to 512.

The CloudFormation template increases the EC2 node's somaxconn to 512. On AWS ECS, the service container directly uses the host network stack for better network performance. So the service container could also see the somaxconn to be 512. While Docker Swarm service creation does not support host network. The docker run command supports modifying the sysctl options, but Docker Swarm does not support the sysctl options yet.

For Docker Swarm, we would have to use some [workarounds](https://residentsummer.github.io/posts/2016/02/07/docker-somaxconn/) before Docker Swarm supports the sysctl option. The workaround is: mount host /proc inside a container and configure network stack from within. Create the service with "-v /proc:/writable-proc". Then could change somaxconn inside container, "echo 512 > /writable-proc/sys/net/core/somaxconn", "echo 512 > /writable-proc/sys/net/ipv4/tcp_max_syn_backlog".

**Overcommit memory**: set vm.overcommit_memory to 1, to avoid failure of the background saving. Redis will not actuallly use out of system memory. See [Redis's detail explanations](https://redis.io/topics/faq).

[**Replication Timeout**](https://redislabs.com/blog/top-redis-headaches-for-devops-replication-timeouts/): the default value is 60s. The timeout could happen at some conditions, such as slow storage, slow network, big dataset, etc. If the timeout is caused by big dataset, you should consider to add more shards to the cluster. Another option is to increase the timeout value.

## Logging

The Redis logs are sent to the Cloud Logs, such as AWS CloudWatch logs.


Refs:

[1]. [Redis Enterprise Doc](https://redislabs.com/resources/documentation/redis-pack-documentation/)

[2]. [AWS ElastiCache Best Practices](http://docs.aws.amazon.com/AmazonElastiCache/latest/UserGuide/BestPractices.html)

[3]. [Redis Security](https://redis.io/topics/security)


# Tutorials

This is a simple tutorial about how to create a Redis service and how to use it. This tutorial assumes the cluster name is "t1", the AWS Region is "us-east-1", and the Redis service name is "myredis".

## Create a Redis service
Follow the [Installation Guide](https://github.com/cloudstax/firecamp/tree/master/docs/installation) guide to create a 3 nodes cluster across 3 availability zones. Create a Redis cluster:
```
firecamp-service-cli -op=create-service -service-type=redis -region=us-east-1 -cluster=t1 -redis-shards=3 -redis-replicas-pershard=2 -volume-size=20 -service-name=myredis -redis-memory-size=4096 -redis-auth-pass=changeme
```

This creates a 3 shards Redis on 3 availability zones. Each shard has 1 master and 1 slave. The master and slave will be assigned to different availability zone, to tolerate one availability zone failure. Every container has 20GB volume. The DNS names of the members would be:
* shard 0, master myredis-0.t1-firecamp.com, slave myredis-1.t1-firecamp.com
* shard 1, master myredis-2.t1-firecamp.com, slave myredis-3.t1-firecamp.com
* shard 2, master myredis-4.t1-firecamp.com, slave myredis-5.t1-firecamp.com.

## Set and Get Keys
1. Set Keys
```
for i in `seq 1 100`
do
  redis-cli -a changeme -h myredis-0.t1-firecamp.com -c set key$i value$i
done
```

2. Get Keys
```
for i in `seq 1 100`
do
  redis-cli -a changeme -h myredis-0.t1-firecamp.com -c get key$i
done
```

