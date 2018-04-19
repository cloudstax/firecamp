#!/bin/bash
set -ex

rootdir=/data
datadir=/data/elasticsearch
confdir=/data/conf

# after release 0.9.5, these files are not created any more.
jvmcfgfile=$confdir/jvm.options
syscfgfile=$confdir/sys.conf

escfgfile=$confdir/elasticsearch.yml
servicecfgfile=$confdir/service.conf
membercfgfile=$confdir/member.conf

escfgdir=/usr/share/elasticsearch/config

USER=elasticsearch

# sanity check to make sure the volume is mounted to /data.
if [ ! -d "$rootdir" ]; then
  echo "error: $rootdir not exist. Please make sure the volume is mounted to $rootdir." >&2
  exit 1
fi
if [ ! -d "$datadir" ]; then
  mkdir "$datadir"
fi
if [ ! -d "$confdir" ]; then
  echo "error: $confdir not exist." >&2
  exit 1
fi
# sanity check to make sure the config files are created.
if [ ! -f "$escfgfile" ]; then
  echo "error: $escfgfile not exist." >&2
  exit 1
fi

# allow the container to be started with `--user`
if [ "$1" = 'elasticsearch' -a "$(id -u)" = '0' ]; then
  rootdiruser=$(stat -c "%U" $rootdir)
  if [ "$rootdiruser" != "$USER" ]; then
    echo "chown -R $USER $rootdir"
    chown -R "$USER" "$rootdir"
  fi
  diruser=$(stat -c "%U" $datadir)
  if [ "$diruser" != "$USER" ]; then
    chown -R "$USER" "$datadir"
  fi
  chown -R "$USER" "$confdir"

  exec gosu "$USER" "$BASH_SOURCE" "$@"
fi

cp $escfgfile $escfgdir

if [ -f $servicecfgfile ]; then
  # after release 0.9.5, load service and member configs
  . $servicecfgfile
  . $membercfgfile

  # update default elasticsearch.yml file
  echo "node.name: $MEMBER_NAME" >> $escfgdir/elasticsearch.yml
  echo "node.attr.zone: $AVAILABILITY_ZONE" >> $escfgdir/elasticsearch.yml
  echo "network.bind_host: $BIND_IP" >> $escfgdir/elasticsearch.yml
  echo "network.publish_host: $SERVICE_MEMBER" >> $escfgdir/elasticsearch.yml

  # member role: master or data node
  # https://www.elastic.co/guide/en/elasticsearch/reference/current/modules-node.html
  if [ "$MEMBER_ROLE" = "master" ]; then
    echo "node.master: true" >> $escfgdir/elasticsearch.yml
    echo "node.data: false" >> $escfgdir/elasticsearch.yml
    echo "node.ingest: false" >> $escfgdir/elasticsearch.yml
  elif [ "$MEMBER_ROLE" = "data" ]; then
    echo "node.master: false" >> $escfgdir/elasticsearch.yml
    echo "node.data: true" >> $escfgdir/elasticsearch.yml
    echo "node.ingest: true" >> $escfgdir/elasticsearch.yml
  else
    # the node is eligible for master or data
    echo "node.master: true" >> $escfgdir/elasticsearch.yml
    echo "node.data: true" >> $escfgdir/elasticsearch.yml
    echo "node.ingest: true" >> $escfgdir/elasticsearch.yml
  fi

  # update jvm.options file
  # docker entrypoint will set ES_JAVA_OPTS for Xms and Xmx.
  # simply set ES_JAVA_OPTS for Xms and Xmx is not enough. has to comment out
  # the default setting in the jvm.options file.
  # https://www.elastic.co/guide/en/elasticsearch/reference/current/heap-size.html
  sed -i '/-Xms/d' $escfgdir/jvm.options
  sed -i '/-Xmx/d' $escfgdir/jvm.options
  # heap dump path set to the external ElasticSearch data volume.
  sed -i 's/#-XX:HeapDumpPath=.*/-XX:HeapDumpPath=\/data\/heapdump.hprof/g' $escfgdir/jvm.options
  # gc detail log enabled with log file rotation.
  sed -i 's/#-XX:+PrintGCDetails/-XX:+PrintGCDetails/g' $escfgdir/jvm.options
  sed -i 's/#-XX:+PrintGCTimeStamps/-XX:+PrintGCTimeStamps/g' $escfgdir/jvm.options
  sed -i 's/#-XX:+PrintGCApplicationStoppedTime/-XX:+PrintGCApplicationStoppedTime/g' $escfgdir/jvm.options
  sed -i 's/#-Xloggc:.*/-Xloggc:\/data\/ecgc-%t.log/g' $escfgdir/jvm.options
  echo "" >> $escfgdir/jvm.options
  echo "-XX:+UseGCLogFileRotation" >> $escfgdir/jvm.options
  echo "-XX:NumberOfGCLogFiles=8" >> $escfgdir/jvm.options
  echo "-XX:GCLogFileSize=64M" >> $escfgdir/jvm.options

  # set java opts
  export ES_JAVA_OPTS="-Xms${HEAP_SIZE_MB}m -Xmx${HEAP_SIZE_MB}m"

else
  # before release 0.9.6, load the sys config file
  cp $jvmcfgfile $escfgdir
  . $syscfgfile
fi

echo $SERVICE_MEMBER

echo "$@"
exec "$@"
