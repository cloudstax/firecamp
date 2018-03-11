#!/bin/bash
set -e

# check required parameters
# SERVICE_NAME is the stateful service to monitor.
# TELEGRAF_SERVICE_NAME is the telegraf service that monitors the stateful service.
if [ -z "$REGION" -o -z "$CLUSTER" -o -z "$SERVICE_NAME" -o -z "$SERVICE_TYPE" -o -z "$SERVICE_MEMBERS" -o -z "$TELEGRAF_SERVICE_NAME" ]; then
  echo "error: please specify the REGION $REGION, CLUSTER $CLUSTER, SERVICE_NAME $SERVICE_NAME, SERVICE_TYPE $SERVICE_TYPE, SERVICE_MEMBERS $SERVICE_MEMBERS, TELEGRAF_SERVICE_NAME $TELEGRAF_SERVICE_NAME"
  exit 1
fi

# set telegraf configs
export HOSTNAME=$TELEGRAF_SERVICE_NAME
export INTERVAL="60s"
if [ -z "$COLLECT_INTERVAL" ]; then
  export INTERVAL=$COLLECT_INTERVAL
fi

# get service members array
OIFS=$IFS
IFS=','
read -a members <<< "${SERVICE_MEMBERS}"
IFS=$OIFS


# add input plugin
if [ "$SERVICE_TYPE" = "redis" ]; then
  # check the service required parameters
  # TODO simply pass redis auth password in the env variable. should fetch from DB or manage server.
  if [ -z "$REDIS_AUTH" ]; then
    echo "error: please specify REDIS_AUTH $REDIS_AUTH"
    exit 2
  fi

  servers=""
  i=0
  for m in "${members[@]}"; do
    if [ "$i" = "0" ]; then
      servers="\"tcp:\/\/:$REDIS_AUTH@$m\""
    else
      servers+=",\"tcp:\/\/:$REDIS_AUTH@$m\""
    fi
    i=$(( $i + 1 ))
  done

  # update the servers in input conf
  sed -i "s/\"firecamp-service-servers\"/$servers/g" /firecamp/input_redis.conf

  # add service input plugin to telegraf.conf
  cat /firecamp/input_redis.conf >> /firecamp/telegraf.conf
fi

# add input plugin
if [ "$SERVICE_TYPE" = "zookeeper" ]; then
  servers=""
  i=0
  for m in "${members[@]}"; do
    if [ "$i" = "0" ]; then
      servers="\"$m\""
    else
      servers+=",\"$m\""
    fi
    i=$(( $i + 1 ))
  done

  # update the servers in input conf
  sed -i "s/\"firecamp-service-servers\"/$servers/g" /firecamp/input_zk.conf

  # add service input plugin to telegraf.conf
  cat /firecamp/input_zk.conf >> /firecamp/telegraf.conf
fi


# add output plugin
cat /firecamp/output_cloudwatch.conf >> /firecamp/telegraf.conf

cat /firecamp/telegraf.conf

echo "$@"
exec "$@"
