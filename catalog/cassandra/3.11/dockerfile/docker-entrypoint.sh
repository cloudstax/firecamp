#!/bin/bash
set -e

DATA_DIR=/data
COMMITLOG_DIR=/journal
CONFIG_DIR=$DATA_DIR/conf
CASSANDRA_YAML_FILE=$CONFIG_DIR/cassandra.yaml
CASSANDRA_RACKDC_FILE=$CONFIG_DIR/cassandra-rackdc.properties
CASSANDRA_LOG_FILE=$CONFIG_DIR/logback.xml
CASSANDRA_JVM_FILE=$CONFIG_DIR/jvm.options
CASSANDRA_JMXREMOTEPASSWD_FILE=$CONFIG_DIR/jmxremote.password
syscfgfile=$CONFIG_DIR/sys.conf

# sanity check to make sure the volume is mounted to /data.
if [ ! -d "$DATA_DIR" ]; then
  echo "error: $DATA_DIR not exist. Please make sure the volume is mounted to $DATA_DIR." >&2
  exit 1
fi
# sanity check to make sure the db config files are created.
if [ ! -d "$CONFIG_DIR" ]; then
  echo "error: $CONFIG_DIR not exist." >&2
  exit 1
fi
if [ ! -d "$COMMITLOG_DIR" ]; then
  echo "error: $COMMITLOG_DIR not exist." >&2
  exit 1
fi

if [ ! -f "$CASSANDRA_YAML_FILE" ]; then
  echo "error: $CASSANDRA_YAML_FILE not exist." >&2
  exit 1
fi
if [ ! -f "$CASSANDRA_RACKDC_FILE" ]; then
  echo "error: $CASSANDRA_RACKDC_FILE not exist." >&2
  exit 1
fi
if [ ! -f "$CASSANDRA_LOG_FILE" ]; then
  echo "error: $CASSANDRA_LOG_FILE not exist." >&2
  exit 1
fi

# sanity check to make sure the sys config file is created.
if [ ! -f "$syscfgfile" ]; then
  echo "error: $syscfgfile not exist." >&2
  exit 1
fi

if [ "$(id -u)" = '0' ]; then
  cp $CASSANDRA_YAML_FILE /etc/cassandra/
  cp $CASSANDRA_RACKDC_FILE /etc/cassandra/
  cp $CASSANDRA_LOG_FILE /etc/cassandra/
  # jvm.options file does not exist before release 0.9.1
  if [ -f "$CASSANDRA_JVM_FILE" ]; then
    cp $CASSANDRA_JVM_FILE /etc/cassandra/
  fi
  # jmxremote.password file does not exist before release 0.9.2
  if [ -f "$CASSANDRA_JMXREMOTEPASSWD_FILE" ]; then
    export LOCAL_JMX="no"
    cp $CASSANDRA_JMXREMOTEPASSWD_FILE /etc/cassandra/
  fi
fi


# allow the container to be started with `--user`
if [ "$1" = 'cassandra' -a "$(id -u)" = '0' ]; then
  commitlogdiruser=$(stat -c "%U" $COMMITLOG_DIR)
  if [ "$commitlogdiruser" != "cassandra" ]; then
	  chown -R cassandra $COMMITLOG_DIR
  fi
  datadiruser=$(stat -c "%U" $DATA_DIR)
  if [ "$datadiruser" != "cassandra" ]; then
	  chown -R cassandra $DATA_DIR
  fi
  chown -R cassandra "$CONFIG_DIR"
  chown -R cassandra /etc/cassandra

	exec gosu cassandra "$BASH_SOURCE" "$@"
fi

# source the sys config file
. $syscfgfile
echo $SERVICE_MEMBER
echo

echo "$@"
exec "$@"
