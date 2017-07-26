#!/bin/bash

# This script configures the authentication for Cassandra service.
# It follows http://cassandra.apache.org/doc/latest/operating/security.html

#export REGION="us-west-1"
#export CLUSTER="cluster1"
#export MANAGE_SERVER_URL="openmanage-manageserver.cluster1-openmanage.com:27040"
#export OP="Catalog-Set-Service-Init"
#export SERVICE_TYPE="cassandra"
#export SERVICE_NAME="mycas"
#export SERVICE_NODE="mycas-0.cluster1-openmanage.com"


# check the environment parameters
if [ -z "$REGION" ] || [ -z "$CLUSTER" ] || [ -z "$MANAGE_SERVER_URL" ] || [ -z "$SERVICE_NAME" ] ||  [ -z "$SERVICE_TYPE" ] || [ -z "$OP" ] || [ -z "$SERVICE_NODE" ]
then
  echo "error: please pass all required environment variables" >&2
  exit 1
fi


# increase the replication factor for the system_auth keyspace to 3
cqlsh $SERVICE_NODE -u cassandra -p cassandra -e "ALTER KEYSPACE system_auth WITH REPLICATION = {'class' : 'NetworkTopologyStrategy', '$REGION' : 3};"


# notify the manage server about the initialization done
data="{\"Region\":\"$REGION\",\"Cluster\":\"$CLUSTER\",\"ServiceName\":\"$SERVICE_NAME\",\"ServiceType\":\"$SERVICE_TYPE\"}"
curl -X PUT -H "Content-Type: application/json" -d $data "$MANAGE_SERVER_URL/$OP"

# sleep some time for the server to restart all containers
sleep 5

