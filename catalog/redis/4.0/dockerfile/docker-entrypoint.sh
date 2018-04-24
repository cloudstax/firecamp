#!/bin/bash
set -e

rootdir=/data
datadir=/data/redis
confdir=/data/conf

# after release 0.9.5, sys.conf is not created any more.
syscfgfile=$confdir/sys.conf

servicecfgfile=$confdir/service.conf
membercfgfile=$confdir/member.conf
staticipcfgfile=$confdir/staticip.conf
rediscfgfile=$confdir/redis.conf

REDIS_USER=redis

redisetcdir=/etc/redis

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

if [ ! -d "$redisetcdir" ]; then
  mkdir "$redisetcdir"
fi

# allow the container to be started with `--user`
if [ "$1" = 'redis-server' -a "$(id -u)" = '0' ]; then
  # copy redis.conf to /etc/redis
  cp $rediscfgfile $redisetcdir
  chown -R $REDIS_USER $redisetcdir

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
  sed -i 's/bind 0.0.0.0/bind '$BIND_IP'/g' $redisetcdir/redis.conf
  maxMem=`expr $MEMORY_CACHE_MB \* 1024 \* 1024`
  sed -i 's/maxmemory 268435456/maxmemory '$maxMem'/g' $redisetcdir/redis.conf
  sed -i 's/maxmemory-policy allkeys-lru/maxmemory-policy '$MAX_MEMORY_POLICY'/g' $redisetcdir/redis.conf
  if [ $REPL_TIMEOUT_SECS -gt 0 ]; then
    sed -i 's/repl-timeout 60/repl-timeout '$REPL_TIMEOUT_SECS'/g' $redisetcdir/redis.conf
  fi
  sed -i 's/rename-command CONFIG \"\"/rename-command CONFIG \"'$CONFIG_CMD_NAME'\"/g' $redisetcdir/redis.conf

  if [ -n "$AUTH_PASS" ]; then
    # enable auth
    echo "requirepass $AUTH_PASS" >> $redisetcdir/redis.conf
    echo "masterauth $AUTH_PASS" >> $redisetcdir/redis.conf
  fi

  if [ "$DISABLE_AOF" = "false" ]; then
    # enable AOF
    echo "appendonly yes" >> $redisetcdir/redis.conf
  fi

  if [ $SHARDS -eq 2 ]; then
    echo "the number of shards could not be 2" >&2
    exit 1
  fi

  if [ $SHARDS -eq 1 ]; then
    # master-slave mode or single instance
    if [ $REPLICAS_PERSHARD -gt 1 -o $MEMBER_INDEX_INSHARD -gt 0 ]; then
      # master-slave mode, set the master dnsname for the slave
      echo "slaveof $MASTER_MEMBER 6379" >> $redisetcdir/redis.conf
    fi
  else
    # cluster mode
    echo "cluster-enabled yes" >> $redisetcdir/redis.conf
    echo "cluster-config-file /data/redis-node.conf" >> $redisetcdir/redis.conf
    echo "cluster-announce-ip $STATIC_IP" >> $redisetcdir/redis.conf
    echo "cluster-node-timeout 15000" >> $redisetcdir/redis.conf
    echo "cluster-slave-validity-factor 10" >> $redisetcdir/redis.conf
    echo "cluster-migration-barrier 1" >> $redisetcdir/redis.conf
    echo "cluster-require-full-coverage yes" >> $redisetcdir/redis.conf
  fi

else
  # load the sys config file
  . $syscfgfile
fi

echo $SERVICE_MEMBER

echo "$@"
exec "$@"
