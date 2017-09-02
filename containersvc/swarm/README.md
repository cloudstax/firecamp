# FireCamp Swarm Internal

By default, docker swarm manager listens on ip:2377 with TLS enabled. And [the swarm manager tls files are not exposed for the external use](https://forums.docker.com/t/cannot-connect-to-cluster-created-with-swarm-mode/16826/7).

One solution is to start docker daemon with our own CA certificate. So the FireCamp manageserver could talk with the docker daemon on the Swarm manager nodes. While, this will not work on the existing swarm cluster created with the default TLS.

It looks better to run the FireCamp manageserver container on the Swarm manager nodes, and talk with Swarm manager via the unix socket. The customer could easily add FireCamp to the existing or new swarm cluster. The FireCamp manageserver container is very light. It is ok to run on the swarm manager node.

Docker swarm service could [pass the task slot in the volume source name](https://docs.docker.com/docker-for-aws/persistent-data-volumes/#use-a-unique-volume-per-task-using-ebs) to the volume plugin. So the volume plugin could directly know which member the container is for.


# Install FireCamp on the existing Swarm cluster

1. Create the FireCamp IAM and assign to all Swarm nodes.

Use packaging/aws-cloudformation/firecamp-iamprofile.template to create the IAM.

2. Decide the cluster name you want to assign to the Swarm cluster.

FireCamp assigns a unique DNS name for every service member. For example, the cluster name is c1, the service name is mymongo and has 3 members. FireCamp will assign mymongo-0.c1-firecamp.com, mymongo-1.c1-firecamp.com and mymongo-2.c1-firecamp.com to these 3 members.

The cluster name has to be unique for the swarm clusters in the same VPC. If 2 swarm clusters have the same name and creates the service with the same name as well, the members of these 2 services will have the same DNS name. This will mess up the service membership. Different VPCs will have different DNS servers, different HostedZone in AWS Route53. So the swarm clusters in different VPCs could have the same name.

3. Create the FireCamp manageserver service on the swarm manager node.

The example command:
`docker service create --name mgt --constraint node.role==manager --publish mode=host,target=27040,published=27040  --mount type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock --replicas 1 -e CONTAINER_PLATFORM=swarm -e DB_TYPE=clouddb -e AVAILABILITY_ZONES=us-east-1a -e CLUSTER=c1 cloudstax/firecamp-manageserver:tag`

You could change the service name, AVAILABILITY_ZONES, CLUSTER and the manageserver docker image tag. Please do NOT change others.

4. Install FireCamp plugin on every swarm worker node.

Create the FireCamp log directory: `sudo mkdir -p /var/log/firecamp`.

Instal plugin: `docker plugin install --grant-all-permissions cloudstax/firecamp-volume:tag PLATFORM=swarm CLUSTER=c1`

5. Create the stateful service.

For example, to create a 2 replicas PostgreSQL, copy the firecamp-service-cli to the manager node and simply run: `firecamp-service-cli -cluster=c1 -op=create-service -service-name=pg1 -service-type=postgresql -replicas=2 -volume-size=1`
