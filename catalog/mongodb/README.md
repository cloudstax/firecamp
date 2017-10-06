The FireCamp MongoDB container is based on the [official MongoDB image](https://hub.docker.com/_/mongo/). The data volume will be mounted to the /data directory inside container. The MongoDB data will be stored at the /data/db directory, and the config files are at the /data/conf directory.

**Security**

The FireCamp MongoDB follows the offical [security checklist](https://docs.mongodb.com/manual/administration/security-checklist). The [Authentication](https://docs.mongodb.com/manual/tutorial/enable-authentication/) is enabled by default. The Admin user is created and the [Keyfile](https://docs.mongodb.com/manual/tutorial/enforce-keyfile-access-control-in-existing-replica-set/) is created for the access control between members of a Replica Set.

**Logging**

The MongoDB logs are sent to the Cloud Logs, such as AWS CloudWatch logs. Could easily check what happens to MongoDB through CloudWatch logs.

The current latest Amazon Linux AMI uses Docker 17.03, which does not support the custom logging driver. So the awslogs driver is directly used.

Every service will have its own log group. For example, create the MongoDB service, mymongo, on cluster t1. The log group t1-mymongo-uuid will be created for the service. A log stream is created for every container. The FireCamp MongoDB container will log its service member at startup. We could get the full logs of one service member by simply checking the server member log entry in the log streams.

The custom logging driver is supported from Docker 17.05. Once Amazone Linux AMI updates to use higher Docker version and supports the custom loggint driver, we will switch to the FireCamp CloudWatch log driver. The FireCamp CloudWatch log driver will send the logs of one service member to one log stream, no matter where the container runs on.

**Cache**

By default, the MongoDB instance inside the container assumes the whole node's memory could be used, and calculate the cache size accordingly. In case you want to limit the memory size, could set the max-memory when creating the service.

