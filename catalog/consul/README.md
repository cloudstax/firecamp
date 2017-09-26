The FireCamp Consul container is based on Debian Jessie. The data volume will be mounted to the /data directory inside container. The Consul data will be stored under /data/consul.

https://www.consul.io/docs/guides/consul-containers.html, see the known issues. We could assign the static IP for Consul.

## Logging

The Consul logs are sent to the Cloud Logs, such as AWS CloudWatch logs.

