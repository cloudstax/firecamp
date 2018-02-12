#!/bin/bash
set -e

datadir=/data
dbdir=$datadir/db
dbjournallinkdir=$datadir/db/journal
journaldir=/journal
confdir=$datadir/conf
cfgfile=$confdir/mongod.conf
syscfgfile=$confdir/sys.conf

# sanity check to make sure the volume is mounted to /data.
if [ ! -d "$datadir" ]; then
  echo "error: $datadir not exist. Please make sure the volume is mounted to $datadir." >&2
  exit 1
fi
if [ ! -d "$confdir" ]; then
  echo "error: $confdir not exist." >&2
  exit 1
fi
if [ ! -d "$journaldir" ]; then
  echo "error: $journaldir not exist." >&2
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

# allow the container to be started with `--user`
if [ "$1" = 'mongod' -a "$(id -u)" = '0' ]; then
  journaldiruser=$(stat -c "%U" $journaldir)
  if [ "$journaldiruser" != "mongodb" ]; then
    chown -R mongodb $journaldir
  fi
  datadiruser=$(stat -c "%U" $datadir)
  if [ "$datadiruser" != "mongodb" ]; then
    chown -R mongodb $datadir
  fi
  # the mongodb init will recreate mongod.conf, chown to mongodb
  chown -R mongodb $confdir

  echo "gosu mongodb $BASH_SOURCE $@"
  exec gosu mongodb "$BASH_SOURCE" "$@"
fi

# symlink mongodb journal directory
# https://docs.mongodb.com/manual/core/journaling/#journal-directory
if [ ! -L $dbjournallinkdir ]; then
  ln -s $journaldir $dbjournallinkdir
fi

# load the sys config file
. $syscfgfile
# When creating the sharded cluster with 3 config servers, occasionally host $SERVICE_MEMBER
# returns 127.0.0.1 for some config server container. Not know the root cause.
# MongoDB config server will then listen on localhost via unix socket, and the config server
# replicaset may not be initialized.
#
# netstat -al | grep 27019
# tcp        0      0 localhost:27019             *:*                         LISTEN
# unix  2      [ ACC ]     STREAM     LISTENING     238639 /tmp/mongodb-27019.sock
host $SERVICE_MEMBER
memberhost=$(host $SERVICE_MEMBER | awk '{ print $4 }')
if [ "$memberhost" == "127.0.0.1" ]; then
  echo "$SERVICE_MEMBER DNS lookup gets 127.0.0.1, exit"
  exit 2
fi
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
