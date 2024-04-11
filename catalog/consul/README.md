* [FireCamp Consul Internals](https://github.com/jazzl0ver/firecamp/pkg/tree/master/catalog/consul#firecamp-consul-internals)
* [Tutorials](https://github.com/jazzl0ver/firecamp/pkg/tree/master/catalog/consul#tutorials)

# FireCamp Consul Internals

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


# Tutorials

This is a simple tutorial about how to create a Consul service and how to use it. This tutorial assumes the cluster name is "t1", the AWS Region is "us-east-1", and the Consul service name is "myconsul".

## Create a Consul service
Follow the [Installation Guide](https://github.com/jazzl0ver/firecamp/pkg/tree/master/docs/installation) guide to create a 3 nodes cluster across 3 availability zones. Create a Consul cluster:
```
firecamp-service-cli -op=create-service -service-type=consul -region=us-east-1 -cluster=t1 -replicas=3 -volume-size=10 -service-name=myconsul
```
Outputs:
```
The consul service created, Consul server ips [172.31.64.5 172.31.64.6 172.31.64.4]
```

This creates a 3 replicas Consul on 3 availability zones. The database replicas will be distributed to different availability zone, to tolerate one availability zone failure. Every replica has 10GB volume. The static ips of the consul servers are: "172.31.64.5 172.31.64.6 172.31.64.4"

## Join a Consul Agent Node
Launch a EC2 instance in the VPC and AppAccessSecurityGroup, which are in the output of the CloudFormation stack that launches the FireCamp cluster. Install docker on the instance, pull consul image `docker pull consul`, and then run a consul container at the client mode.
```
sudo docker run -d --net=host -e 'CONSUL_LOCAL_CONFIG={"leave_on_terminate": true, "datacenter": "us-east-1"}' consul agent -bind=EC2InstanceAddr -retry-join=172.31.64.4
```

## Try Consul operations
Try the consul operations on the launched EC2 instance.
1. Check Service Health: `curl http://localhost:8500/v1/health/service/consul?pretty`
2. DNS Lookup: `dig @localhost -p 8600 consul.service.consul`
3. KV Store: `curl -X PUT http://localhost:8500/v1/kv/key1 -d '{ "a": "a1", "b": "b1" }'`

