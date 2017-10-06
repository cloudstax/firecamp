#!/bin/dumb-init /bin/sh
set -e

# Note above that we run dumb-init as PID 1 in order to reap zombie processes
# as well as forward signals to all processes in its session. Normally, sh
# wouldn't do either of these functions so we'd leak zombies as well as do
# unclean termination of all our sub-processes.

rootdir=/data
datadir=/data/consul
confdir=/data/conf
syscfgfile=$confdir/sys.conf
basiccfgfile=$confdir/basic_config.json

USER=consul

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
if [ ! -f "$syscfgfile" ]; then
  echo "error: $syscfgfile not exist." >&2
  exit 1
fi
if [ ! -f "$basiccfgfile" ]; then
  echo "error: $basiccfgfile not exist." >&2
  exit 1
fi

# If we are running Consul, make sure it executes as the proper user.
if [ "$1" = 'consul' -a "$(id -u)" = '0' ]; then
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

  set -- gosu consul "$@"
fi

# load the sys config file
. $syscfgfile
echo $SERVICE_MEMBER

echo "$@"
exec "$@"
