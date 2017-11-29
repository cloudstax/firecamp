* [The Stateful Service Requirements](https://github.com/cloudstax/firecamp/tree/master/docs/architect#the-stateful-service-requirements)
  * [Service Member Static IP](https://github.com/cloudstax/firecamp/tree/master/docs/architect#service-member-static-ip)
* [Architecture](https://github.com/cloudstax/firecamp/tree/master/docs/architect#architecture)
* [Components](https://github.com/cloudstax/firecamp/tree/master/docs/architect#components)
  * [Key-Value Database](https://github.com/cloudstax/firecamp/tree/master/docs/architect#the-key-value-database)
  * [Registry Service](https://github.com/cloudstax/firecamp/tree/master/docs/architect#the-registry-service)
  * [Container Plugin](https://github.com/cloudstax/firecamp/tree/master/docs/architect#the-container-plugin)
  * [Catalog Service](https://github.com/cloudstax/firecamp/tree/master/docs/architect#the-catalog-service)
  * [Common Services](https://github.com/cloudstax/firecamp/tree/master/docs/architect#the-common-services)

# The Stateful Service Requirements
**The stateful services have 2 basic requirements: membership and data.**

When one member moves from one node to another, the member needs to keep the original data by accessing the previous data volume or recovering data from other members. Recovering data from other member could take long time when data is large, and also impact other member's performance. FireCamp binds the data volume with the member. When the member moves to another node, FireCamp will automatically move the data volume. There is no data copy involved during the member failover.

Also when the member moves, the member should be able to join back the cluster as the original member. There are different ways to maintain the membership. One common option is DNS that every member keeps a well-known DNS name. When the member moves, the DNS record is updated to the new IP address. For the services that work well with the member DNS name, FireCamp follows the DNS way. Every member is assigned a unique DNS name. FireCamp binds the DNS name with the service member. When the member moves to another node, FireCamp will automatically update the DNS address.

While, some service, such as Redis, does not support the DNS name, instead, Redis requires the static IP for each member.

## Service Member Static IP
Basically this requires the ability to bind the static IP with one member.

One option is to use the virtual network that the container orchestration framework provides, or the third-party network plugin for the framework. But to access the services outside the container cluster, such as accessing the MongoDB from the Lambda function, would have to access the services through the nodes' public IPs, Load Balancer, or using the Proxy Verb. [Kubernetes access cluster services](https://kubernetes.io/docs/tasks/administer-cluster/access-cluster-services/) and [Kubernetes Publishing services](https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services---service-types) discuss about it. Using the Proxy Verb has some limitations. For example, Kubernetes Proxy Verb only works for HTTP/HTTPS. Accessing through the public IPs directly is not easy. Every node has its own public IP. When the member moves from one node to another, the public IP to access the member will also change. This requires the customers to detect the public IP change of the member. Load Balancer could help to detect the public IP change automatically. The Cloud Load Balancer introduces the additional cost and the additional network hop.

FireCamp follows a simpler way that leverages the multiple IPs ability of one instance. FireCamp assigns one private IP for every Redis memeber. When the member moves to a new node, FireCamp unassigns the secondary ip from the old node and assigns it to the new node. So all members could talk with each other via these assigned static IPs. This also provides the flexibility to access the service from anywhere, as long as the application (independent process or function) is running in the same VPC.

This is also a common solution for public/private clouds. AWS/Azure/GCP all supports multiple IPs for one vm. Linux supports to assign multiple IPs to one network interface. For example, one EC2 instance could have [multiple private IP addresses](http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/MultipleIP.html). After assigning a new IP, need to add it to an active network interface, so other nodes could access it. Could use the ip addr command like "sudo ip addr add 172.31.8.118/20 dev eth0".

This solution is limited by the maximum private ips for one instance. See [IP Addresses Per Network Interface Per Instance Type](http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-eni.html#AvailableIpPerENI). If one EC2 instance reaches the maximum private IPs, it could not serve more service. While, this is not a big issue. The m1.large instance supports up to 10 IPv4 addresses per network interface. And it is not a common case to run many stateful services on a single node in production.


# Architecture
The FireCamp platform is built on top of Clouds and Container Orchestration frameworks. This enables the customers to choose Cloud and Container Orchestration framework freely.

The following picture illustrates FireCamp's architecture.

![Architecture](https://s3.amazonaws.com/cloudstax/firecamp/docs/arch.png)

# Components
FireCamp has 5 major components. The Catalog service and Container Plugin are 2 key components that work with the orchestration framework and coordinates other components. Please refer to [Work Flows](https://github.com/cloudstax/firecamp/tree/master/docs/workflows) for how the components work together.

## The Key-Value Database
The database is the central place to store all stateful services related metadata. The database keeps the service's attributes, including the service uuid, the service members, member names, data volumes, the service config files, etc.

There are 2 requirements for the database: 1) conditional creation/update (create-if-not-exist and update-if-match). 2) strong consistency on get/list. The customer is free to use the databases they are familiar with, as long as the database supports these 2 requirements. FireCamp has built-in support for AWS DynamoDB and a simple controldb. More databases will be supported in the future.

## The Registry Service
Currently the major component of the Registry Service is the DNS service. Every service member will have a unique DNS name. No matter where the container moves, FireCamp will keep the DNS server updated. So the members could always talk with each other, and clients could reach the members through the DNS names.

If the service requires static IP, every member will also get a static IP. The member's DNS name will always point to this static IP. When the member moves to one node, the FireCamp plugin will move the static IP to the node. The clients could either talk to the member through the member's DNS name or static IP directly.

## The Container Plugin
The FireCamp Container plugin is the central coordinator between the container orchestration framework and other FireCamp components.

When the container orchestration framework schedules a service container to a worker node, the plugin will talk with the database to find out which member the container serves, updates the registry service (update the member's DNS record or move the member's static IP), and mounts the member's data volume. By this, the service will recognize the new container as the original member. Please refer to [Service Scheduling Flow](https://github.com/cloudstax/firecamp/tree/master/docs/workflows#service-scheduling-flow) for more details.

## The Catalog Service
The Catalog service manages the lifecycle of the stateful services, including deployment, upgrade, scaling, data management, etc. The Catalog service understands the differences of each service, and drives the container orchestration framework to work accordingly.

For example, to create a MongoDB ReplicaSet, users simply calls one FireCamp cli command. The Catalog service will deploy a 3 replicas MongoDB ReplicaSet on 3 availability zones of one AWS region. The Catalog service will store the service information in the Key-Value Database and manage the corresponding services/tasks on the underline container orchestration framework. For more details, please refer to [Service Creation Flow](https://github.com/cloudstax/firecamp/tree/master/docs/workflows#service-creation-flow).

When users ask to upgrade the stateful service, the Catalog service will perform the service aware upgrade. For example, when upgrade a MongoDB ReplicaSet, the Catalog service will automatically detect who is the primary member, gracefully stop the mongod daemon, upgrade the primary member's container first, and then upgrade other members one by one.

## The Common Services
FireCamp includes a set of common services to better serve the stateful services. The common services include the service aware logging, monitoring, security, etc. As FireCamp binds the container with one member, FireCamp could naturally provide the service aware monitoring. No matter where the container of one service member moves and how many times it move, FireCamp keeps tracking a member's logs and metrics for the member. FireCamp also enforces the security. For example, restrict the access to the nodes where the stateful services run on. Only the nodes in the allowed security group could access the stateful services.

