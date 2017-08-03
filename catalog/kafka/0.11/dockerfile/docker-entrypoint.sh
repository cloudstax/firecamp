#!/bin/bash
set -e

rootdir=/data
datadir=/data/kafka
confdir=/data/conf
cfgfile=$confdir/server.properties
logcfgfile=$confdir/log4j.properties
javaenvfile=$confdir/java.env
syscfgfile=$confdir/sys.conf

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
if [ ! -f "$cfgfile" ]; then
  echo "error: $cfgfile not exist." >&2
  exit 1
fi
if [ ! -f "$syscfgfile" ]; then
  echo "error: $syscfgfile not exist." >&2
  exit 1
fi
if [ ! -f "$logcfgfile" ]; then
  echo "error: $logcfgfile not exist." >&2
  exit 1
fi
if [ ! -f "$javaenvfile" ]; then
  echo "error: $javaenvfile not exist." >&2
  exit 1
fi

#if [ "$(id -u)" = '0' ]; then
  # increase the max open files
#  ulimit -n 90000
#fi

# allow the container to be started with `--user`
if [ "$1" = 'kafka-server-start.sh' -a "$(id -u)" = '0' ]; then
  rootdiruser=$(stat -c "%U" $rootdir)
  if [ "$rootdiruser" != "$KAFKA_USER" ]; then
    echo "chown -R $KAFKA_USER $rootdir"
    chown -R "$KAFKA_USER" "$rootdir"
  fi
  diruser=$(stat -c "%U" $datadir)
  if [ "$diruser" != "$KAFKA_USER" ]; then
    chown -R "$KAFKA_USER" "$datadir"
  fi
  chown -R "$KAFKA_USER" "$confdir"

  exec su-exec "$KAFKA_USER" "$BASH_SOURCE" "$@"
fi

# copy server.properties and log4j.properties
cp $cfgfile /kafka/config/
cp $logcfgfile /kafka/config/

# load the sys config file
. $syscfgfile
echo $SERVICE_MEMBER
echo ""

# load the java env file
. $javaenvfile
echo $KAFKA_HEAP_OPTS
echo $KAFKA_JVM_PERFORMANCE_OPTS
export KAFKA_HEAP_OPTS=$KAFKA_HEAP_OPTS
export KAFKA_JVM_PERFORMANCE_OPTS=$KAFKA_JVM_PERFORMANCE_OPTS

echo "$@"
exec "$@"
