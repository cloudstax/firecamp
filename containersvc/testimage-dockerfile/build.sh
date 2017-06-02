#!/bin/sh

# build the cloudstax/openmanage-busybox image for ecs and swarm unit test
target=openmanage-busybox

docker build -t cloudstax/$target .

