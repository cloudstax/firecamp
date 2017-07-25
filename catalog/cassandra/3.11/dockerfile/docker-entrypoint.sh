#!/bin/bash
set -e

DATA_DIR=/data
CONFIG_DIR=$DATA_DIR/conf
CASSANDRA_YAML_FILE=$CONFIG_DIR/cassandra.yaml
CASSANDRA_RACKDC_FILE=$CONFIG_DIR/cassandra-rackdc.properties
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
if [ ! -f "$CASSANDRA_YAML_FILE" ]; then
  echo "error: $CASSANDRA_YAML_FILE not exist." >&2
  exit 1
fi
if [ ! -f "$CASSANDRA_RACKDC_FILE" ]; then
  echo "error: $CASSANDRA_RACKDC_FILE not exist." >&2
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
fi

# allow the container to be started with `--user`
if [ "$1" = 'cassandra' -a "$(id -u)" = '0' ]; then
	chown -R cassandra $DATA_DIR
	exec gosu cassandra "$BASH_SOURCE" "$@"
fi

# print out the sys config file
cat $syscfgfile
echo ""

echo "$@"
exec "$@"
