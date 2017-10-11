#!/bin/bash
set -ex

rootdir=/data
datadir=/data/elasticsearch
confdir=/data/conf
cfgfile=$confdir/elasticsearch.yml
jvmcfgfile=$confdir/jvm.options
syscfgfile=$confdir/sys.conf
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
if [ ! -f "$cfgfile" ]; then
  echo "error: $cfgfile not exist." >&2
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

cp $cfgfile $escfgdir
cp $jvmcfgfile $escfgdir

# load the sys config file
. $syscfgfile
echo $SERVICE_MEMBER

/waitdns.sh $SERVICE_MEMBER
# check service member dns name again. if lookup fails, the script will exit.
host $SERVICE_MEMBER

echo "$@"
exec "$@"
