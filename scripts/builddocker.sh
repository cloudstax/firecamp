#!/bin/sh
set -x
set -e

export TOPWD="$(pwd)"

org=$1
version=$2
buildtarget=$3

system="firecamp"

replaceOrgName="s!%%OrgName%%!${org}!g;"

BuildPlugin() {
  path="${TOPWD}/scripts/plugin-dockerfile"
  target="$system-pluginbuild"
  image="${org}${target}"

  echo "### docker build: builder image"
	docker build -q -t $image $path

	echo "### docker run: builder image with source code dir mounted"
  containername="$system-buildtest"
  docker rm $containername || true
  # the build container not exist, create and run it
  docker run --name $containername -v ${TOPWD}:/go/src/github.com/cloudstax/firecamp $image

  # build the volume plugin
  volumePluginPath="${TOPWD}/syssvc/firecamp-dockervolume/dockerfile"
  volumePluginImage="${org}${system}-volume"
	echo "### docker build: rootfs image with firecamp-dockervolume"
	docker cp $containername:/go/bin/firecamp-dockervolume $volumePluginPath
	docker build -q -t ${volumePluginImage}:rootfs $volumePluginPath
  rm -f $volumePluginPath/firecamp-dockervolume

  echo "### create the plugin rootfs directory"
  volumePluginBuildPath="${TOPWD}/build/volumeplugin"
	mkdir -p $volumePluginBuildPath/rootfs
  docker rm -vf tmp || true
	docker create --name tmp ${volumePluginImage}:rootfs
	docker export tmp | tar -x -C $volumePluginBuildPath/rootfs
	cp ${TOPWD}/syssvc/firecamp-dockervolume/config.json $volumePluginBuildPath
	docker rm -vf tmp

	echo "### create new plugin ${volumePluginImage}:${version}"
	docker plugin rm -f ${volumePluginImage}:${version} || true
	docker plugin create ${volumePluginImage}:${version} $volumePluginBuildPath
	docker plugin push ${volumePluginImage}:${version}


  # build the log plugin
  logPluginPath="${TOPWD}/syssvc/firecamp-dockerlog/dockerfile"
  logPluginImage="${org}${system}-log"
	echo "### docker build: rootfs image with firecamp-dockerlog"
	docker cp $containername:/go/bin/firecamp-dockerlog $logPluginPath
	docker build -q -t ${logPluginImage}:rootfs $logPluginPath
  rm -f $logPluginPath/firecamp-dockerlog

  echo "### create the plugin rootfs directory"
  logPluginBuildPath="${TOPWD}/build/logplugin"
	mkdir -p $logPluginBuildPath/rootfs
  docker rm -vf tmp || true
	docker create --name tmp ${logPluginImage}:rootfs
	docker export tmp | tar -x -C $logPluginBuildPath/rootfs
	cp ${TOPWD}/syssvc/firecamp-dockerlog/config.json $logPluginBuildPath
	docker rm -vf tmp

	echo "### create new plugin ${logPluginImage}:${version}"
	docker plugin rm -f ${logPluginImage}:${version} || true
	docker plugin create ${logPluginImage}:${version} $logPluginBuildPath
	docker plugin push ${logPluginImage}:${version}
}

BuildManageImages() {
  # build test busybox docker image
  echo
  echo "build test busybox image for ecs and swarm unit test"
  target="${system}-busybox"
  image="${org}${target}:${version}"
  path="${TOPWD}/containersvc/busybox-test-dockerfile/"
  docker build -q -t $image $path
  docker push $image

  # build manageserver docker image
  echo
  target=$system"-manageserver"
  image="${org}${target}:${version}"
  binfile=$target
  path="${TOPWD}/syssvc/firecamp-manageserver/dockerfile/"
  cp $GOPATH/bin/$binfile $path
  docker build -q -t $image $path
  rm -f $path$binfile
  docker push $image

  # build initcontainer docker image
  echo
  target=$system"-initcontainer"
  image="${org}${target}:${version}"
  binfile=$target
  path="${TOPWD}/containersvc/k8s/firecamp-initcontainer/"
  cp $GOPATH/bin/$binfile $path
  docker build -q -t $image $path
  rm -f $path$binfile
  docker push $image

  echo
  # docker image to clean up when the service container stops, such as deleting the static ip.
  target=$system"-stopcontainer"
  image="${org}${target}:${version}"
  binfile=$system"-stopcontainer"
  path="${TOPWD}/containersvc/k8s/firecamp-stopcontainer/"
  cp $GOPATH/bin/$binfile $path
  docker build -q -t $image $path
  rm -f $path$binfile
  docker push $image


  # build controldb docker image
  echo
  target=$system"-controldb"
  image="${org}${target}:${version}"
  binfile=$target
  path="${TOPWD}/syssvc/firecamp-controldb/dockerfile/"
  cp $GOPATH/bin/$binfile $path
  docker build -q -t $image $path
  rm -f $path$binfile
  docker push $image
  echo

}

BuildCatalogImages() {
  # build mongodb docker image
  target=$system"-mongodb"
  image="${org}${target}:3.4"
  path="${TOPWD}/catalog/mongodb/3.4/dockerfile/"
  docker build -q -t $image $path
  docker push $image

  echo
  target=$system"-mongodb-init"
  image="${org}${target}:3.4"
  path="${TOPWD}/catalog/mongodb/3.4/init-task-dockerfile/"
  cp ${TOPWD}/catalog/waitdns.sh ${path}
  docker build -q -t $image $path
  rm -f ${path}/waitdns.sh
  docker push $image


  # build postgres docker image
  echo
  target=$system"-postgres"
  image="${org}${target}:9.6"
  path="${TOPWD}/catalog/postgres/9.6/dockerfile/"
  cp ${TOPWD}/catalog/waitdns.sh ${path}
  docker build -q -t $image $path
  rm -f ${path}/waitdns.sh
  docker push $image

  # build postgres postgis docker image
  echo
  target=$system"-postgres-postgis"
  image="${org}${target}:9.6"
  path="${TOPWD}/catalog/postgres/9.6/postgis-dockerfile/"
  cd $path
  sed -r "$replaceOrgName" Dockerfile.template > Dockerfile
  docker build -q -t $image .
  docker push $image
  rm -f Dockerfile
  cd -

  # build cassandra docker image
  echo
  target=$system"-cassandra"
  image="${org}${target}:3.11"
  path="${TOPWD}/catalog/cassandra/3.11/dockerfile/"
  docker build -q -t $image $path
  docker push $image

  echo
  target=$system"-cassandra-init"
  image="${org}${target}:3.11"
  path="${TOPWD}/catalog/cassandra/3.11/init-task-dockerfile/"
  cp ${TOPWD}/catalog/waitdns.sh ${path}
  docker build -q -t $image $path
  rm -f ${path}/waitdns.sh
  docker push $image


  # build zookeeper docker image
  echo
  target=$system"-zookeeper"
  image="${org}${target}:3.4"
  path="${TOPWD}/catalog/zookeeper/3.4/dockerfile/"
  docker build -q -t $image $path
  docker push $image


  # build kafka docker image
  echo
  target=$system"-kafka"
  image="${org}${target}:1.0"
  path="${TOPWD}/catalog/kafka/1.0/dockerfile/"
  docker build -q -t $image $path
  docker push $image


  # build redis docker image
  echo
  target=$system"-redis"
  image="${org}${target}:4.0"
  path="${TOPWD}/catalog/redis/4.0/dockerfile/"
  cp ${TOPWD}/catalog/waitdns.sh ${path}
  docker build -q -t $image $path
  rm -f ${path}/waitdns.sh
  docker push $image

  echo
  target=$system"-redis-init"
  image="${org}${target}:4.0"
  path="${TOPWD}/catalog/redis/4.0/init-task-dockerfile/"
  cp ${TOPWD}/catalog/waitdns.sh ${path}
  docker build -q -t $image $path
  rm -f ${path}/waitdns.sh
  docker push $image


  # build couchdb docker image
  echo
  target=$system"-couchdb"
  image="${org}${target}:2.1"
  path="${TOPWD}/catalog/couchdb/2.1/dockerfile/"
  docker build -q -t $image $path
  docker push $image

  echo
  target=$system"-couchdb-init"
  image="${org}${target}:2.1"
  path="${TOPWD}/catalog/couchdb/2.1/init-task-dockerfile/"
  cp ${TOPWD}/catalog/waitdns.sh ${path}
  docker build -q -t $image $path
  rm -f ${path}/waitdns.sh
  docker push $image


  # build consul docker image
  echo
  target=$system"-consul"
  image="${org}${target}:1.0"
  path="${TOPWD}/catalog/consul/1.0/dockerfile/"
  docker build -q -t $image $path
  docker push $image


  # build elasticsearch docker image
  echo
  target=$system"-elasticsearch"
  image="${org}${target}:5.6"
  path="${TOPWD}/catalog/elasticsearch/5.6/dockerfile/"
  docker build -q -t $image $path
  docker push $image


  # build kibana docker image
  echo
  target=$system"-kibana"
  image="${org}${target}:5.6"
  path="${TOPWD}/catalog/kibana/5.6/dockerfile/"
  docker build -q -t $image $path
  docker push $image


  # build logstash docker image
  echo
  target=$system"-logstash"
  image="${org}${target}:5.6"
  path="${TOPWD}/catalog/logstash/5.6/dockerfile/"
  docker build -q -t $image $path
  docker push $image

  # build logstash docker image with couchdb input plugin
  echo
  target=$system"-logstash-input-couchdb"
  image="${org}${target}:5.6"
  path="${TOPWD}/catalog/logstash/5.6/input-couchdb-dockerfile/"
  cd $path
  sed -r "$replaceOrgName" Dockerfile.template > Dockerfile
  docker build -q -t $image .
  docker push $image
  rm -f Dockerfile
  cd -

}

if [ "$buildtarget" = "all" ]; then
  BuildPlugin
  BuildManageImages
  BuildCatalogImages
elif [ "$buildtarget" = "pluginimages" ]; then
  BuildPlugin
elif [ "$buildtarget" = "manageimages" ]; then
  BuildManageImages
elif [ "$buildtarget" = "catalogimages" ]; then
  BuildCatalogImages
fi

