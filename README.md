# [OpenManage](https://github.com/openconnectio/openmanage)

*OpenManage* is an open source platform to easily setup, manage and scale the Dockerized stateful services. The platform deeply integrates with the popular open source stateful services, such as MongoDB, Cassandra, MySQl, PostgreSQL, etc. The platform manages the service members, such as MongoDB replicas, and their data volumes together. When the container of a service member moves to a different host, the platform will move the data volume as well.

Currently the platform has built-in integrations for MongoDB and PostgreSQL. MySQL and Cassandra are coming soon. More services will be integrated in the future.

## Key Features

Cross container orchestraction frameworks. The platform is built on top of the popular container orchestraction frameworks. Currently the platform supports [Amazon EC2 Container Service](https://aws.amazon.com/ecs/). The [Docker Swarm](https://docs.docker.com/engine/swarm/) on Amazon Web Services is coming soon. Other container orchestraction frameworks (Mesos and Kubernetes) will be supported in the future.

Cross Clouds. Currently support Amazon Web Services. Will support Google Cloud and Microsoft Azure Cloud in the future.

Easy setup. the customer could simply setup a stateful service by a single command.

Fast failover.

Smooth upgrade. The platform is aware of the service's membership, and follows the service's rule to maintain the membership. For example, when upgrade a MongoDB ReplicaSet, it is better to gracefully stop and upgrade the primary member first. The platform will automatically detect who is the primary member and upgrade it first.

Easy scale.

Data management: Snapshot, Backup, Restore.

Geo distribution.

Security.

## How does it work?

## Installation

