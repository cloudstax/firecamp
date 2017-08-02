#!/bin/bash
set -e

datadir=/data
confdir=/data/conf
cfgfile=$confdir/zoo.cfg
myidfile=$confdir/myid
syscfgfile=$confdir/sys.conf

export ZOOCFGDIR=$confdir

# sanity check to make sure the volume is mounted to /data.
if [ ! -d "$datadir" ]; then
  echo "error: $datadir not exist. Please make sure the volume is mounted to $datadir." >&2
  exit 1
fi
if [ ! -d "$confdir" ]; then
  echo "error: $confdir not exist." >&2
  exit 1
fi
# sanity check to make sure the service config file is created.
if [ ! -f "$cfgfile" ]; then
  echo "error: $cfgfile not exist." >&2
  exit 1
fi
if [ ! -f "$myidfile" ]; then
  echo "error: $myidfile not exist." >&2
  exit 1
fi
# sanity check to make sure the sys config file is created.
if [ ! -f "$syscfgfile" ]; then
  echo "error: $syscfgfile not exist." >&2
  exit 1
fi

# allow the container to be started with `--user`
if [ "$1" = 'zkServer.sh' -a "$(id -u)" = '0' ]; then
  datadiruser=$(stat -c "%U" $datadir)
  if [ "$datadiruser" != "$ZOO_USER" ]; then
    chown -R "$ZOO_USER" "$datadir"
  fi
  chown -R "$ZOO_USER" "$confdir"

  exec su-exec "$ZOO_USER" "$BASH_SOURCE" "$@"
fi

# copy myid file if it doesn't exist
if [ ! -f "$datadir/myid" ]; then
  cp $myidfile $datadir/myid
fi

# load the sys config file
. $syscfgfile
echo $SERVICE_MEMBER
echo ""

echo "$@"
exec "$@"
