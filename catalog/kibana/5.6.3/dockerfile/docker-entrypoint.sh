#!/bin/bash
set -e

rootdir=/data
confdir=/data/conf
syscfgfile=$confdir/sys.conf

kbdatadir=/data/kibana
kbcfgfile=$confdir/kibana.yml
kbcfgdir=/usr/share/kibana/config/

esdatadir=/data/elasticsearch
escfgfile=$confdir/elasticsearch.yml
esjvmcfgfile=$confdir/jvm.options
escfgdir=/usr/share/elasticsearch/config

USER=elasticsearch

# sanity check to make sure the volume is mounted to /data.
if [ ! -d "$rootdir" ]; then
  echo "error: $rootdir not exist. Please make sure the volume is mounted to $rootdir." >&2
  exit 1
fi
if [ ! -d "$kbdatadir" ]; then
  mkdir "$kbdatadir"
fi
if [ ! -d "$esdatadir" ]; then
  mkdir "$esdatadir"
fi
if [ ! -d "$confdir" ]; then
  echo "error: $confdir not exist." >&2
  exit 1
fi
# sanity check to make sure the config files are created.
if [ ! -f "$syscfgfile" ]; then
  echo "error: $syscfgfile not exist." >&2
  exit 1
fi
if [ ! -f "$kbcfgfile" ]; then
  echo "error: $kbcfgfile not exist." >&2
  exit 1
fi
if [ ! -f "$escfgfile" ]; then
  echo "error: $escfgfile not exist." >&2
  exit 1
fi
if [ ! -f "$esjvmcfgfile" ]; then
  echo "error: $esjvmcfgfile not exist." >&2
  exit 1
fi

# allow the container to be started with `--user`
if [ "$(id -u)" = '0' ]; then
  rootdiruser=$(stat -c "%U" $rootdir)
  if [ "$rootdiruser" != "$USER" ]; then
    echo "chown -R $USER $rootdir"
    chown -R "$USER" "$rootdir"
  fi
  diruser=$(stat -c "%U" $kbdatadir)
  if [ "$diruser" != "$USER" ]; then
    chown -R "$USER" "$kbdatadir"
  fi
  diruser=$(stat -c "%U" $esdatadir)
  if [ "$diruser" != "$USER" ]; then
    chown -R "$USER" "$esdatadir"
  fi
  chown -R "$USER" "$confdir"

  cp $escfgfile $escfgdir
  cp $esjvmcfgfile $escfgdir
  cp $kbcfgfile $kbcfgdir

  exec gosu "$USER" "$BASH_SOURCE" "$@"
fi

# load the sys config file
. $syscfgfile
echo $SERVICE_MEMBER

# run elasticsearch in the background
echo "start elasticsearch in the coordinating mode"
elasticsearch -d

echo "start kibana"
exec kibana

test() {
while true; do
  ps aux | grep elasticsearch | grep -q -v grep
  if [ "$?" != "0" ]; then
    echo "elasticsearch process exited"
    ps ax | grep elasticsearch
    exit 2
  fi

  ps aux | grep kibana | grep -q -v grep
  if [ "$?" != "0" ]; then
    echo "kibana process exited"
    ps ax | grep kibana
    exit 2
  fi

  sleep 10
done
}
