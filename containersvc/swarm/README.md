By default, docker swarm manager listens on ip:2377 with TLS enabled. And [the swarm manager tls files are not exposed for the external use](https://forums.docker.com/t/cannot-connect-to-cluster-created-with-swarm-mode/16826/7).

One solution is to start docker daemon with our own CA certificate. So the FireCamp manageserver could talk with the docker daemon on the Swarm manager nodes. While, this will not work on the swarm cluster created with the default TLS.

It looks better to run the FireCamp manageserver container on the Swarm manager nodes, and talk with Swarm manager via the unix socket. The customer could easily add FireCamp to the existing swarm cluster. The FireCamp manageserver container is very light. It is ok to run on the swarm manager node.

Docker swarm service could [pass the task slot in the volume source name](https://docs.docker.com/docker-for-aws/persistent-data-volumes/#use-a-unique-volume-per-task-using-ebs) to the volume plugin. So the volume plugin does not need to use the swarm service to talk with the swarm manager.

