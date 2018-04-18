#!/bin/bash
set -e

rootdir=/data
datadir=/data/redis
confdir=/data/conf

# after release 0.9.5, redis.conf and sys.conf are not created any more.
# entrypoint will get the configs in service.conf and member.conf, and update the default redis.conf
rediscfgfile=$confdir/redis.conf
syscfgfile=$confdir/sys.conf

servicecfgfile=$confdir/service.conf
membercfgfile=$confdir/member.conf
staticipcfgfile=$confdir/staticip.conf

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

# copy redis.conf to /etc
cp $rediscfgfile /etc/

if [ -f $servicecfgfile ]; then
  # after release 0.9.5
  if [ ! -f $membercfgfile ] || [ ! -f $staticipcfgfile ]; then
    echo "error: $membercfgfile or $staticipcfgfile file not exist." >&2
    exit 2
  fi

  # load service, member and staticip configs
  . $servicecfgfile
  . $membercfgfile
  . $staticipcfgfile

  # update redis.conf
  sed -i 's/bind 0.0.0.0/bind '$BIND_IP'/g' /etc/redis.conf
  maxMem=`expr $MEMORY_CACHE_MB \* 1024 \* 1024`
  sed -i 's/maxmemory 268435456/maxmemory '$maxMem'/g' /etc/redis.conf
  sed -i 's/maxmemory-policy allkeys-lru/maxmemory-policy '$MAX_MEMORY_POLICY'/g' /etc/redis.conf
  if [ $REPL_TIMEOUT_SECS -gt 0 ]; then
    sed -i 's/repl-timeout 60/repl-timeout '$REPL_TIMEOUT_SECS'/g' /etc/redis.conf
  fi
  sed -i 's/rename-command CONFIG \"\"/rename-command CONFIG \"'$CONFIG_CMD_NAME'\"/g' /etc/redis.conf

  if [ -n "$AUTH_PASS" ]; then
    # enable auth
    echo "requirepass $AUTH_PASS" >> /etc/redis.conf
    echo "masterauth $AUTH_PASS" >> /etc/redis.conf
  fi

  if [ "$DISABLE_AOF" = "false" ]; then
    # enable AOF
    echo "appendonly yes" >> /etc/redis.conf
  fi

  if [ $SHARDS -eq 2 ]; then
    echo "the number of shards could not be 2" >&2
    exit 1
  fi

  if [ $SHARDS -eq 1 ]; then
    # master-slave mode or single instance
    if [ $REPLICAS_PERSHARD -gt 1 -o $MEMBER_INDEX_INSHARD -gt 0 ]; then
      # master-slave mode, set the master dnsname for the slave
      echo "slaveof $MASTER_MEMBER 6379" >> /etc/redis.conf
    fi
  else
    # cluster mode
    echo "cluster-enabled yes" >> /etc/redis.conf
    echo "cluster-config-file /data/redis-node.conf" >> /etc/redis.conf
    echo "cluster-announce-ip $STATIC_IP" >> /etc/redis.conf
    echo "cluster-node-timeout 15000" >> /etc/redis.conf
    echo "cluster-slave-validity-factor 10" >> /etc/redis.conf
    echo "cluster-migration-barrier 1" >> /etc/redis.conf
    echo "cluster-require-full-coverage yes" >> /etc/redis.conf
  fi

else
  # load the sys config file
  . $syscfgfile
fi

echo $SERVICE_MEMBER

echo "$@"
exec "$@"
