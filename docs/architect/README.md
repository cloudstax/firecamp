* [The Stateful Service Requirements](https://github.com/jazzl0ver/firecamp/tree/master/docs/architect#the-stateful-service-requirements)
* [Design Considerations](https://github.com/jazzl0ver/firecamp/tree/master/docs/architect#design-considerations)
  * [Storage](https://github.com/jazzl0ver/firecamp/tree/master/docs/architect#storage)
  * [Network](https://github.com/jazzl0ver/firecamp/tree/master/docs/architect#network)
    * [Static IP](https://github.com/jazzl0ver/firecamp/tree/master/docs/architect#static-ip)
* [Architecture](https://github.com/jazzl0ver/firecamp/tree/master/docs/architect#architecture)
* [Components](https://github.com/jazzl0ver/firecamp/tree/master/docs/architect#components)
  * [Key-Value Database](https://github.com/jazzl0ver/firecamp/tree/master/docs/architect#the-key-value-database)
  * [Registry Service](https://github.com/jazzl0ver/firecamp/tree/master/docs/architect#the-registry-service)
  * [Container Plugin](https://github.com/jazzl0ver/firecamp/tree/master/docs/architect#the-container-plugin)
  * [Catalog Service](https://github.com/jazzl0ver/firecamp/tree/master/docs/architect#the-catalog-service)
  * [Common Services](https://github.com/jazzl0ver/firecamp/tree/master/docs/architect#the-common-services)

# The Stateful Service Requirements
**The stateful services have 2 basic requirements: membership and data.** Every service member, such as DB replica, needs to have a unique and stable network identity. So the members could talk with each other to form a cluster. And every service member will have a volume to persist data.

When one member moves from one node to another, the member needs to keep the original data by accessing the previous data volume or recovering data from other members. Recovering data from other member could take long time when data is large, and also impact other member's performance.

Also when the member moves, the member should be able to join back the cluster as the original member. There are different ways to maintain the membership. One common option is DNS that every member keeps a well-known DNS name. When the member moves, the DNS record is updated to the new IP address.

While, some services, such as Redis and Consul, do not work with the DNS name, instead, they require the static IP for each member. To run this kind of services, a static IP needs to be binded to the container of one service member.

# Design Considerations
The stability is always the most important thing for a stateful service. The top 3 goals of FireCamp are stability, ease of use and performance. FireCamp follows below design principles.

1. **Self-Manage.**
The stateful services should be easy to set up, manage and scale.

2. **Public cloud first.**
More and more workloads are running on public cloud directly. The public cloud provides stable network and storage for the applications. It would be great to leverage the public cloud ability. For example, AWS Route53 supports multiple regions. It would be good to leverage Route53 to run the stateful service across regions.

3. **Fit for private cloud.**
The solution could be easily expanded to the private cloud and hybrid cloud. On the private cloud, it will be valuable to support the EBS like storage such as Ceph, GlusterFS, etc, and commodity hardware.

4. **Cross container orchestration frameworks.**
Kubernetes is the most popular framework. AWS ECS is also a very valuable option on AWS. AWS ECS is simple to set up and use, and there is no master nodes for you to manage. Docker Swarm is very simple. Docker Swarm would also be a good option for some simple workloads. It will be valuable to have the stateful services work across these frameworks seamlessly.

## Storage
Public cloud provides the elastic block storage such as EBS. The volume could be detached from one node, and attached to another node. This is a great ability to leverage when the service member container moves from one node to another. FireCamp binds the data volume with the member. When the member moves to another node, FireCamp will automatically move the data volume. There is no data copy involved during the member failover.

In the private cloud, there are 2 types of storage. One is EBS like storage, such as Ceph, GlusterFS, etc. We could develop the similar volume failover mechanism with public cloud storage. The other is the local disk storage on the commodity hardware. This could save the storage usage for the stateful service, as the stateful service usually has its own replication mechanism to protect the local disk failure. A trade-off is the data recovery. The stateful service has to assign a new disk and replicate the data of the failed disk from other nodes. We will further update the design for the commodity hardware.

## Network
Network is a critical and complex component for the container orchestration framework. There are many network plugins for container, such as Calico, Weave, etc. The stability, performance and global scalability are very important for the stateful services. Public cloud provides great support for the network. FireCamp tries to leverage the public cloud network and reduce the dependency on others as much as possible. This would help the stateful services to be more stable and have better performance and scalability.

Another advantage of using the public cloud network service is the communication with other cloud applications/services. For example, AWS Lambda function wants to access the MongoDB cluster. The Lambda function would have to access the services through the nodes' public IPs, Load Balancer, or using the Proxy Verb. [Kubernetes access cluster services](https://kubernetes.io/docs/tasks/administer-cluster/access-cluster-services/) and [Kubernetes Publishing services](https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services---service-types) discuss about it. Using the Proxy Verb has some limitations. For example, Kubernetes Proxy Verb only works for HTTP/HTTPS. Accessing through the public IPs directly is not easy. Every node has its own public IP. When the member moves from one node to another, the public IP to access the member will also change. This requires the customers to detect the public IP change of the member. Load Balancer could help to detect the public IP change automatically. The Cloud Load Balancer introduces the additional cost and the additional network hop. Integrating with the public cloud network service, such as AWS Route53 DNS service, will make it easy for the cloud applications/services to talk with each other. For Kubernetes, we will integrate with the [external DNS project](https://github.com/kubernetes-incubator/external-dns) in the future.

FireCamp associates one Route53 HostedZone with one cluster. Every service member is assigned a unique DNS name in Route53 HostedZone. FireCamp binds the DNS name with the service member. When the member moves to another node, FireCamp will automatically update the DNS address in the HostedZone. AWS Route53 supports multiple regions. This makes it easier to deploy the stateful service globally. For example, to deploy a MongoDB cluster with the primary replica at region1 and a secondary replica at region2, FireCamp registers both the primary and secondary replicas to the same HostedZone.

For the private cloud, we will further evaluate to integrate with such as Calico, Consul, etc.

### Static IP
Some service, such as Redis, requires the static IP for each member. Basically this requires the ability to bind the static IP with one member.

FireCamp follows a simpler way that leverages the multiple IPs ability of one instance. FireCamp assigns one private IP for every Redis memeber. When the member moves to a new node, FireCamp unassigns the secondary ip from the old node and assigns it to the new node. So all members could talk with each other via these assigned static IPs. This also provides the flexibility to access the service from anywhere, as long as the application (independent process or function) is running in the same VPC.

This is a common solution for public clouds. AWS/Azure/GCP all supports multiple IPs for one vm. Linux supports to assign multiple IPs to one network interface. For example, one EC2 instance could have [multiple private IP addresses](http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/MultipleIP.html). After assigning a new IP, need to add it to an active network interface, so other nodes could access it. Could use the ip addr command like "sudo ip addr add 172.31.8.118/20 dev eth0".

This solution is limited by the maximum private ips for one instance. See [IP Addresses Per Network Interface Per Instance Type](http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html#AvailableIpPerENI). If one EC2 instance reaches the maximum private IPs, it could not serve more service. While, this is not a big issue. The m1.large instance supports up to 10 IPv4 addresses per network interface. And it is not a common case to run many stateful services on a single node in production.

This solution would also work for the private cloud. We will further evaluate the best solution for the private cloud.

# Architecture
The FireCamp platform is built on top of Clouds and Container Orchestration frameworks. This enables the customers to choose Cloud and Container Orchestration framework freely.

The following picture illustrates FireCamp's architecture.

![Architecture](https://s3.amazonaws.com/jazzl0ver/firecamp/docs/arch.png)

# Components
FireCamp has 5 major components. The Catalog service and Container Plugin are 2 key components that work with the orchestration framework and coordinates other components. Please refer to [Work Flows](https://github.com/jazzl0ver/firecamp/tree/master/docs/workflows) for how the components work together.

## The Key-Value Database
The database is the central place to store all stateful services related metadata. The database keeps the service's attributes, including the service uuid, the service members, member names, data volumes, the service config files, etc.

There are 2 requirements for the database: 1) conditional creation/update (create-if-not-exist and update-if-match). 2) strong consistency on get/list. The customer is free to use the databases they are familiar with, as long as the database supports these 2 requirements. FireCamp has built-in support on AWS DynamoDB for AWS ECS and Docker Swarm. For Kubernetes, FireCamp implements DB base on Kubernetes ConfigMap. This will be used for Kubernetes on the public and private clouds.

## The Registry Service
Currently the major component of the Registry Service is the DNS service. Every service member will have a unique DNS name. No matter where the container moves, FireCamp will keep the DNS server updated. So the members could always talk with each other, and clients could reach the members through the DNS names.

If the service requires static IP, every member will also get a static IP. The member's DNS name will always point to this static IP. When the member moves to one node, the FireCamp plugin will move the static IP to the node. The clients could either talk to the member through the member's DNS name or static IP directly.

## The Container Plugin
The FireCamp Container plugin is the central coordinator between the container orchestration framework and other FireCamp components.

When the container orchestration framework schedules a service container to a worker node, the plugin will talk with the database to find out which member the container serves, updates the registry service (update the member's DNS record or move the member's static IP), and mounts the member's data volume. By this, the service will recognize the new container as the original member. Please refer to [Service Scheduling Flow](https://github.com/jazzl0ver/firecamp/tree/master/docs/workflows#service-scheduling-flow) for more details.

For Kubernetes, FireCamp leverages the [StatefulSets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/), which binds the volume with one pod. The StatefulSets also binds one internal DNS name with one pod. FireCamp reuses the StatefulSets volume binding, and simply registers the external DNS name with the public cloud DNS service, such as AWS Route53.

## The Catalog Service
The Catalog service manages the lifecycle of the stateful services, including deployment, upgrade, scaling, data management, etc. The Catalog service understands the differences of each service, and drives the container orchestration framework to work accordingly.

For example, to create a MongoDB ReplicaSet, users simply calls one FireCamp cli command. The Catalog service will deploy a 3 replicas MongoDB ReplicaSet on 3 availability zones of one AWS region. The Catalog service will store the service information in the Key-Value Database and manage the corresponding services/tasks on the underline container orchestration framework. For more details, please refer to [Service Creation Flow](https://github.com/jazzl0ver/firecamp/tree/master/docs/workflows#service-creation-flow).

When users ask to upgrade the stateful service, the Catalog service will perform the service aware upgrade. For example, when upgrade a MongoDB ReplicaSet, the Catalog service will automatically detect who is the primary member, gracefully stop the mongod daemon, upgrade the primary member's container first, and then upgrade other members one by one.

## The Common Services
FireCamp includes a set of common services to better serve the stateful services. The common services include the service aware logging, monitoring, security, etc. As FireCamp binds the container with one member, FireCamp could naturally provide the service aware monitoring. No matter where the container of one service member moves and how many times it move, FireCamp keeps tracking a member's logs and metrics for the member. FireCamp also enforces the security. For example, restrict the access to the nodes where the stateful services run on. Only the nodes in the allowed security group could access the stateful services.

