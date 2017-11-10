The FireCamp Logstash container is based on the offical elastic.co logstash image, docker.elastic.co/logstash/logstash:5.6.3. The data volume will be mounted to the /data directory inside container. The Logstash data will be stored under /data/logstash.

## Logstash

**Topology**

One Logstash member only runs in one availability zone. To tolerate one availability zone failure, the Logstash service could have multiple replicas. FireCamp will distribute the replicas evenly to availability zones.

**Memory**

By default, FireCamp sets 2GB for Logstash heap. As Logstash suggests, the Xms and Xmx are set to the same value to prevent the heap from resizing at runtime. You could adjust the memory size by setting the "ls-heap-size" when creating the service.

**Pipeline Config File**

FireCamp Logstash supports one config file, as Logstash currently has a single event pipeline. All configuration files are just concatenated (in order) as if you had written a single flat file. You should use "if" conditions to select the filters and outputs. See [Running multiple config files](https://discuss.elastic.co/t/running-multiple-independent-logstash-config-files-with-input-filter-and-output/29757), and [Multiple Logstash config files](https://discuss.elastic.co/t/solved-multiple-logstash-config-file/51692).

Please help to verify your config file is valid before creating the service. Could install logstash and run: /usr/share/logstash/bin/logstash -t -f your-logstash.conf.

**Pipeline tuning**

FireCamp Logstash uses the default Pipeline parameters. If you want to tune the parameters, could set them when creating the service. Updating the configs will be supported in the coming releases.

## Plugins

**Custom Plugins**

The CouchDB input plugin is supported as the example for how to support the custom plugin, catalog/logstash/5.6.3/dockerfile-input-couchdb. To use the CouchDB input plugin, specify -ls-container-image=cloudstax/firecamp-logstash-input-couchdb:latest at the service creation.

If the additional custom plugin is required, we could follow the same way to create a new docker image and specify it when creating the service. Note, this is currently disabled until we hear the actual requirement.

**Plugin Security**

Some plugins, such as the beats input plugin, supports SSL. This will be supported in the coming releases.

**Plugin Listen Port**

Logstash docker image exposes port 9600 and 5044. 9600 is used by Logstash metrics. 5044 is used by the beats plugin. If you use some plugin that requires to listen on another port, we could follow the same mechanism with the custom plugin, to create a new docker image to expose the port.

## Logging

The Logstash logs are sent to the Cloud Logs, such as AWS CloudWatch logs.


Refs:

1. [Performance Troubleshooting](https://www.elastic.co/guide/en/logstash/current/performance-troubleshooting.html)

2. [Tuning and Profiling Logstash Performance](https://www.elastic.co/guide/en/logstash/5.6/tuning-logstash.html)

