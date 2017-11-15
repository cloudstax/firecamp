#!/bin/bash
set -e

datadir=/data
dbdir=$datadir/db
dbjournallinkdir=$datadir/db/journal
journaldir=/journal
configdbdir=$datadir/configdb
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

if [ ! -d "$configdbdir" ]; then
  mkdir $configdbdir
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
echo $SERVICE_MEMBER
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
