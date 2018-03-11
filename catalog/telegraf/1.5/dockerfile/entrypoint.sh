#!/bin/bash
set -e

# check required parameters
if [ -z "$REGION" -o -z "$CLUSTER" -o -z "$SERVICE_NAME" -o -z "$SERVICE_TYPE" ]; then
  echo "error: please specify the REGION $REGION, CLUSTER $CLUSTER, SERVICE_NAME $SERVICE_NAME, SERVICE_TYPE $SERVICE_TYPE"
  exit 1
fi

# add input plugin
if [ "$SERVICE_TYPE" = "redis" ]; then
  # check redis required parameters
  # TODO simply pass redis auth password in the env variable. should fetch from DB or manage server.
  if [ -z "$SERVICE_MEMBERS" -o -z "$REDIS_AUTH" ]; then
    echo "error: please specify SERVICE_MEMBERS $SERVICE_MEMBERS and REDIS_AUTH $REDIS_AUTH"
  fi

  OIFS=$IFS
  IFS=','
  read -a members <<< "${SERVICE_MEMBERS}"
  IFS=$OIFS

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

  # update the redis servers to input_redis.conf
  sed -i "s/\"firecamp-redis-servers\"/$servers/g" /firecamp/input_redis.conf

  # add redis input plugin to telegraf.conf
  cat /firecamp/input_redis.conf >> /firecamp/telegraf.conf
fi

# add output plugin
cat /firecamp/output_cloudwatch.conf >> /firecamp/telegraf.conf

cat /firecamp/telegraf.conf

echo "$@"
exec "$@"
