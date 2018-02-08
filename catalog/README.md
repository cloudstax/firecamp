This doc discusses about the common mechanisms shared by all catalog services.

# Logging

FireCamp integrates with cloud logs. On AWS, the service logs are sent to AWS CloudWatch.

On AWS, every service will have its own log group: firecamp-clustername-servicename-serviceUUID. Every service member will get the stream name: membername/hostname/containerID. AWS ECS and Docker Swarm will have the same log group and stream. K8s will be slightly different, as K8s has namespace and init container concepts. K8s log group: firecamp-clustername-namespace-servicename-serviceUUID. K8s log stream: membername/containername/hostname/containerID. FireCamp will set the init/stop container name with the init and stop prefix in the pod.

# FireCamp System Configs

**Transparent Huge Pages**

The THP (Transparent Huge Pages) is set to never, as THP might impact the performance for some services such as MongoDB and Redis. Could set to madvise later if some service really benefits from madvise.

**File Descriptors**

The file descriptors are increased 100,000, as the stateful service such as Kafka/ElasticSearch may use a large number of files.

**Number of Threads**

The number of threads are increased to 64,000. So the stateful service could use as many threads as needed.

**Tcp backlog**

Both /proc/sys/net/core/somaxconn and /proc/sys/net/ipv4/tcp_max_syn_backlog are increased to 512.

On AWS ECS, the service container directly uses the host network stack for better network performance. So the service container could also see the somaxconn to be 512. While Docker Swarm service creation does not support host network. The docker run command supports modifying the sysctl options, but Docker Swarm does not support the sysctl options yet.

For Docker Swarm, we would have to use some [workarounds](https://residentsummer.github.io/posts/2016/02/07/docker-somaxconn/) before Docker Swarm supports the sysctl option. The workaround is: mount host /proc inside a container and configure network stack from within. Create the service with "-v /proc:/writable-proc". Then could change somaxconn inside container, "echo 512 > /writable-proc/sys/net/core/somaxconn", "echo 512 > /writable-proc/sys/net/ipv4/tcp_max_syn_backlog".

**Memory Configs**

**swapping** is bad for performance and stability. For example, if the JVM heap is swapped out to disk, the JVM garbage collections may last for minutes instead of milliseconds and can cause nodes to respond slowly or even to disconnect from the cluster. FireCamp sets the vm.swappiness to 1. This reduces the kernel’s tendency to swap and should not lead to swapping under normal circumstances, while still allowing the whole system to swap in emergency conditions. And the vm.overcommit_memory is disabled. If the system memory is not enough for the stateful service, it would be better to add more memory.

**mmap** may be used by some stateful services such as ElasticSearch. The vm.max_map_count is increased to 262144 for ElasticSearch.

