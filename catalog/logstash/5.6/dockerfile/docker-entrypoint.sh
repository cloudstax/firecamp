#!/bin/bash
set -ex

rootdir=/data
datadir=/data/logstash
confdir=/data/conf

# after release 0.9.5, these files are not created any more.
syscfgfile=$confdir/sys.conf
jvmcfgfile=$confdir/jvm.options
lsymlfile=$confdir/logstash.yml

lspipelinecfgfile=$confdir/logstash.conf
servicecfgfile=$confdir/service.conf
membercfgfile=$confdir/member.conf

cfgdir=/usr/share/logstash/config
pipelinedir=/usr/share/logstash/pipeline

USER=logstash

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
if [ ! -f "$lspipelinecfgfile" ]; then
  echo "error: $lspipelinecfgfile not exist." >&2
  exit 1
fi

# allow the container to be started with `--user`
if [ "$1" = 'logstash' -a "$(id -u)" = '0' ]; then
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

cp $lspipelinecfgfile $pipelinedir

if [ -f $servicecfgfile ]; then
  # after release 0.9.5, load service and member configs
  . $servicecfgfile
  . $membercfgfile

  # overwrite the logstash.yml file
  # https://www.elastic.co/guide/en/logstash/5.6/production.html
  echo "path.data: /data/logstash" > $cfgdir/logstash.yml
  echo "path.config: /usr/share/logstash/pipeline" >> $cfgdir/logstash.yml
  echo "node.name: $MEMBER_NAME" >> $cfgdir/logstash.yml
  echo "http.host: $BIND_IP" >> $cfgdir/logstash.yml
  echo "http.port: 9600" >> $cfgdir/logstash.yml
  echo "queue.type: $QUEUE_TYPE" >> $cfgdir/logstash.yml
  echo "dead_letter_queue.enable: $ENABLE_DEAD_LETTER_QUEUE" >> $cfgdir/logstash.yml
  # configure pipeline
  if [ $PIPELINE_WORKERS -gt 0 ]; then
    echo "pipeline.workers: $PIPELINE_WORKERS" >> $cfgdir/logstash.yml
  fi
  if [ $PIPELINE_OUTPUT_WORKERS -gt 0 ]; then
    echo "pipeline.output.workers: $PIPELINE_OUTPUT_WORKERS" >> $cfgdir/logstash.yml
  fi
  if [ $PIPELINE_BATCH_SIZE -gt 0 ]; then
    echo "pipeline.batch.size: $PIPELINE_BATCH_SIZE" >> $cfgdir/logstash.yml
  fi
  if [ $PIPELINE_BATCH_DELAY -gt 0 ]; then
    echo "pipeline.batch.delay: $PIPELINE_BATCH_DELAY" >> $cfgdir/logstash.yml
  fi
  # disable xpack
  echo "xpack.monitoring.enabled: false" >> $cfgdir/logstash.yml

  # update jvm.options file
  # Unlike ElasticSearch, if LS_JAVA_OPTS is set, it will overwrite the default opts in jvm.options.
  # see /usr/share/logstash/logstash.lib.sh. Still remove it to keep consistent with ElasticSearch.
  sed -i '/-Xms/d' $cfgdir/jvm.options
  sed -i '/-Xmx/d' $cfgdir/jvm.options
  # set heap dump path to the external data volume.
  sed -i 's/#-XX:HeapDumpPath=.*/-XX:HeapDumpPath=\/data\/heapdump.hprof/g' $cfgdir/jvm.options
  # enable gc detail log with log file rotation.
  sed -i 's/#-XX:+PrintGCDetails/-XX:+PrintGCDetails/g' $cfgdir/jvm.options
  sed -i 's/#-XX:+PrintGCTimeStamps/-XX:+PrintGCTimeStamps/g' $cfgdir/jvm.options
  sed -i 's/#-XX:+PrintGCApplicationStoppedTime/-XX:+PrintGCApplicationStoppedTime/g' $cfgdir/jvm.options
  sed -i 's/#-Xloggc:.*/-Xloggc:\/data\/lsgc-%t.log/g' $cfgdir/jvm.options
  echo "" >> $cfgdir/jvm.options
  echo "-XX:+UseGCLogFileRotation" >> $cfgdir/jvm.options
  echo "-XX:NumberOfGCLogFiles=8" >> $cfgdir/jvm.options
  echo "-XX:GCLogFileSize=64M" >> $cfgdir/jvm.options

  # set heap size
  export LS_JAVA_OPTS="-Xms${HEAP_SIZE_MB}m -Xmx${HEAP_SIZE_MB}m"

else
  # before release 0.9.6
  cp $lsymlfile $cfgdir
  cp $jvmcfgfile $cfgdir
  . $syscfgfile
fi

echo $SERVICE_MEMBER

echo "$@"
exec "$@"
