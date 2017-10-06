The FireCamp Consul container is based on the [offical consul image](https://hub.docker.com/r/_/consul/). The data volume will be mounted to the /data directory inside container. The Consul data will be stored under /data/consul.

https://www.consul.io/docs/guides/consul-containers.html, see the known issues. We could assign the static IP for Consul.

## Topology

Consul replicas will be evenly distributed to the availability zones in the cluster. If the cluster has 3 availability zones and the consul service has 3 replicas, each zone will have one replica. The production cluster should have 3 availability zones, to tolerate the availability zone failure.

The multiple datacenter (AWS Regions) will be supported in the future.

## Static IP

Consul requires using ip address. For example, [No name resolution at advertise parameter](https://github.com/hashicorp/consul/issues/1185). FireCamp will automatically assign one static ip to each Consul container. When the container moves to another node, the ip will follow, so consul container will always use that ip.

## Security

The secret key is supported for encryption of Consul network traffic.

ACL and TLS will be supported in the later release.

## Logging

The Consul logs are sent to the Cloud Logs, such as AWS CloudWatch logs.

