The OpenManage MongoDB container is based on Debian. The data volume will be mounted to the /data directory inside container. The MongoDB data will be stored at the /data/db directory. And the config files are at the /data/conf directory.

**Security**

The OpenManage MongoDB follows the offical [security checklist](https://docs.mongodb.com/manual/administration/security-checklist). The [Authentication](https://docs.mongodb.com/manual/tutorial/enable-authentication/) is enabled by default. The Admin user is created and the [Keyfile](https://docs.mongodb.com/manual/tutorial/enforce-keyfile-access-control-in-existing-replica-set/) is created for the access control between members of a Replica Set.

**Logging**

The MongoDB container writes the logs to a file. MongoDB does not rotate the logs by default. See MongoDB Administration, [Rotate Log Files](https://docs.mongodb.com/manual/tutorial/rotate-log-files): "MongoDB only rotates logs in response to the [logRotate](https://docs.mongodb.com/manual/reference/command/logRotate/#dbcmd.logRotate) command, or when the mongod or mongos process receives a SIGUSR1 signal from the operating system". So the log file could grow infinitely. The OpenManage MongoDB uses the logrotate tool to monitor and notify MongoDB to rotate the log file. [See the reference discussion](https://stackoverflow.com/questions/5004626/mongodb-log-file-growth). And the logs will be sent to CloudWatch to avoid consuming too much local storage capacity.

