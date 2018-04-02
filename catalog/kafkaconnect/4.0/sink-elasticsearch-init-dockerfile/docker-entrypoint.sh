#!/bin/bash

# common configs:
#export REGION="us-west-1"
#export CLUSTER="t1"
#export MANAGE_SERVER_URL="firecamp-manageserver.t1-firecamp.com:27040"
#export OP="Catalog-Set-Service-Init"
#export SERVICE_TYPE="kafkasinkes"
#export SERVICE_NAME="mysinkes"

# https://docs.confluent.io/3.1.0/connect/connect-elasticsearch/docs/configuration_options.html
# Example configs:
# {"name": "t1-mysinkes", "config": {"connector.class":"io.confluent.connect.elasticsearch.ElasticsearchSinkConnector", "tasks.max":"1", "topics":"sinkes", "schema.ignore":"true", "key.ignore":"true", "type.name":"kafka-connect", "connection.url":"http://myes-0.t1-firecamp.com:9200"}}
# CONNECTOR_NAME="t1-mysinkes"
# CONNECTOR_HOSTS="esconn-0.t1-firecamp.com,esconn-1.t1-firecamp.com"
# CONNECTOR_PORT=8083

# check the environment parameters
if [ -z "$REGION" -o -z "$CLUSTER" -o -z "$MANAGE_SERVER_URL" -o -z "$OP" -o -z "$SERVICE_NAME" -o -z "$SERVICE_TYPE" -o -z "$CONNECTOR_NAME" ]
then
  echo "error: please pass all required environment variables" >&2
  exit 1
fi

# check elasticsearch connector parameters
if [ -z "$CONNECTOR_HOSTS" -o -z "$CONNECTOR_PORT" -o -z "$ELASTICSEARCH_CONFIGS" ]; then
  echo "error: please specify the CONNECTOR_HOSTS $CONNECTOR_HOSTS, CONNECTOR_PORT $CONNECTOR_PORT, ELASTICSEARCH_CONFIGS $ELASTICSEARCH_CONFIGS"
  exit 1
fi

echo "service $SERVICE_NAME, connector name $CONNECTOR_NAME, hosts $CONNECTOR_HOSTS"
echo "$ELASTICSEARCH_CONFIGS"

# get connector array
OIFS=$IFS
IFS=','
read -a chosts <<< "${CONNECTOR_HOSTS}"
IFS=$OIFS

# wait till the service node dns is ready
for chost in "${chosts[@]}"; do
  /waitdns.sh $chost
done

for chost in "${chosts[@]}"; do
  succ=-1

  for i in 5 10 10; do
    sleep $i

    date
    echo "create connector to $chost"

    # create the connector
    curl -s -X POST -H "Content-Type: application/json" --data "$ELASTICSEARCH_CONFIGS" http://$chost:$CONNECTOR_PORT/connectors
    res=$?
    if [ "$res" != "0" ]; then
      echo "create sink elasticsearch worker error: $res"
      continue
    fi

    # check whether connector is created
    output=$(curl http://$chost:$CONNECTOR_PORT/connectors/$CONNECTOR_NAME)
    res=$?
    if [ "$res" != "0" ]; then
      echo "check sink elasticsearch worker error: $res, $output"
      continue
    fi
    errcode=$(echo "$output" | grep "error_code")
    if [ -n "$errcode" ]; then
      echo "get sink elasticsearch worker error: $output"
      continue
    fi

    # successfully created
    echo "created sink elasticsearch worker: $output"
    succ=0
    break
  done

  if [ $succ -ne 0 ]; then
    echo "create sink elasticsearch worker to $chost failed, exit"
    exit 2
  fi
done

# notify the manage server about the initialization done
data="{\"Region\":\"$REGION\",\"Cluster\":\"$CLUSTER\",\"ServiceName\":\"$SERVICE_NAME\",\"ServiceType\":\"$SERVICE_TYPE\"}"
curl -X PUT -H "Content-Type: application/json" -d $data "$MANAGE_SERVER_URL/$OP"

