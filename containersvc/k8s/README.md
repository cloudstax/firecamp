# FireCamp Kubernetes

FireCamp stateful services on Kubernetes is built on top of [Kubernetes StatefulSet](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/). The StatefulSet binds the storage volume with one Pod. When the Pod moves to another node, the storage volume moves as well.

FireCamp builds a few more things on top of the StatefulSet.

**Manage the stateful service**

FireCamp distributes the members of one stateful service across Availability Zones. When one Availability Zone goes down, the service will still work. The common mechanisms such as Security, Upgrade, Monitoring, Data Management, etc, also work for the stateful servics on Kubernetes.

**Integrate with AWS network**

FireCamp minimizes the dependency on the external components. FireCamp does not rely on any third party network plugin, such as Calico. FireCamp directly integrate with [AWS Route53](https://aws.amazon.com/route53/) for DNS and [EC2 Secondary Private IP](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/MultipleIP.html) for the Static IP support. The application running on EC2, Container or Lambda could easily talk with the stateful service without the need to setup such as the Proxy Verb. For more details, please refer to [FireCamp Architect](https://github.com/cloudstax/firecamp/tree/master/docs/architect#network).

**Send logs to AWS CloudWatch**

Kubernetes does not support the custom log driver. FireCamp deploys a Fluentd DaemonSet. The Fluentd is configured to create the log stream for each service member under the same log group. For the detail log stream and group name, please refer to the [Catalog Service Log](https://github.com/cloudstax/firecamp/tree/master/catalog#logging).


## FireCamp Kubernetes StatefulSet

**PodManagementPolicy**: The ParallelPodManagement policy is used instead of OrderedReadyPodManagement. The OrderedReadyPodManagement create pods in strictly increasing order. This may introduce some issue when running in cloud. For example, Cassandra service has 3 replicas on 3 AZs. The replica0 is on AZ1. If AZ1 goes down, the pods for replica1 and 2 will keep waiting for replica0.

**StatefulSetUpdateStrategy**: The OnDeleteStatefulSetStrategyType is used instead of RollingUpdateStatefulSetStrategyType. As the RollingUpdateStatefulSetStrategyType is not suitable for the master-slave service, such as MongoDB, Postgres, etc. The graceful update should restart the master first, then the slaves. FireCamp will perform the update according to the service's requirement. FireCamp will track and restart the master container first for the master-slave services.

Note: this is not implemented yet. Currently the service is simply rolling updated.

## How to Run FireCamp on the existing kubernetes

Currently FireCamp does not set up Kubernetes on AWS. We will integrate with AWS EKS (Elastic Container Service for Kubernetes) for the deployment once EKS is officially announced.

If you have a Kubernetes cluster in AWS, you could deploy FireCamp following below steps:

1. Add FireCamp IAM permissions to your node.
The detail IAM permissions are described in the firecamp-iamprofile.template.

2. Add FireCamp tag to each node, tag key: firecamp-worker, value: clustername.

3. Create the Fluentd DaemonSet to send logs to CloudWatch.
Update the AWS_REGION and FIRECAMP_CLUSTER_NAME in fluentd-cw-ds.yaml, and run `kubectl create -f fluentd-cw-configmap.yaml` and `kubectl create -f fluentd-cw-ds.yaml`.

4. Create the FireCamp manage service ReplicaSet.
Update the AVAILABILITY_ZONES and CLUSTER in manageservice-replicaset.yaml, and run `kubectl create -f manageservice-rbac.yaml` and `kubectl create -f manageservice-replicaset.yaml`.

Note: Kubernetes nodes should run in all AVAILABILITY_ZONES. If one Availability Zone does not have the running node, the stateful service will fail to start. The CLUSTER should be set to the same with FIRECAMP_CLUSTER_NAME in fluentd-cw-ds.yaml.

Then you could login to any k8s node, download firecamp cli and create the stateful service.
