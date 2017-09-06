#!/bin/bash -x

clusterName=$1

# set never for THP (Transparent Huge Pages), as THP might impact the performance for some services
# such as MongoDB and Reddis. Could set to madvise if some service really benefits from madvise.
# https://www.kernel.org/doc/Documentation/vm/transhuge.txt
echo never | sudo tee /sys/kernel/mm/transparent_hugepage/enabled

# increase somaxconn to 512 for such as Redis
echo "net.core.somaxconn=512" >> /etc/sysctl.conf
sysctl -w net.core.somaxconn=512
echo "net.ipv4.tcp_max_syn_backlog=512" >> /etc/sysctl.conf
sysctl -w net.ipv4.tcp_max_syn_backlog=512

# set overcommit to 1 as required by Redis. Would not cause issue to other services
echo "vm.overcommit_memory=1" >> /etc/sysctl.conf
sysctl -w vm.overcommit_memory=1

# install docker
yum install -y docker

# Kafka uses a very large number of files, increase the file descriptor count.
# AWS AMI sets the ulimit for docker daemon, OPTIONS=\"--default-ulimit nofile=1024:4096\".
# The container inherits the docker daemon ulimit.
# The docker daemon config file is different on different Linux. AWS AMI is /etc/sysconfig/docker.
# Ubuntu is /etc/init/docker.conf
sed -i 's/OPTIONS=\"--default-ulimit.*/OPTIONS=\"--default-ulimit nofile=100000:100000\"/g' /etc/sysconfig/docker

service docker start

# install cloudstax ecs init
wget -O /tmp/cloudstax-ecs-init-1.14.4-1.amzn1.x86_64.rpm https://s3.amazonaws.com/cloudstax/firecamp/releases/latest/packages/cloudstax-ecs-init-1.14.4-1.amzn1.x86_64.rpm
rpm -ivh /tmp/cloudstax-ecs-init-1.14.4-1.amzn1.x86_64.rpm
echo "ECS_CLUSTER=$clusterName" >> /etc/ecs/ecs.config
start ecs

# install firecamp docker volume plugin
mkdir -p /var/log/firecamp
docker plugin install --grant-all-permissions cloudstax/firecamp-volume:latest

# TODO enable log driver after upgrade to 17.05/06
# install firecamp docker log driver
# docker plugin install cloudstax/firecamp-logs

