* [FireCamp PostgreSQL Internals](https://github.com/cloudstax/firecamp/tree/master/catalog/postgres#firecamp-postgresql-internals)
* [Tutorials](https://github.com/cloudstax/firecamp/tree/master/catalog/postgres#tutorials)

# FireCamp PostgreSQL Internals

The FireCamp PostgreSQL container is based on the [official PostgreSQL Debian image](https://hub.docker.com/_/postgres/). Two volumes will be created, one for PostgreSQL WAL log, the other for all other data. The WAL log volume will be mounted to the /journal directory inside container. The data volume will be mounted to the /data directory inside container. The PostgreSQL data will be stored at the /data/db directory, and the config files are at the /data/conf directory.

## PostgreSQL Cluster

**Replication**

PostgreSQL has a few [replication mechanisms](https://www.postgresql.org/docs/current/static/high-availability.html). The FireCamp PostgreSQL uses the streaming replication.

With the default streaming replication, the primary server might recycle old WAL segments before the standby has received them. Currently the replication simply keeps upto 32 WAL segments. The replication could lag behind and the primary recycle the old segments before the standby has received them. If this occurs, the standby will need to be reinitialized from a new base backup. Archiving the segments to S3 is planned in the future to avoid it.

The replication slots provide an automated way to ensure that the primary does not remove WAL segments until they have been received by all standbys. While currently the replication slots could not bound the space requirement for pg_xlog. If the standby goes down for a long time, the primary's xlog may fill up the disk. Once the disk is full, the primary will crash. In Cloud one availability zone could go down. Typically the primary and standby would be deployed to 2 different availability zones. If the zone of the standby DB goes down for a while, the primary will be down when the xlog fill up the disk.

Currently the streaming replication is asynchronous. The synchronous replication will be enabled in the future.

For more than one standbys, the Cascading Replication will be considered in the future to reduce the number of direct connection to the primary.

**Initialization**

When the primary container starts at the first time, the primary will initialize db and create the replication user. Once the primary is initialized, the primary will start serving the trasactions and streaming the logs to the standby.

When the standby container starts at the first time, the standby will use pg_basebackup to copy the base backup from the primary. Once the standby is initialized, the standby will keep streaming the logs from the primary.

**Custom Plugins**

The PostGIS is supported as the example for how to customize your PostgreSQL, catalog/postgres/9.6/postgis-dockerfile. To create a PostgreSQL cluster with PostGIS, specify -pg-image=cloudstax/firecamp-postgres-postgis:9.6 at the service creation.

If the additional custom plugin is required, we could follow the same way to create a new docker image and specify it when creating the service. Note, this is currently disabled until we hear the actual requirement.

## Security

User and password are required to access the DB. The replication user and password is also enabled for the standby to replicate from the primary.

## Logging

The PostgreSQL logs are sent to the Cloud Logs, such as AWS CloudWatch logs. Could easily check what happens to PostgreSQL through CloudWatch logs.

The current latest Amazon Linux AMI uses Docker 17.03, which does not support the custom logging driver. So the awslogs driver is directly used.

Every service will have its own log group. For example, create the PostgreSQL service, mypg, on cluster t1. The log group t1-mypg-uuid will be created for the service. A log stream is created for every container. The FireCamp PostgreSQL container will log its service member at startup. We could get the full logs of one service member by simply checking the server member log entry in the log streams.

The custom logging driver is supported from Docker 17.05. Once Amazone Linux AMI updates to use higher Docker version and supports the custom loggint driver, we will switch to the FireCamp CloudWatch log driver. The FireCamp CloudWatch log driver will send the logs of one service member to one log stream, no matter where the container runs on.


# Tutorials

This is a simple tutorial about how to create a PostgreSQL service and how to use it. This tutorial assumes the cluster name is "t1", the AWS Region is "us-east-1", and the Kafka service name is "mypg".

## Create a PostgreSQL service
Follow the [Installation](https://github.com/cloudstax/firecamp/tree/master/docs/installation) guide to create a 3 nodes cluster across 3 availability zones. Create a PostgreSQL cluster:
```
firecamp-service-cli -op=create-service -service-type=postgresql -region=us-east-1 -cluster=t1 -service-name=mypg -replicas=3 -volume-size=100 -journal-volume-size=10 -password=changeme -pg-image=cloudstax/firecamp-postgres:9.6 -pg-repluser=repluser -pg-replpasswd=replpassword
```

This creates a 3 replicas PostgreSQL on 3 availability zones. Each replica has 2 volumes, 10GB volume for journal and 100GB volume for data. The PostgreSQL admin is "postgres", password is "changeme". The PostgreSQL replication user is "repluser", password is "replpassword". The DNS names of the replicas would be: mypg-0.t1-firecamp.com, mypg-1.t1-firecamp.com, mypg-2.t1-firecamp.com. By default the first replica, mypg-0 will be the primary.

PostgreSQL service creation is pretty fast. Wait till all containers are running may take some time. In case the service creation fails, please simply retry it.

## Create the user and user db
1. Connect to the Primary from the application node: `psql -h mypg-0.t1-firecamp.com -U postgres`
2. Create a user: `CREATE USER tester WITH PASSWORD 'password';`
3. Create a db: `CREATE DATABASE testdb;`
4. Grant DB permission to user: `GRANT ALL PRIVILEGES ON DATABASE testdb to tester;`

## Connect to the user db
1. Connect to the primary from the application node: `psql -h mypg-0.t1-firecamp.com -d testdb -U tester`
2. Create table: `CREATE TABLE guestbook (visitor_email text, vistor_id serial, date timestamp, message text);`
3. Insert record: `INSERT INTO guestbook (visitor_email, date, message) VALUES ( 'jim@gmail.com', current_date, 'This is a test.');`
4. View the record: `SELECT * FROM guestbook;`

