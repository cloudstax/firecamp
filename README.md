# [OpenManage](https://github.com/openconnectio/openmanage)

*OpenManage* is an open source platform to easily setup, manage and scale the Dockerized stateful services. The platform deeply integrates with the popular open source stateful services, such as MongoDB, Cassandra, MySQl, PostgreSQL, etc. The platform manages the service members, such as MongoDB replicas, and their data volumes together. When the container of a service member moves to a different node, the platform will maintain the membership and move the data volume as well.

The OpenManage platform is one step towards the free serverless computing. The customer could easily run any service with no management overhead. Currently the platform has built-in integrations for MongoDB and PostgreSQL. MySQL and Cassandra are coming soon. More services will be integrated in the future.

## Key Features

**Cross container orchestraction frameworks**: The platform is built on top of the popular container orchestraction frameworks. Currently the platform supports [Amazon EC2 Container Service](https://aws.amazon.com/ecs/). The [Docker Swarm](https://docs.docker.com/engine/swarm/) on Amazon Web Services is coming soon. Other container orchestraction frameworks (Mesos and Kubernetes) will be supported in the future.

**Cross Clouds**: Currently support Amazon Web Services. Will support Google Cloud and Microsoft Azure Cloud in the future.

**Easy setup**: The customer could simply setup a stateful service, such as a MongoDB ReplicaSet, by a single command.

**Fast failover**: When a node crashes, the container orchestraction frameworks will reschedule it to another node. The OpenManage platform will automatically move the data volume and the membership. There is no data copy involved during the failover.

**Smooth upgrade**: The platform is aware of the service's membership, and follows the service's rule to maintain the membership. For example, when upgrade a MongoDB ReplicaSet, it is better to gracefully stop and upgrade the primary member first. The platform will automatically detect who is the primary member, upgrade it first, and then upgrade other members one by one.

**Easy scale**: The platform makes it easy to scale out the stateful services. For the primary based service, such as MongoDB, the platform integrates with Cloud Volume Snapshot to simplify adding a new replica. For example, to add a new replica to an existing MongoDB ReplicaSet, the platform will pause one current replica, create a Snapshot of the replica's Cloud Volume, create a new Volume from the Snapshot, and assign the new volume to the new replica.

**Easy Data management**: The platform provides the policy based data management. The customer could easily setup the policy to manage the data Snapshot, Backup, Restore, etc.

**GEO access and protection**: The platform could manage the service membership across multiple Regions, and support backup data to the remote region.

**Service Aware Logging**:

**Service Aware Monitoring**:

**Security**: The platform will automatically apply the best practices to achieve the better security. For example, in the same AWS region, the platform will restrict the access to the EC2 instances where the stateful services run on. Only the EC2 instances in the allowed security group could access the stateful services. The customer should have the application running on the EC2 of that security group.

## How does it work?

The stateful services have 2 basic requirements: membership and data. When one member moves from one node to another, the member should be able to join back the cluster as the original member. Different services have different ways to achieve this. One common way is: every member keeps a well-known DNS name. When the member moves, the DNS record is updated to the new IP address.

The OpenManage platform follows the DNS way. Every member has a DNS name and a data volume. The OpenManage platform binds the DNS name and data volume with the service member.

The OpenManage platform has 4 basic components:
- **A key-value database**. The database keeps the service's attributes, including the service uuid, the number of members, every member's data volume and config files, etc. There are 2 requirements for the database: 1) conditional creation/update (create-if-not-exist and update-if-match). 2) strong consistency on get/list. The customer is free to use the databases they are familiar with, as long as the database supports these 2 requirements. The platform has built-in support for a simple controldb and AWS DynamoDB. More databases will be supported in the future.
- **The DNS server**. Every service member will have a unique DNS name. No matter where the container runs at, the platform will keep the DNS server updated. So the members could always talk with each other.
- **The volume driver**. The platform assigns a uuid for every service. When creating the service in the underline container orchestraction framework, the service uuid is passed to the volume driver. When a container starts, the volume driver will talk with the database to find out which member the container serves, updates the DNS record, and mounts the data volume. By this, the service will recognize the new container as the original member.
- **The manage service**. The manage service handles the different requirements of different services. So the customer could easily deploy a service by a single command.

## Installation

