This doc discusses about the common mechanisms shared by all catalog services.

# Logging

FireCamp integrates with cloud logs. On AWS, the service logs are sent to AWS CloudWatch.

AWS ECS and Docker Swarm have the same log group and stream names.
- Every stateful service will have its own log group: firecamp-clustername-servicename-serviceUUID. Every stateful service member will get the stream name: membername/hostname/containerID.
- The stateful service task, such as the init task, will use the same log group with the stateful service. The log stream has the task type as prefix.
- The stateless service, such as the FireCamp management service, Kafka Manager service, etc, will have the same log group name with the stateful service. The log stream has the service name as prefix.

K8s will be slightly different. K8s does not support custom log driver like AWS ECS and Docker Swarm supports. K8s simply writes the log to the local files. The Fluentd DaemonSet is used to send the logs to CloudWatch. The log group and stream are different, as K8s has namespace and init container concepts.

K8s log group: firecamp-clustername-namespace-servicename-serviceUUID. K8s log stream: membername/containername/hostname/containerID. FireCamp will set the init/stop container name with the init and stop prefix in the pod. Please refer to [FireCamp K8s Fluentd ConfigMap](https://github.com/cloudstax/firecamp/tree/master/containersvc/k8s/fluentd-cw-configmap.yaml) for the format.

# FireCamp System Configs

**Transparent Huge Pages**

The THP (Transparent Huge Pages) is set to madvise. THP might impact the performance for some services such as MongoDB and Redis. While, some service such as Cassandra could benefit from THP.

**File Descriptors**

The file descriptors are increased 100,000, as the stateful service such as Kafka/ElasticSearch may use a large number of files.

**Number of Threads**

The number of threads are increased to 64,000. So the stateful service could use as many threads as needed.

**Tcp backlog**

Both /proc/sys/net/core/somaxconn and /proc/sys/net/ipv4/tcp_max_syn_backlog are increased to 512.

On AWS ECS, the service container directly uses the host network stack for better network performance. So the service container could also see the somaxconn to be 512. While Docker Swarm service creation does not support host network. The docker run command supports modifying the sysctl options, but Docker Swarm does not support the sysctl options yet.

For Docker Swarm, we would have to use some [workarounds](https://residentsummer.github.io/posts/2016/02/07/docker-somaxconn/) before Docker Swarm supports the sysctl option. The workaround is: mount host /proc inside a container and configure network stack from within. Create the service with "-v /proc:/writable-proc". Then could change somaxconn inside container, "echo 512 > /writable-proc/sys/net/core/somaxconn", "echo 512 > /writable-proc/sys/net/ipv4/tcp_max_syn_backlog".

**Tcp tuning**

The idle connection timeout is adjusted. The socket memory is increased to handle thousands of concurrent connections.
'''
net.ipv4.tcp_keepalive_time=60
net.ipv4.tcp_keepalive_probes=3
net.ipv4.tcp_keepalive_intvl=10
net.core.rmem_max=16777216
net.core.wmem_max=16777216
net.core.rmem_default=16777216
net.core.wmem_default=16777216
net.core.optmem_max=40960
net.ipv4.tcp_rmem=4096 87380 16777216
net.ipv4.tcp_wmem=4096 65536 16777216
'''

**Memory Configs**

**swapping** is bad for performance and stability. For example, if the JVM heap is swapped out to disk, the JVM garbage collections may last for minutes instead of milliseconds and can cause nodes to respond slowly or even to disconnect from the cluster. FireCamp sets the vm.swappiness to 1. This reduces the kernel’s tendency to swap and should not lead to swapping under normal circumstances, while still allowing the whole system to swap in emergency conditions. And the vm.overcommit_memory is disabled. If the system memory is not enough for the stateful service, it would be better to add more memory.

**mmap** may be used by some stateful services such as ElasticSearch, Cassandra. The vm.max_map_count is increased to 1048575.

**memlock** is set to unlimited. So the service such as Cassandra could lock the memory in RAM and never get moved to the swap disk.
