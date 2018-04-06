This is a guide for how to upgrade from release 0.9.4 to 0.9.5.

Upgrading to release 0.9.5 requires to stop all services first before upgrade, except firecamp management service. The upgrade takes around 10 minutes. So if you are running in production, unfortunately you will have to schedule a time window for upgrade. This is the first upgrade support. In the later release, the procedure will be further enhanced to keep the service live during upgrade.

Assume the cluster name is "t1" at region "us-east-1".
1. Stop all existing services in the cluster
For example, if you have a Cassandra service "mycas" running, stop it, `firecamp-service-cli -region=us-east-1 -cluster=t1 -op=stop-service -service-name=mycas`.

2. Upgrade cluster to 0.9.5
* Go to AWS CloudFormation Console, and select the root Stack.

  ![](https://s3.amazonaws.com/cloudstax/firecamp/docs/upgrade/0.9.5/upgrade1.png)

* Click "Actions" and select "Update Stack".

  ![](https://s3.amazonaws.com/cloudstax/firecamp/docs/upgrade/0.9.5/upgrade2.png)

* "Select Template": specify the template URL: https://s3.amazonaws.com/quickstart-reference/cloudstax/firecamp/latest/templates/firecamp.template, and click "Next".

  ![](https://s3.amazonaws.com/cloudstax/firecamp/docs/upgrade/0.9.5/upgrade3.png)

* "Specify Details": select 0.9.5 FireCamp Release and not change any other parameter, and then click "Next".

  ![](https://s3.amazonaws.com/cloudstax/firecamp/docs/upgrade/0.9.5/upgrade4.png)

* "Options": click "Next".

* "Review": check the acknowledge for IAM, and click "Update"

  ![](https://s3.amazonaws.com/cloudstax/firecamp/docs/upgrade/0.9.5/upgrade5.png)

3. Wait till the stack is successfully upgraded to 0.9.5

  ![](https://s3.amazonaws.com/cloudstax/firecamp/docs/upgrade/0.9.5/upgrade7.png)

4. Upgrade the existing services
Download the 0.9.5 CLI, `wget https://s3.amazonaws.com/cloudstax/firecamp/releases/0.9.5/packages/firecamp-service-cli.tgz`. And upgrade the Cassandra service, `firecamp-service-cli -region=us-east-1 -cluster=t1 -op=upgrade-service -service-type=cassandra -service-name=mycas`.

5. Start the existing services
`firecamp-service-cli -region=us-east-1 -cluster=t1 -op=start-service -service-name=mycas`.

Note: All services, except KafkaManager, could follow the stop, upgrade and start service. If you have KafkaManager service running, you will need to delete the KafkaManager service before upgrade and recreate it after upgrade completes. Release 0.9.5 has some internal change for the stateless service. To simplify the upgrade changes, KafkaManager needs to be recreated. This will not be required for upgrading 0.9.5 to later release.
