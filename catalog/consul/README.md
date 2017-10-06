The FireCamp Consul container is based on the [offical Consul image](https://hub.docker.com/r/_/consul/). The data volume will be mounted to the /data directory inside container. The Consul data will be stored under /data/consul.

## Topology

Consul replicas will be evenly distributed to the availability zones in the cluster. If the cluster has 3 availability zones and the consul service has 3 replicas, each zone will have one replica. The production cluster should have 3 availability zones, to tolerate the availability zone failure.

By default, Consul service sets the current AWS Region as datacenter. If you want to specify your own datacenter name, could specify it when creating the consul service, "-consul-datacenter=yourdcname".

The multiple datacenter (AWS Regions) will be supported in the future.

## Static IP

Consul requires using ip address. For example, [No name resolution at advertise parameter](https://github.com/hashicorp/consul/issues/1185). FireCamp will automatically assign one static ip to each Consul container. When the container moves to another node, the ip will follow, so consul container will always use that ip.

The firecamp service cli will return the assigned ips for the consul servers. You could then run the consul agent (the client mode) on the application node, to use Consul's functions. For example, pull the consul official docker image, `docker pull consul`. Then run consul, `docker run -d --net=host -e 'CONSUL_LOCAL_CONFIG={"leave_on_terminate": true, "datacenter": "us-east-1"}' consul agent -bind=<current node ip> -retry-join=<one consul server ip>`.

## Security

The secret key is supported for encryption of Consul network traffic.

ACL and TLS will be supported in the later release.

## Logging

The Consul logs are sent to the Cloud Logs, such as AWS CloudWatch logs.

