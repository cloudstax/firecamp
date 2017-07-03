# [OpenManage](https://github.com/cloudstax/openmanage)

*OpenManage* is an open source platform to easily setup, manage and scale the Dockerized stateful services. The platform deeply integrates with the popular open source stateful services, such as MongoDB, PostgreSQL, MySQL, Cassandra, etc. The platform manages the service members, such as MongoDB replicas, and their data volumes together. When the container of a service member moves to a different node, the platform will maintain the membership and move the data volume as well.

The OpenManage platform is one step towards the free Serverless computing. The customer could easily run any service with no management overhead on any Cloud. Currently the platform has built-in integrations for MongoDB and PostgreSQL. MySQL and Cassandra are coming soon. More services will be integrated in the future.

## Key Features

**Cross container orchestraction frameworks**: The platform is built on top of the popular container orchestraction frameworks. Currently the platform supports [Amazon EC2 Container Service](https://aws.amazon.com/ecs/). The [Docker Swarm](https://docs.docker.com/engine/swarm/) on Amazon Web Services is coming soon. Other container orchestraction frameworks (Mesos and Kubernetes) will be supported in the future.

**Cross Clouds**: Currently support Amazon Web Services. Will support Google Cloud and Microsoft Azure Cloud in the future.

**Easy setup**: The platform's Catalog service integrates the stateful services. The customer could simply call a single command to setup a stateful service, such as a MongoDB ReplicaSet.

**Fast failover**: The OpenManage platform binds the data volume and membership with the container. When a node crashes, the container orchestraction frameworks will reschedule the container to another node. The OpenManage platform will automatically move the data volume and the membership. There is no data copy involved during the failover.

**Smooth upgrade**: The platform is aware of the service's membership, and follows the service's rule to maintain the membership. For example, when upgrade a MongoDB ReplicaSet, it is better to gracefully stop and upgrade the primary member first. The platform will automatically detect who is the primary member, upgrade it first, and then upgrade other members one by one.

**Easy scale**: The platform makes it easy to scale out the stateful services. For the primary based service, such as MongoDB, the platform integrates with Cloud Volume Snapshot to simplify adding a new replica. For example, to add a new replica to an existing MongoDB ReplicaSet, the platform will pause one current replica, create a Snapshot of the replica's Cloud Volume, create a new Volume from the Snapshot, and assign the new volume to the new replica.

**Easy Data management**: The platform provides the policy based data management. The customer could easily setup the policy to manage the data Snapshot, Backup, Restore, etc.

**GEO access and protection**: The platform could manage the service membership across multiple Regions, and support backup data to the remote region.

**Security**: The platform will automatically apply the best practices to achieve the better security. For example, in the same AWS region, the platform will restrict the access to the EC2 instances where the stateful services run on. Only the EC2 instances in the allowed security group could access the stateful services. The customer should have the application running on the EC2 of that security group.

**Service Aware Monitoring**: No matter where the container of one service member moves and how many times it move, the platform keeps tracking a member's logs and metrics for the member. The platform will also track the containers of different services running on the same host, and correlate them for the troubleshooting.

## How does it work?

The stateful services have 2 basic requirements: membership and data. When one member moves from one node to another, the member should be able to join back the cluster as the original member. Different services have different ways to achieve this. One common way is: every member keeps a well-known DNS name. When the member moves, the DNS record is updated to the new IP address.

The OpenManage platform follows the DNS way. Every member has a DNS name and a data volume. The OpenManage platform binds the DNS name and data volume with the service member.

The OpenManage platform has 4 basic components:
- **A Key-Value Database**. The database keeps the service's attributes, including the service uuid, the number of members, every member's data volume and config files, etc. There are 2 requirements for the database: 1) conditional creation/update (create-if-not-exist and update-if-match). 2) strong consistency on get/list. The customer is free to use the databases they are familiar with, as long as the database supports these 2 requirements. The platform has built-in support for a simple controldb and AWS DynamoDB. More databases will be supported in the future.
- **The DNS Server**. Every service member will have a unique DNS name. No matter where the container runs at, the platform will keep the DNS server updated. So the members could always talk with each other.
- **The Volume Driver**. The platform assigns a uuid for every service. When creating the service in the underline container orchestraction framework, the service uuid is passed to the volume driver. When a container starts, the volume driver will talk with the database to find out which member the container serves, updates the DNS record, and mounts the data volume. By this, the service will recognize the new container as the original member.
- **The Catalog Service**. The platform provides a catalog of built-in services, that could be easily deployed by a single command. For example, the catalog service supports MongoDB ReplicaSet. The customer could click to deploy a 3 replicas MongoDB cluster on 3 availability zones of one AWS region.

## Installation
The OpenManage platform could be easily installed using AWS CloudFormation. It will create an ECS cluster with 3 AutoScaleGroups on 3 AvailabilityZones. So the stateful services such as MongoDB could have 3 replicas on 3 AvailabilityZones, to tolerate the single availability zonefailure.

The template will also create the EcsAccessSecurityGroup to protect the access the stateful services running on the ECS cluster. You could run the applications on the EcsAccessSecurityGroup. Also a Bastion AutoScaleGroup is created and is the only one that could ssh to the ECS cluster nodes. The Bastion AutoScaleGroup could access the OpenManage manage http server as well.

The CloudFormation template also creates the OpenManage tables in AWS DynamoDB, and the HostedZone in AWS Route53.

When the created stack is deleted, all related resources, except the Route53 HostedZone, will be deleted. All DNS records need to be deleted before deleting the HostedZone. Deleting a DNS record requires to specify the current mapping from the DNS record name to the node's IP address. Unfortunately the CloudFormation stack is not able to know this mapping. So it could not delete the DNS record. Please manually delete the DNS records and the HostedZone after deleting the stack.

The detail steps to create a CloudFormation stack:
1. Go to [AWS CloudFormation](https://console.aws.amazon.com/cloudformation/), click "Create New Stack".
2. "Select Template": specify the Amazon S3 template URL, https://s3-us-west-2.amazonaws.com/cloudstax-cf-templates/openmanage/latest/templates/openmanage-master.template, click "Next".
3. "Specify Details": specify the "Stack name", select 3 "AvailabilityZones", specify the "ECSClusterName", select the "KeyPairName", and specify the "RemoteAccessCIDR". Click "Next".
4. "Options": click "Next".
5. "Review": check the acknowledge, and click "Create".

After the stack is created, could ssh to the Bastion node or one ECS cluster node, get the [OpenManage service cli](https://s3-us-west-2.amazonaws.com/cloudstax-cf-templates/openmanage/alpha/openmanage-service-cli), and then create the MongoDB ReplicaSet or PostgreSQL cluster.

For example, the ECS cluster "test" is created at region us-east-1. To create a 3 replias MongoDB ReplicaSet with 100GB volume for every replica, run:
  openmanage-service-cli -op=create-service -service-type=mongodb -server-url=openmange-manageserver.test-openmanage.com:27040 -region=us-east-1 -cluster=test -service-name=mongodb-replicaset -replicas=3 -volume-size=100
To create a 3 nodes PostgreSQL cluster, with 100GB volume for every replica, run:
  openmanage-service-cli -op=create-service -service-type=postgresql -server-url=openmange-manageserver.test-openmanage.com:27040 -region=us-east-1 -cluster=test -service-name=pgcluster -replicas=3 -volume-size=100
