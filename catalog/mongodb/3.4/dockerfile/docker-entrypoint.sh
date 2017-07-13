#!/bin/bash
set -e

datadir=/data
cfgfile=$datadir/conf/mongod.conf
dbdir=$datadir/db
configdbdir=$datadir/configdb

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

# create the db data and config data dirs.
if [ ! -d "$dbdir" ]; then
  mkdir $dbdir
fi

if [ ! -d "$configdbdir" ]; then
  mkdir $configdbdir
fi

echo "$dbdir $configdbdir and $cfgfile exist"

# start the log rotate job in the background
if [ "$(id -u)" = '0' ]; then
  /startlogrotate.sh &
fi

# allow the container to be started with `--user`
if [ "$1" = 'mongod' -a "$(id -u)" = '0' ]; then
  chown -R mongodb $datadir
  echo "gosu mongodb $BASH_SOURCE $@"
  exec gosu mongodb "$BASH_SOURCE" "$@"
fi

if [ "$1" = 'mongod' ]; then
  numa='numactl --interleave=all'
  if $numa true &> /dev/null; then
    echo "set -- $numa $@"
    set -- $numa "$@"
  fi
fi

echo "$@"
exec "$@"
