#!/bin/bash
set -e

DATA_DIR=/data
COMMITLOG_DIR=/journal
CONFIG_DIR=$DATA_DIR/conf

# after release 0.9.5, cassandra.yaml, rackdc.properties and jvm.options are not created any more.
# instead, we will read the configs from service.conf and sys.conf, and update the default files.
CASSANDRA_YAML_FILE=$CONFIG_DIR/cassandra.yaml
CASSANDRA_RACKDC_FILE=$CONFIG_DIR/cassandra-rackdc.properties
CASSANDRA_JVM_FILE=$CONFIG_DIR/jvm.options

CASSANDRA_LOG_FILE=$CONFIG_DIR/logback.xml
CASSANDRA_JMXREMOTEPASSWD_FILE=$CONFIG_DIR/jmxremote.password
syscfgfile=$CONFIG_DIR/sys.conf
servicecfgfile=$CONFIG_DIR/service.conf

# Sanity check to make sure the volume is mounted to /data and /journal.
# If volume is not mounted for any reason, exit. This ensures data will not be written
# to the local directory, and get lost when the container moves to another node.
if [ ! -d "$DATA_DIR" ]; then
  echo "error: $DATA_DIR not exist. Please make sure the volume is mounted to $DATA_DIR." >&2
  exit 1
fi
if [ ! -d "$COMMITLOG_DIR" ]; then
  echo "error: $COMMITLOG_DIR not exist." >&2
  exit 1
fi

# sanity check to make sure the db config files are created.
if [ ! -d "$CONFIG_DIR" ]; then
  echo "error: $CONFIG_DIR not exist." >&2
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
  if [ -f "$CASSANDRA_YAML_FILE" ]; then
    cp $CASSANDRA_YAML_FILE /etc/cassandra/
    cp $CASSANDRA_RACKDC_FILE /etc/cassandra/
  fi
  # jvm.options file does not exist before release 0.9.1
  if [ -f "$CASSANDRA_JVM_FILE" ]; then
    cp $CASSANDRA_JVM_FILE /etc/cassandra/
  fi

  cp $CASSANDRA_LOG_FILE /etc/cassandra/
  # jmxremote.password file does not exist before release 0.9.2
  if [ -f "$CASSANDRA_JMXREMOTEPASSWD_FILE" ]; then
    export LOCAL_JMX="no"
    cp $CASSANDRA_JMXREMOTEPASSWD_FILE /etc/cassandra/
  fi

  # after 0.9.5, replace the fields in the default cassandra config files.
  if [ -f "$servicecfgfile" ]; then
    # source the sys and service config files
    . $syscfgfile
    . $servicecfgfile

    # replace the configs in cassandra.yaml
    DefaultYaml="/etc/cassandra/cassandra.yaml"
    sed -ri 's/(cluster_name:).*/\1 '\'$CLUSTER\''/' $DefaultYaml

    sed -i 's/# hints_directory:.*/hints_directory: \/data\/hints/g' $DefaultYaml
    sed -i 's/- \/var\/lib\/cassandra\/data/- \/data\/data/g' $DefaultYaml
    sed -i 's/commitlog_directory:.*/commitlog_directory: \/journal/g' $DefaultYaml
    sed -i 's/saved_caches_directory:.*/saved_caches_directory: \/data\/saved_caches/g' $DefaultYaml

    sed -ri 's/(- seeds:).*/\1 "'"$CASSANDRA_SEEDS"'"/' $DefaultYaml

    ListenAddr=$SERVICE_MEMBER
    if [ "$PLATFORM" = "swarm" ]; then
      ListenAddr=""
    fi
    sed -i 's/listen_address:.*/listen_address: '$ListenAddr'/g' $DefaultYaml
    sed -i 's/broadcast_address:.*/broadcast_address: '$SERVICE_MEMBER'/g' $DefaultYaml
    RpcAddr=$SERVICE_MEMBER
    if [ "$PLATFORM" = "swarm" ]; then
      RpcAddr="0.0.0.0"
    fi
    sed -i 's/rpc_address:.*/rpc_address: '$RpcAddr'/g' $DefaultYaml
    sed -i 's/broadcast_rpc_address:.*/broadcast_rpc_address: '$SERVICE_MEMBER'/g' $DefaultYaml

    sed -i 's/endpoint_snitch:.*/endpoint_snitch: GossipingPropertyFileSnitch/g' $DefaultYaml

    sed -i 's/authenticator:.*/authenticator: PasswordAuthenticator/g' $DefaultYaml
    sed -i 's/authorizer:.*/authorizer: CassandraAuthorizer/g' $DefaultYaml

    # replace the configs in cassandra-rackdc.properties
    RackdcFile="/etc/cassandra/cassandra-rackdc.properties"
    sed -i 's/dc=.*/dc='$REGION'/g' $RackdcFile
    sed -i 's/rack=.*/rack='$AVAILABILITY_ZONE'/g' $RackdcFile
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

# source the sys and service config files
. $syscfgfile
if [ -f "$servicecfgfile" ]; then
  . $servicecfgfile
fi
echo $SERVICE_MEMBER
echo "$JVM_OPTS"
echo


# enable jolokia agent
# TODO enable the basic auth, https://jolokia.org/reference/html/agents.html
if [ "$PLATFORM" = "swarm" ]; then
  # docker swarm, does not allow not using host network for service, listen on 0.0.0.0
  export JVM_OPTS="$JVM_OPTS -javaagent:/opt/jolokia-agent/jolokia-jvm-1.5.0-agent.jar=port=8778,host=0.0.0.0"
else
  export JVM_OPTS="$JVM_OPTS -javaagent:/opt/jolokia-agent/jolokia-jvm-1.5.0-agent.jar=port=8778,host=$SERVICE_MEMBER"
fi

echo "$@"
exec "$@"
