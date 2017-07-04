# [OpenManage](https://github.com/cloudstax/openmanage)

*OpenManage* is an open-source platform to easily setup, manage and scale the Dockerized stateful services. The platform deeply integrates with the popular open source stateful services, such as MongoDB, PostgreSQL, MySQL, Cassandra, etc. The platform manages the service members, such as MongoDB replicas, and their data volumes together. When the container of a service member moves to a different node, the platform will maintain the membership and move the data volume as well.

The platform is built on top of all popular container orchestration frameworks, including
[Amazon EC2 Container Service](https://aws.amazon.com/ecs/), [Docker Swarm](https://docs.docker.com/engine/swarm/), [Mesos](http://mesos.apache.org/) and [Kubernetes](https://kubernetes.io). You could use any container orchestration framework you want.

With OpenManage, you are able to quickly and efficiently respond to the stateful service demands:
* Deploy a stateful service quickly.
* Scale the stateful service on the fly.
* Upgrade the stateful service smoothly.
* Manage the stateful service data easily.
* Move from one cloud to another easily.

The OpenManage platform is one step towards the free Serverless computing. The customer could easily run any service with no management overhead on any Cloud. Currently the platform has built-in integrations for MongoDB and PostgreSQL. MySQL and Cassandra are coming soon. More services will be integrated in the future.

## Key Features

**Cross container orchestration frameworks**: The platform is built on top of the popular container orchestration frameworks. Currently the platform supports [Amazon EC2 Container Service](https://aws.amazon.com/ecs/). The [Docker Swarm](https://docs.docker.com/engine/swarm/) on Amazon Web Services is coming soon. Other container orchestration frameworks (Mesos and Kubernetes) will be supported in the future.

**Cross Clouds**: Currently support Amazon Web Services. Will support Google Cloud and Microsoft Azure Cloud in the future.

**Easy setup**: The platform's Catalog service integrates the stateful services. The customer could simply call a single command to setup a stateful service, such as a MongoDB ReplicaSet.

**Fast failover**: The OpenManage platform binds the data volume and membership with the container. When a node crashes, the container orchestration frameworks will reschedule the container to another node. The OpenManage platform will automatically move the data volume and the membership. There is no data copy involved during the failover.

**Smooth upgrade**: The platform is aware of the service's membership, and follows the service's rule to maintain the membership. For example, when upgrade a MongoDB ReplicaSet, it is better to gracefully stop and upgrade the primary member first. The platform will automatically detect who is the primary member, upgrade it first, and then upgrade other members one by one.

**Easy scale**: The platform makes it easy to scale out the stateful services. For the primary based service, such as MongoDB, the platform integrates with Cloud Volume Snapshot to simplify adding a new replica. For example, to add a new replica to an existing MongoDB ReplicaSet, the platform will pause one current replica, create a Snapshot of the replica's Cloud Volume, create a new Volume from the Snapshot, and assign the new volume to the new replica.

**Easy Data management**: The platform provides the policy based data management. The customer could easily setup the policy to manage the data Snapshot, Backup, Restore, etc.

**GEO access and protection**: The platform could manage the service membership across multiple Regions, and support backup data to the remote region.

**Security**: The platform will automatically apply the best practices to achieve the better security. For example, in the same AWS region, the platform will restrict the access to the EC2 instances where the stateful services run on. Only the EC2 instances in the allowed security group could access the stateful services. The customer should have the application running on the EC2 of that security group.

**Service Aware Monitoring**: No matter where the container of one service member moves and how many times it move, the platform keeps tracking a member's logs and metrics for the member. The platform will also track the containers of different services running on the same host, and correlate them for the troubleshooting.

## How does it work?

Basically, the OpenManage platform maintains the service membership and data volume for the service. When the container moves from one node to another node, the OpenManage platform could recognize the member of the new container and mount the original data volume. For more details, please refer to [Architecture](https://github.com/cloudstax/openmanage/wiki/Architecture) and [Work Flow](https://github.com/cloudstax/openmanage/wiki/Work-Flows) wiki.

## Installation
The OpenManage platform could be easily installed using AWS CloudFormation. The template will create an ECS cluster with 3 AutoScaleGroups on 3 AvailabilityZones. So the stateful services such as MongoDB could have 3 replicas on 3 AvailabilityZones, to tolerate the single availability zone failure.

The template will create the EcsAccessSecurityGroup to protect the access the stateful services running on the ECS cluster. You could run the applications on the EcsAccessSecurityGroup. Also a Bastion AutoScaleGroup is created and is the only one that could ssh to the ECS cluster nodes. The Bastion AutoScaleGroup could access the OpenManage manage http server as well.

The template also creates the OpenManage tables in AWS DynamoDB, and the HostedZone in AWS Route53.

When the created stack is deleted, all related resources, except the Route53 HostedZone, will be deleted. All DNS records need to be deleted before deleting the HostedZone. Deleting a DNS record requires to specify the current mapping from the DNS record name to the node's IP address. Unfortunately the CloudFormation stack is not able to know this mapping. So it could not delete the DNS record. Please manually delete the DNS records and the HostedZone after deleting the stack.

Follow the steps to create a CloudFormation stack:
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

