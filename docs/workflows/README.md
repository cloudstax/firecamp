* [Service Scheduling Flow](https://github.com/cloudstax/firecamp/tree/master/docs/workflows#service-scheduling-flow)
* [Service Creation Flow](https://github.com/cloudstax/firecamp/tree/master/docs/workflows#service-creation-flow)

# Service Scheduling Flow
When service starts running or service container moves from one node to another, the container orchestration framework will schedule the container to one node. The container will invoke the FireCamp container plugin. The FireCamp container plugin will talk with the FireCamp KeyValue DB to find out which member the container belongs to. Then the container plugin will update the member address to the Registry Service, or reassign the IP from the old node to the current node if the service requires the static IP. And the container plugin will mount the volume to the local worker node.

![Service Scheduling Flow](https://s3.amazonaws.com/cloudstax/firecamp/docs/firecamp+service+scheduling+flow.png)

# Service Creation Flow
Users send the service creation request to the Catalog service. The Catalog service creates the volumes, such as AWS EBS volume, assigns the DNS names to each member, and persists the service related metadata into the FireCamp KeyValue DB.

Then the Catalog service talks with the underline container orchestration framework to create the service. The orchestration framework schedules the service to run, which follows the above "Service Scheduling Flow". The Catalog service waits till all service containers are running. Then the Catalog service schedules the additional initialization task if necessary. For example, for a MongoDB ReplicaSet, the initialization task will initialize the ReplicaSet Primary/Standby membership, create the admin user, create the key file for the internal authentication between members, and restart all service containers.

After the service is created and initialized, the Catalog service returns success to the user.

![Service Creation Flow](https://s3.amazonaws.com/cloudstax/firecamp/docs/firecamp+service+creation+flow.png)

