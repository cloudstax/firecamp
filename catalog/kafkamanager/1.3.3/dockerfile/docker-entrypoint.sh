#!/bin/bash

if [ -z "$ZK_HOSTS" -o -z "$CLUSTER" -o -z "$SERVICE_NAME" ]; then
  echo "error: please specify the ZK_HOSTS $ZK_HOSTS, CLUSTER $CLUSTER and SERVICE $SERVICE_NAME"
  exit 1
fi

if [[ $KM_USERNAME != ''  && $KM_PASSWORD != '' ]]; then
  sed -i.bak '/^basicAuthentication/d' /kafka-manager-${KM_VERSION}/conf/application.conf
  echo 'basicAuthentication.enabled=true' >> /kafka-manager-${KM_VERSION}/conf/application.conf
  echo "basicAuthentication.username=${KM_USERNAME}" >> /kafka-manager-${KM_VERSION}/conf/application.conf
  echo "basicAuthentication.password=${KM_PASSWORD}" >> /kafka-manager-${KM_VERSION}/conf/application.conf
  echo 'basicAuthentication.realm="Kafka-Manager"' >> /kafka-manager-${KM_VERSION}/conf/application.conf
fi

./firecamp-service-updatedns -cluster=$CLUSTER -service-name=$SERVICE_NAME

exec ./bin/kafka-manager -Dconfig.file=${KM_CONFIGFILE} "${KM_ARGS}" "${@}"

