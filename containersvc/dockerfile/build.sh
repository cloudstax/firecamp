#!/bin/sh

# build the openconnectio/openmanage-busybox image for ecs and swarm unit test
target=openmanage-busybox

docker build -t openconnectio/$target .

