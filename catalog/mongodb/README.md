The OpenManage MongoDB container is based on Debian. The data volume will be mounted to the /data directory inside container. The MongoDB data will be stored at the /data/db directory. And the config file is at the /data/conf directory.

The MongoDB container writes the logs to a file. MongoDB does not rotate the logs by default. See MongoDB Administration, [Rotate Log Files](https://docs.mongodb.com/manual/tutorial/rotate-log-files): "MongoDB only rotates logs in response to the [logRotate](https://docs.mongodb.com/manual/reference/command/logRotate/#dbcmd.logRotate) command, or when the mongod or mongos process receives a SIGUSR1 signal from the operating system". So the log file could grow infinitely.

https://stackoverflow.com/questions/5004626/mongodb-log-file-growth
https://dba.stackexchange.com/questions/25658/how-to-keep-mongodb-log-small-via-the-conf-file-or-other-persistent-method

 add logrotated to container. send log to CloudWatch.


MongoDB security
https://docs.mongodb.com/manual/administration/security-checklist/

