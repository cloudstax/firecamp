#!/bin/bash
set -e

# the root db dir
PGDIR=/data
# the db data dir
export PGDATA=$PGDIR/db
PGJournalDir=/journal
# the target configs, these files will be copied to $PGDATA after initdb at primary or basebackup at standby
PGConfDIR=$PGDIR/conf

# after release 0.9.5, these files are not created any more.
syscfgfile=$PGConfDIR/sys.conf
PGConf=$PGConfDIR/postgresql.conf
PGHbaConf=$PGConfDIR/pg_hba.conf

ServiceConfigFile=$PGConfDIR/service.conf
MemberConfigFile=$PGConfDIR/member.conf
PGRecoveryConf=$PGConfDIR/recovery.conf
PrimaryPGConf=$PGConfDIR/primary_postgresql.conf
StandbyPGConf=$PGConfDIR/standby_postgresql.conf
PrimaryPGHbaConf=$PGConfDIR/primary_pg_hba.conf
StandbyPGHbaConf=$PGConfDIR/standby_pg_hba.conf

PGUSER="postgres"
ROLE_PRIMARY="primary"
ROLE_STANDBY="standby"


SanityCheck() {
  # sanity check to make sure the volume is mounted to $PGDIR.
  if [ ! -d "$PGDIR" ]; then
    echo "error: $PGDIR not exist. Please make sure the volume is mounted to $PGDIR." >&2
    exit 1
  fi
  if [ ! -d "$PGJournalDir" ]; then
    echo "error: $PGJournalDir not exist. Please make sure the journal volume is mounted to $PGJournalDir." >&2
    exit 1
  fi

  # sanity check to make sure the config file is created.
  if [ ! -f "$ServiceConfigFile" ]; then
    echo "error: $ServiceConfigFile not exist." >&2
    exit 1
  fi
}

InitPrimaryDB() {
  echo "PostgreSQL primary init process start"

  # initialize db
  eval "initdb -U $PGUSER -D $PGDATA"
  echo "PostgreSQL primary initdb completes, initdb -U $PGUSER -D $PGDATA"

  if [ -f "$MemberConfigFile" ]; then
    # after release 0.9.5
    # copy over the config files
    cp $PrimaryPGConf $PGDATA/postgresql.conf
    cp $PrimaryPGHbaConf $PGDATA/pg_hba.conf

    # update listen_addresses in postgresql.conf
    sed -i 's/localhost/'$BIND_IP'/g' $PGDATA/postgresql.conf
    # update replication user in pg_hba.conf
    sed -i 's/defaultReplUser/'$REPLICATION_USER'/g' $PGDATA/pg_hba.conf
  else
    # before release 0.9.6
    # copy over the config files
    cp $PGConf $PGDATA/
    cp $PGHbaConf $PGDATA/
  fi

  # internal start of server to create replication user
  pg_ctl -D "$PGDATA" -o "-c listen_addresses='localhost'" -w start

  # The streaming replication is used. Could not use replication slot. If the standby goes down
  # for a long time, the primary xlog will become full. The postgres db will be down.
  # To tolerate a zone failure, the primary and standby would usually be deployed to 2 zones.
  # If one AWS zone goes down for a while, the standby will be down as well.
  # TODO archive to S3.

  # create replication user
  # if local auth method is not set as "trust" in pg_hba.conf, has to call with PGPASSWORD,
  # such as PGPASSWORD=pass1234 psql -v ...
  psql -v ON_ERROR_STOP=1 -U "$PGUSER" -c "CREATE ROLE "$REPLICATION_USER" WITH REPLICATION PASSWORD '$REPLICATION_PASSWORD' LOGIN"
  echo "PostgreSQL replication user created, $REPLICATION_USER"

  # set "postgres" user password for the remote login
  psql -U "$PGUSER" -c "ALTER USER $PGUSER WITH PASSWORD '$POSTGRES_PASSWORD'"

  # stop postgres
  pg_ctl -D "$PGDATA" -m fast -w stop

  echo "PostgreSQL primary init process complete; ready for start up."
}

InitStandbyDB() {
  echo "PostgreSQL standby init process start"

  # create the .pgpass file under /home/postgres for the "postgres" user,
  # so pg_basebackup does not require a password prompt.
  # The .pgpass file contains lines of the following format:
  #   hostname:port:database:username:password
  echo "$PRIMARY_HOST:5432:replication:$REPLICATION_USER:$REPLICATION_PASSWORD" > ~/.pgpass
  chmod 600 ~/.pgpass
  echo "PostgreSQL created ~/.pgpass file"

  pg_basebackup -h "$PRIMARY_HOST" -D "$PGDATA" -P -U "$REPLICATION_USER" --xlog-method=stream -w

  if [ -f "$MemberConfigFile" ]; then
    # after release 0.9.5
    # copy over the config files
    cp $StandbyPGConf $PGDATA/postgresql.conf
    cp $StandbyPGHbaConf $PGDATA/pg_hba.conf
    cp $PGRecoveryConf $PGDATA/

    # update listen_addresses in postgresql.conf
    sed -i 's/localhost/'$BIND_IP'/g' $PGDATA/postgresql.conf
  else
    # before release 0.9.6
    # copy over the config files
    cp $PGConf $PGDATA/
    cp $PGHbaConf $PGDATA/
    cp $PGRecoveryConf $PGDATA/
  fi

  echo "PostgreSQL standby pg_basebackup from primary complete; ready for start up."
}


SanityCheck

# allow the container to be started with `--user`
if [ "$(id -u)" = '0' ]; then
  journaldiruser=$(stat -c "%U" $PGJournalDir)
  if [ "$journaldiruser" != "$PGUSER" ]; then
    echo "chown -R $PGUSER $PGJournalDir"
    chown -R $PGUSER "$PGJournalDir"
  fi
  pgdiruser=$(stat -c "%U" $PGDIR)
  if [ "$pgdiruser" != "$PGUSER" ]; then
    echo "chown -R $PGUSER $PGDIR"
    chown -R $PGUSER "$PGDIR"
  fi
  chown -R $PGUSER "$PGConfDIR"

  mkdir -p /var/run/postgresql
  chown -R postgres /var/run/postgresql
  chmod g+s /var/run/postgresql

  exec gosu postgres "$BASH_SOURCE" "$@"
fi

# load the configs from the config file, including the container role (primary or slave),
# primary hostname, postgres password, replication user & password.
. $ServiceConfigFile

if [ -f "$MemberConfigFile" ]; then
  # after release 0.9.5
  . $MemberConfigFile
else
  # before release 0.9.5
  . $syscfgfile
fi

# check all required configs are loaded
if [ -z "$CONTAINER_ROLE" ] || [ -z "$PRIMARY_HOST" ]
then
  echo "error: please write all required configs in the service and member config files." >&2
  exit 1
fi


# load the sys config file
echo $SERVICE_MEMBER
echo "primary host $PRIMARY_HOST"
# wait for dns update
/waitdns.sh $SERVICE_MEMBER
/waitdns.sh $PRIMARY_HOST
echo ""


# if PG_VERSION file does not exist, db is not initialized
if [ ! -s "$PGDATA/PG_VERSION" ]; then
  if [ -z "$POSTGRES_PASSWORD" ] || [ -z "$REPLICATION_USER" ] || [ -z "$REPLICATION_PASSWORD" ]
  then
    echo "error: please include password and repliation user/password in the config file $ServiceConfigFile." >&2
    exit 1
  fi

  if [ "$CONTAINER_ROLE" = "$ROLE_PRIMARY" ]; then
    InitPrimaryDB
  else
    InitStandbyDB
  fi
fi

if [ ! -L $PGDATA/pg_xlog ]; then
  # use separate disk for PG WAL logs.
  mv $PGDATA/pg_xlog/* $PGJournalDir
  rm -fr $PGDATA/pg_xlog
  ln -s $PGJournalDir $PGDATA/pg_xlog
fi


# Currently the PG conf files will not be changed once created.
# So no need to copy over the conf files to $PGDATA

echo "$@"
exec "$@"
