#!/bin/bash
set -e

datadir=/data
cfgfile=$datadir/conf/mongod.conf
dbdir=$datadir/db
configdbdir=$datadir/configdb
syscfgfile=$datadir/conf/sys.conf

# sanity check to make sure the volume is mounted to /data.
if [ ! -d "$datadir" ]; then
  echo "error: $datadir not exist. Please make sure the volume is mounted to $datadir." >&2
  exit 1
fi

# sanity check to make sure the mongodb config file is created.
if [ ! -f "$cfgfile" ]; then
  echo "error: $cfgfile not exist." >&2
  exit 1
fi

# sanity check to make sure the sys config file is created.
if [ ! -f "$syscfgfile" ]; then
  echo "error: $syscfgfile not exist." >&2
  exit 1
fi

# create the db data and config data dirs.
if [ ! -d "$dbdir" ]; then
  mkdir $dbdir
fi

if [ ! -d "$configdbdir" ]; then
  mkdir $configdbdir
fi

# allow the container to be started with `--user`
if [ "$1" = 'mongod' -a "$(id -u)" = '0' ]; then
  chown -R mongodb $datadir
  echo "gosu mongodb $BASH_SOURCE $@"
  exec gosu mongodb "$BASH_SOURCE" "$@"
fi

echo "$dbdir $configdbdir and $cfgfile exist"

# load the sys config file
. $syscfgfile
echo $SERVICE_MEMBER
/checkdns.sh $SERVICE_MEMBER
echo ""

if [ "$1" = 'mongod' ]; then
  numa='numactl --interleave=all'
  if $numa true &> /dev/null; then
    echo "set -- $numa $@"
    set -- $numa "$@"
  fi
fi

echo "$@"
exec "$@"
