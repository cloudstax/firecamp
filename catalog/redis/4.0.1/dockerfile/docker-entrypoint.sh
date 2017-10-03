#!/bin/bash
set -e

rootdir=/data
datadir=/data/redis
confdir=/data/conf
cfgfile=$confdir/redis.conf
syscfgfile=$confdir/sys.conf
clustercfgfile=$confdir/cluster.info
redisnodefile=$rootdir/redis-node.conf

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

if [ "$(id -u)" = '0' ]; then
  # load the sys config file
  . $syscfgfile

  if [ "$PLATFORM" = "swarm" ]; then
    # some platform, such as docker swarm, does not allow not using host network for service.
    # append the SERVICE_MEMBER to the last line of /etc/hosts, so the service could bind the
    # member name and serve correctly.
    echo "$(cat /etc/hosts) $SERVICE_MEMBER" > /etc/hosts
  fi
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

/waitdns.sh $SERVICE_MEMBER
# check service member dns name again. if lookup fails, the script will exit.
host $SERVICE_MEMBER

if [ -f "$clustercfgfile" -a "$PLATFORM" = "swarm" ]; then
  # cluster is already initialized, check and update the master's address
  /redis-node.sh $clustercfgfile $redisnodefile
  if [ "$?" != "0" ]; then
    echo "update redis routing ip failed"
    exit 2
  fi
fi

echo "$@"
exec "$@"
