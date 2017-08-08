#!/bin/bash
set -e

rootdir=/data
datadir=/data/redis
confdir=/data/conf
cfgfile=$confdir/redis.conf
syscfgfile=$confdir/sys.conf

REDIS_USER=redis

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


# allow the container to be started with `--user`
if [ "$1" = 'redis-server' -a "$(id -u)" = '0' ]; then
  rootdiruser=$(stat -c "%U" $rootdir)
  if [ "$rootdiruser" != "$REDIS_USER" ]; then
    echo "chown -R $REDIS_USER $rootdir"
    chown -R "$REDIS_USER" "$rootdir"
  fi
  diruser=$(stat -c "%U" $datadir)
  if [ "$diruser" != "$REDIS_USER" ]; then
    chown -R "$REDIS_USER" "$datadir"
  fi
  chown -R "$REDIS_USER" "$confdir"

  exec gosu "$REDIS_USER" "$BASH_SOURCE" "$@"
fi

# load the sys config file
. $syscfgfile
echo $SERVICE_MEMBER
echo ""

echo "$@"
exec "$@"
