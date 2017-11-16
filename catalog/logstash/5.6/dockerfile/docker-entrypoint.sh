#!/bin/bash
set -ex

rootdir=/data
datadir=/data/logstash
confdir=/data/conf

cfgfile=$confdir/logstash.yml
jvmcfgfile=$confdir/jvm.options
lscfgfile=$confdir/logstash.conf
syscfgfile=$confdir/sys.conf

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
if [ ! -f "$cfgfile" ]; then
  echo "error: $cfgfile not exist." >&2
  exit 1
fi
if [ ! -f "$lscfgfile" ]; then
  echo "error: $lscfgfile not exist." >&2
  exit 1
fi
if [ ! -f "$jvmcfgfile" ]; then
  echo "error: $jvmcfgfile not exist." >&2
  exit 1
fi
if [ ! -f "$syscfgfile" ]; then
  echo "error: $syscfgfile not exist." >&2
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

cp $cfgfile $cfgdir
cp $jvmcfgfile $cfgdir
cp $lscfgfile $pipelinedir

# load the sys config file
. $syscfgfile
echo $SERVICE_MEMBER

echo "$@"
exec "$@"
