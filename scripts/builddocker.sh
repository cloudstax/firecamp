#!/bin/sh
set -x
set -e

export TOPWD="$(pwd)"

version=$1

org="cloudstax/"
system="openmanage"

# build docker log driver
echo "build the docker log driver"
path="${TOPWD}/build/docker-logdriver"
target="$system-dockerlogs"
image="${org}${target}:${version}"
mkdir -p ${path}/rootfs
cp ${GOPATH}/bin/openmanage-dockerlogs ${path}/rootfs/
cp ${TOPWD}/syssvc/openmanage-dockerlogs/config.json ${path}
docker plugin rm -f $image || true
docker plugin create $image $path
docker plugin push $image


# build test busybox docker image
echo
echo "build test busybox image for ecs and swarm unit test"
target="${system}-busybox"
image="${org}${target}:${version}"
path="${TOPWD}/containersvc/busybox-test-dockerfile/"
docker build -t $image $path
docker push $image

# build manageserver docker image
echo
target=$system"-manageserver"
image="${org}${target}:${version}"
binfile=$target
path="${TOPWD}/syssvc/openmanage-manageserver/dockerfile/"
cp $GOPATH/bin/$binfile $path
docker build -t $image $path
rm -f $path$binfile
docker push $image


# build controldb docker image
echo
target=$system"-controldb"
image="${org}${target}:${version}"
binfile=$target
path="${TOPWD}/syssvc/openmanage-controldb/dockerfile/"
cp $GOPATH/bin/$binfile $path
docker build -t $image $path
rm -f $path$binfile
docker push $image
echo


# build mongodb docker image
target=$system"-mongodb"
image="${org}${target}:${version}"
path="${TOPWD}/catalog/mongodb/3.4/dockerfile/"
docker build -t $image $path
docker push $image

echo
target=$system"-mongodb-init"
image="${org}${target}:${version}"
path="${TOPWD}/catalog/mongodb/3.4/init-task-dockerfile/"
cp ${TOPWD}/catalog/waitdns.sh ${path}
docker build -t $image $path
rm -f ${path}/waitdns.sh
docker push $image


# build postgres docker image
echo
target=$system"-postgres"
image="${org}${target}:${version}"
path="${TOPWD}/catalog/postgres/9.6/dockerfile/"
cp ${TOPWD}/catalog/waitdns.sh ${path}
docker build -t $image $path
rm -f ${path}/waitdns.sh
docker push $image


# build cassandra docker image
echo
target=$system"-cassandra"
image="${org}${target}:${version}"
path="${TOPWD}/catalog/cassandra/3.11/dockerfile/"
docker build -t $image $path
docker push $image

echo
target=$system"-cassandra-init"
image="${org}${target}:${version}"
path="${TOPWD}/catalog/cassandra/3.11/init-task-dockerfile/"
cp ${TOPWD}/catalog/waitdns.sh ${path}
docker build -t $image $path
rm -f ${path}/waitdns.sh
docker push $image


# build zookeeper docker image
echo
target=$system"-zookeeper"
image="${org}${target}:${version}"
path="${TOPWD}/catalog/zookeeper/3.4.10/dockerfile/"
docker build -t $image $path
docker push $image


# build kafka docker image
echo
target=$system"-kafka"
image="${org}${target}:${version}"
path="${TOPWD}/catalog/kafka/0.11/dockerfile/"
docker build -t $image $path
docker push $image


# build redis docker image
echo
target=$system"-redis"
image="${org}${target}:${version}"
path="${TOPWD}/catalog/redis/4.0.1/dockerfile/"
cp ${TOPWD}/catalog/waitdns.sh ${path}
docker build -t $image $path
rm -f ${path}/waitdns.sh
docker push $image

echo
target=$system"-redis-init"
image="${org}${target}:${version}"
path="${TOPWD}/catalog/redis/4.0.1/init-task-dockerfile/"
cp ${TOPWD}/catalog/waitdns.sh ${path}
docker build -t $image $path
rm -f ${path}/waitdns.sh
docker push $image

