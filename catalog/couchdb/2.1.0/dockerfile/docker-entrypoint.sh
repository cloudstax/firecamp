#!/bin/bash
set -e

rootdir=/data
datadir=/data/couchdb
confdir=/data/conf
localinifile=$confdir/local.ini
vmargsfile=$confdir/vm.args

syscfgfile=$confdir/sys.conf

USER=couchdb

export ERL_FLAGS="-couch_ini /opt/couchdb/etc/default.ini $localinifile"

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
if [ ! -f "$localinifile" ]; then
  echo "error: $localinifile not exist." >&2
  exit 1
fi
if [ ! -f "$vmargsfile" ]; then
  echo "error: $vmargsfile not exist." >&2
  exit 1
fi
if [ ! -f "$syscfgfile" ]; then
  echo "error: $syscfgfile not exist." >&2
  exit 1
fi

# allow the container to be started with `--user`
if [ "$1" = '/opt/couchdb/bin/couchdb' -a "$(id -u)" = '0' ]; then
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

  exec gosu "$USER" "$BASH_SOURCE" "$@"
fi

# add configuration files
cp $vmargsfile /opt/couchdb/etc/

# load the sys config file
. $syscfgfile
echo $SERVICE_MEMBER

echo "$@"
exec "$@"
