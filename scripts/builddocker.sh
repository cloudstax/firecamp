#!/bin/sh
set -x
set -e

export TOPWD="$(pwd)"

version=$1

org="cloudstax/"
system="openmanage"

# build test busybox docker image
target="${system}-busybox"
image="${org}${target}:${version}"
path="${TOPWD}/containersvc/testimage-dockerfile/"
echo "build test busybox image for ecs and swarm unit test"
docker build -t $image $path
docker push $image

# build manageserver docker image
target=$system"-manageserver"
image="${org}${target}:${version}"
binfile=$target
path="${TOPWD}/syssvc/openmanage-manageserver/dockerfile/"
echo
cp $GOPATH/bin/$binfile $path
docker build -t $image $path
rm -f $path$binfile
docker push $image


# build controldb docker image
target=$system"-controldb"
image="${org}${target}:${version}"
binfile=$target
path="${TOPWD}/syssvc/openmanage-controldb/dockerfile/"
echo
cp $GOPATH/bin/$binfile $path
docker build -t $image $path
rm -f $path$binfile
docker push $image


# build mongodb docker image
target=$system"-mongodb"
image="${org}${target}:${version}"
path="${TOPWD}/catalog/mongodb/3.4/dockerfile/"
echo
docker build -t $image $path
docker push $image

target=$system"-mongodb-init"
image="${org}${target}:${version}"
path="${TOPWD}/catalog/mongodb/3.4/init-task-dockerfile/"
echo
docker build -t $image $path
docker push $image


# build postgres docker image
target=$system"-postgres"
image="${org}${target}:${version}"
path="${TOPWD}/catalog/postgres/9.6/dockerfile/"
echo
docker build -t $image $path
docker push $image


