#!/bin/bash

# This script configures the authentication for Cassandra service.
# It follows http://cassandra.apache.org/doc/latest/operating/security.html

#export REGION="us-west-1"
#export CLUSTER="cluster1"
#export MANAGE_SERVER_URL="firecamp-manageserver.cluster1-firecamp.com:27040"
#export OP="Catalog-Set-Service-Init"
#export SERVICE_TYPE="cassandra"
#export SERVICE_NAME="mycas"
#export SERVICE_NODE="mycas-0.cluster1-firecamp.com"


# check the environment parameters
if [ -z "$REGION" ] || [ -z "$CLUSTER" ] || [ -z "$MANAGE_SERVER_URL" ] || [ -z "$OP" ] || [ -z "$SERVICE_NAME" ] ||  [ -z "$SERVICE_TYPE" ] || [ -z "$SERVICE_NODE" ]
then
  echo "error: please pass all required environment variables" >&2
  exit 1
fi

# wait till the service node dns is ready
/waitdns.sh $SERVICE_NODE

echo "alter keyspace system_auth at $REGION to 3 replicas for service $SERVICE_NAME"

# increase the replication factor for the system_auth keyspace to 3
cqlsh $SERVICE_NODE -u cassandra -p cassandra -e "ALTER KEYSPACE system_auth WITH REPLICATION = {'class' : 'NetworkTopologyStrategy', '$REGION' : 3};"
if [ "$?" != "0" ]; then
  echo "cqlsh alter system_auth keyspace error" >&2
  sleep 5
  exit 2
fi

echo "alter keyspace succeeded, set cassandra service initialized"

# Note there is no need to run "nodetool repair system_auth". The 3.11 Cassandra could automatically redistribute it.
# "nodetool getendpoints system_auth roles cassandra" shows the key is replicated to 3 nodes.
# http://cassandra.apache.org/doc/latest/operating/security.html does not suggest to run repair.
# http://docs.datastax.com/en/cassandra/3.0/cassandra/configuration/secureConfigNativeAuth.html suggests to run repair.
#nodetool -h $SERVICE_NODE -u cassandra -pw cassandra repair system_auth

# notify the manage server about the initialization done
data="{\"Region\":\"$REGION\",\"Cluster\":\"$CLUSTER\",\"ServiceName\":\"$SERVICE_NAME\",\"ServiceType\":\"$SERVICE_TYPE\"}"
curl -X PUT -H "Content-Type: application/json" -d $data "$MANAGE_SERVER_URL/$OP"

