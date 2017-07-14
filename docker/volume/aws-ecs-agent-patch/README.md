AWS ECS currently does not support to specify volume driver. Once AWS ECS supports
the custom volume driver, there is no need to patch ecs-agent.

Apply patch
1. copy openmanage_task_engine.go to cloudstax/amazon-ecs-agent/agent/engine/
2. apply docker_task_engine_patch to agent/engine/docker_task_engine.go
3. apply makefile_patch to Makefile

Then simply sudo make to build the ecs-agent container image.

To manually run the agent container, please initialize the agent first.
1. Initialize cloudstax/amazon-ecs-agent on EC2 for the first time, run start_ecs_agent.sh.
2. If the system is reboot after ecs-agent initialized, run: docker start ecs-agent-containerID.

Could also use RPM to automatically install the agent. The RPM could be downloaded from the CloudStax S3 bucket for every release. The RPM is built from [CloudStax Amazon ECS Container Agent](http://github.com/cloudstax/amazon-ecs-agent), which is a fork of the [Amazon ECS Init](https://github.com/aws/amazon-ecs-init) with the patch applied.
