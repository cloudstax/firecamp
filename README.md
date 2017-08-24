# [FireCamp](https://github.com/cloudstax/firecamp)

**FireCamp is an open-source platform to easily setup, manage and scale the Dockerized stateful services. The customer could easily run any service with no management overhead on any cloud. The FireCamp platform is one step towards the free Serverless Cloud. The application functions could freely talk to any stateful service. It makes it easy to develop and test the functions locally with the stateful service, and run in any cloud at scale with the same stateful service.**

The FireCamp platform deeply integrates with the popular open source stateful services, such as MongoDB, PostgreSQL, MySQL, Cassandra, ZooKeeper, Kafka, Redis, ElasticSearch, etc. The platform manages the service members, such as MongoDB replicas, and their data volumes together. When the container of a service member moves to a different node, the platform will maintain the membership and move the data volume as well.

With FireCamp, you are able to quickly and efficiently respond to the stateful service demands:
* **Deploy a stateful service quickly.**
* **Handle the failure automatically.**
* **Scale the stateful service on the fly.**
* **Upgrade the stateful service smoothly.**
* **Manage the stateful service data easily.**
* **Move from one cloud to another freely.**

## Key Features

**Cross Container Orchestration Frameworks**: The platform is built on top of the popular container orchestration frameworks. Currently the platform supports [Amazon EC2 Container Service](https://aws.amazon.com/ecs/). The [Docker Swarm](https://docs.docker.com/engine/swarm/) on Amazon Web Services is coming soon. Other container orchestration frameworks ([Mesos](http://mesos.apache.org/) and [Kubernetes](https://kubernetes.io)) will be supported in the future.

**Cross Clouds**: Currently support Amazon Web Services. Will support Google Cloud, Microsoft Azure Cloud, Private and Hybrid Cloud in the future.

**Easy Deploy**: The platform's Catalog service integrates the stateful services. The customer could simply call a single command to deploy a stateful service, such as a MongoDB ReplicaSet.

**Fast and Auto Failover**: The FireCamp platform binds the data volume and membership with the container. When a node crashes, the container orchestration frameworks will reschedule the container to another node. The FireCamp platform will automatically move the data volume and the membership. There is no data copy involved during the failover.

**Security**: The platform will enforce the security at both the platform level and the service level.
* The AppAccessSecurityGroup: the platform creates the AppAccessSecurityGroup to restricts the access to the stateful services. Only the EC2 instances in the AppAccessSecurityGroup could access the stateful services. The customer should have the application running on the EC2 of the AppAccessSecurityGroup and the same VPC.
* The Bastion node: the Bastion AutoScaleGroup is created and is the only one that could SSH to the FireCamp cluster nodes and talk with the FireCamp manage service.
* Service security: user and password are required to access the service, such as MongoDB. And other service internal security features are enabled, such as MongoDB access control between members of a ReplicaSet.

**Easy Scale**: The platform makes it easy to scale out the stateful services. For the primary based service, such as MongoDB, the platform integrates with Cloud Volume Snapshot to simplify adding a new replica. For example, to add a new replica to an existing MongoDB ReplicaSet, the platform will pause one current replica, create a Snapshot of the replica's Cloud Volume, create a new Volume from the Snapshot, and assign the new volume to the new replica.

**Smooth Upgrade**: The platform is aware of the service's membership, and follows the service's rule to maintain the membership. For example, when upgrade a MongoDB ReplicaSet, it is better to gracefully stop and upgrade the primary member first. The platform will automatically detect who is the primary member, upgrade it first, and then upgrade other members one by one.

**Easy Data Management**: The platform provides the policy based data management. The customer could easily setup the policy to manage the data Snapshot, Backup, Restore, etc.

**GEO Access and Protection**: The platform could manage the service membership across multiple Regions, and support backup data to the remote region.

**Service Aware Monitoring**: No matter where the container of one service member moves and how many times it move, the platform keeps tracking a member's logs and metrics for the member. The platform will also track the containers of different services running on the same host, and correlate them for the troubleshooting.

## How does it work?

Basically, the FireCamp platform maintains the service membership and data volume for the service. When the container moves from one node to another node, the FireCamp platform could recognize the member of the new container and mount the original data volume. For more details, please refer to [Architecture](https://github.com/cloudstax/firecamp/wiki/Architecture) and [Work Flow](https://github.com/cloudstax/firecamp/wiki/Work-Flows) wiki.

## Installation
The FireCamp cluster could be easily installed using AWS CloudFormation. For the details, please refer to [Installation](https://github.com/cloudstax/firecamp/wiki/Installation) wiki.

## Catalog Services
* [MongoDB](https://github.com/cloudstax/firecamp/tree/master/catalog/mongodb)
* [PostgreSQL](https://github.com/cloudstax/firecamp/tree/master/catalog/postgres)
* [Cassandra](https://github.com/cloudstax/firecamp/tree/master/catalog/cassandra)
* [ZooKeeper](https://github.com/cloudstax/firecamp/tree/master/catalog/zookeeper)
* [Kafka](https://github.com/cloudstax/firecamp/tree/master/catalog/kafka)
* Redis: coming soon.
* MySQL: coming soon.
* ElasticSearch: coming soon.
