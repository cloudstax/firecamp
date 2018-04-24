#!/bin/bash
set -e

datadir=/data
dbdir=$datadir/db
dbjournallinkdir=$datadir/db/journal
journaldir=/journal
confdir=$datadir/conf

# after release 0.9.5, sys.conf file is not created any more.
# instead service.conf and emember.conf are created for the common service configs
# and the service member configs. The default mongod.conf will be updated with the configs
# in service.conf and member.conf
syscfgfile=$confdir/sys.conf

servicecfgfile=$confdir/service.conf
membercfgfile=$confdir/member.conf
mongodfile=$confdir/mongod.conf
keyfile=$configdir/keyfile

mongoetcdir=/etc/mongo

# sanity check to make sure the data volume and journal volume are mounted to /data and /journal
if [ ! -d "$datadir" ]; then
  echo "error: $datadir not exist. Please make sure the volume is mounted to $datadir." >&2
  exit 1
fi
if [ ! -d "$journaldir" ]; then
  echo "error: $journaldir not exist." >&2
  exit 1
fi

# sanity check to make sure the config directory exists.
if [ ! -d "$confdir" ]; then
  echo "error: $confdir not exist." >&2
  exit 1
fi
# sanity check to make sure the mongodb config file is created.
if [ ! -f "$mongodfile" ]; then
  echo "error: $mongodfile not exist." >&2
  exit 1
fi

# create the db data and config data dirs.
if [ ! -d "$dbdir" ]; then
  mkdir $dbdir
fi

# create the mongod config dir under /etc
if [ ! -d "$mongoetcdir" ]; then
  mkdir $mongoetcdir
fi

# allow the container to be started with `--user`
if [ "$1" = 'mongod' -a "$(id -u)" = '0' ]; then
  cp $mongodfile $mongoetcdir/mongod.conf
  chown -R mongodb $mongoetcdir

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

if [ -f "$servicecfgfile" ]; then
  # after release 0.9.5
  # load service and member config files
  . $servicecfgfile
  . $membercfgfile

  # replace the member specific fields in the default mongod.conf
  # set bind ip
  sed -i 's/bindIp: 0.0.0.0/bindIp: '$BIND_IP'/g' $mongoetcdir/mongod.conf
  # set cluster role for the sharded cluster. do nothing for a single repliaset
  if [ "$CLUSTER_ROLE" = "configsvr" ]; then
    sed -i 's/port: 27017/port: 27019/g' $mongoetcdir/mongod.conf
    echo "sharding:\n  clusterRole: $CLUSTER_ROLE" >> $mongoetcdir/mongod.conf
  elif [ "$CLUSTER_ROLE" = "shardsvr" ]; then
    sed -i 's/port: 27017/port: 27018/g' $mongoetcdir/mongod.conf
    echo "sharding:\n  clusterRole: $CLUSTER_ROLE" >> $mongoetcdir/mongod.conf
  fi
  # set replSetName
  sed -i 's/replSetName: default/replSetName: '$REPLICASET_NAME'/g' $mongoetcdir/mongod.conf
  # enabled security
  if [ "$ENABLE_SECURITY" = "true" ]; then
    echo "security:\n  keyFile: ${keyfile}\n  authorization: enabled" >> $mongoetcdir/mongod.conf
  fi
else
  # before release 0.9.6, load the sys config file
  . $syscfgfile
fi

# symlink mongodb journal directory
# https://docs.mongodb.com/manual/core/journaling/#journal-directory
if [ ! -L $dbjournallinkdir ]; then
  ln -s $journaldir $dbjournallinkdir
fi

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
