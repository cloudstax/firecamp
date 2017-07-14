The OpenManage PostgreSQL container is based on Debian. The data volume will be mounted to the /data directory inside container. The PostgreSQL data will be stored at the /data/db directory, and the config files are at the /data/conf directory.

**Security**

User and password are required to access the DB. The replication user and password is also enabled for the standby to replicate from the primary.

**Replication**

PostgreSQL has a few [replication mechanisms](https://www.postgresql.org/docs/current/static/high-availability.html). The OpenManage PostgreSQL uses the streaming replication. The replication slot is not suitable. If the standby goes down for a long time, the primary xlog will become full. The DB will go down. In Cloud one availability zone could go down. Typically the primary and standby would be deployed to 2 different availability zones. If the zone of the standby DB goes down for a while, the primary will be down because the xlog becomes full.

Currently WAL segments are not archived. The replication simply keeps upto 32 WAL segments. If replication could lag behind and the primary recycle the old segments before the standby has received them, the standby will need to be reinitialized from a new base backup. Archiving the segments to S3 is planned in the future to avoid it.

**Initialization**

When the primary container starts at the first time, the primary will initialize db and create the replication user. Once the primary is initialized, the primary will start serving the trasactions and streaming the logs to the standby.

When the standby container starts at the first time, the standby will use pg_basebackup to copy the base backup from the primary. Once the standby is initialized, the standby will keep streaming the logs from the primary.
