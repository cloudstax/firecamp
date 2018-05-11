AWS ECS currently does not support to specify volume driver. See ECS Open Issue [Volume Driver support](https://github.com/aws/amazon-ecs-agent/issues/236). Once AWS ECS supports the custom volume driver, there is no need to patch ecs-agent.

Apply patch
1. copy firecamp_task_engine.go to cloudstax/amazon-ecs-agent/agent/engine/
2. apply docker_task_engine_patch to agent/engine/docker_task_engine.go
3. apply makefile_patch to Makefile

Then simply sudo make to build the ecs-agent container image.

To manually run the agent container, please initialize the agent first.
1. Initialize cloudstax/amazon-ecs-agent on EC2 for the first time, run start_ecs_agent.sh.
2. If the system is reboot after ecs-agent initialized, run: docker start ecs-agent-containerID.

We also fork the [Amazon ECS Init](https://github.com/cloudstax/amazon-ecs-init) to directly load the ECS Agent container image from docker hub. The FireCamp cluster auto deployment (AWS CloudFormation) will automatically download the Amazon ECS Init RPM from CloudStax S3 bucket and install on the node. The ECS Init will manage the [CloudStax Amazon ECS Container Agent](http://github.com/cloudstax/amazon-ecs-agent).

Docker plugin is uniquely identified by the plugin name and version, such as cloudstax/firecamp-volume:v1. The ECS service task definition will include the current version as environment variable. So ECS Agent patch could get the version and set the plugin name correctly.


https://help.github.com/articles/syncing-a-fork/

git fetch upstream
git checkout master
git merge upstream/master
