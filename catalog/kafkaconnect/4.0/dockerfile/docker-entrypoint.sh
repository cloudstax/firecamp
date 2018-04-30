#!/bin/bash
set -ex

# Example command to run cp-kafka-connect docker container. For FireCamp connector service,
# all the env variables, except CONNECT_REST_HOST_NAME and CONNECT_REST_ADVERTISED_HOST_NAME,
# will be set when creating the connector service. The service member will be assigned by
# firecamp-selectmember tool. The tool also updates the service member's DNS record to point to the local address.
#
#docker run -d \
#  --name=kafka-connect-es \
#  --net=host \
#  -e CONNECT_BOOTSTRAP_SERVERS=mykafka-0.t1-firecamp.com:9092 \
#  -e CONNECT_REST_HOST_NAME="localhost" \
#  -e CONNECT_REST_PORT=8083 \
#  -e CONNECT_GROUP_ID="quickstart-es" \
#  -e CONNECT_CONFIG_STORAGE_TOPIC="quickstart-es-config" \
#  -e CONNECT_OFFSET_STORAGE_TOPIC="quickstart-es-offsets" \
#  -e CONNECT_STATUS_STORAGE_TOPIC="quickstart-es-status" \
#  -e CONNECT_CONFIG_STORAGE_REPLICATION_FACTOR=1 \
#  -e CONNECT_OFFSET_STORAGE_REPLICATION_FACTOR=1 \
#  -e CONNECT_STATUS_STORAGE_REPLICATION_FACTOR=1 \
#  -e CONNECT_KEY_CONVERTER="org.apache.kafka.connect.json.JsonConverter" \
#  -e CONNECT_VALUE_CONVERTER="org.apache.kafka.connect.json.JsonConverter" \
#  -e CONNECT_KEY_CONVERTER_SCHEMAS_ENABLE=false \
#  -e CONNECT_VALUE_CONVERTER_SCHEMAS_ENABLE=false \
#  -e CONNECT_INTERNAL_KEY_CONVERTER="org.apache.kafka.connect.json.JsonConverter" \
#  -e CONNECT_INTERNAL_VALUE_CONVERTER="org.apache.kafka.connect.json.JsonConverter" \
#  -e CONNECT_INTERNAL_KEY_CONVERTER_SCHEMAS_ENABLE=false \
#  -e CONNECT_INTERNAL_VALUE_CONVERTER_SCHEMAS_ENABLE=false \
#  -e CONNECT_REST_ADVERTISED_HOST_NAME="localhost" \
#  -e CONNECT_LOG4J_ROOT_LOGLEVEL=DEBUG \
#  -e CONNECT_LOG4J_LOGGERS=org.reflections=ERROR \
#  -e CONNECT_PLUGIN_PATH=/usr/share/java \
#  confluentinc/cp-kafka-connect:4.0.0

# check required parameters
if [ -z "$REGION" -o -z "$CLUSTER" -o -z "$SERVICE_NAME" -o -z "$CONTAINER_PLATFORM" -o -z "$DB_TYPE" ]; then
  echo "error: please specify the REGION $REGION, CLUSTER $CLUSTER, SERVICE_NAME $SERVICE_NAME, CONTAINER_PLATFORM $CONTAINER_PLATFORM, DB_TYPE $DB_TYPE"
  exit 1
fi

# for now, simply pass all configs in env variables. later, we may put all configs in service.conf
if [ "$CONTAINER_PLATFORM" = "ecs" -o "$CONTAINER_PLATFORM" = "swarm" ]; then
  # select one service member, update member dns and write the member dns name to /etc/firecamp-member.
  /firecamp-selectmember -cluster=$CLUSTER -service-name=$SERVICE_NAME -container-platform=$CONTAINER_PLATFORM

  # load member configs
  . /etc/firecamp-member

else
  # for K8s
  # if member is only accessible from within k8s cluster
  #   SERVICE_MEMBER=podname.internaldomain
  # else
  #   the member's dns name the init container will update member dns, or call a tool here.
  #   SERVICE_MEMBER=podname.externaldomain
  # SERVICE_MEMBER=${POD_NAME}.${DOMAIN}
  echo "error: not support k8s yet"
  exit 2
fi

echo "$SERVICE_MEMBER"
export CONNECT_REST_HOST_NAME=$SERVICE_MEMBER
export CONNECT_REST_ADVERTISED_HOST_NAME=$SERVICE_MEMBER

echo "$@"
exec "$@"
