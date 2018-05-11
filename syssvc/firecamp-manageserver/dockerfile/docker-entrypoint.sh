#!/bin/bash
set -e

# see supported platforms and db types in types.go
#CONTAINER_PLATFORM="ecs"
#CLUSTER="t1"
#DB_TYPE="clouddb"

if [ -z "$CONTAINER_PLATFORM" -o -z "$CLUSTER" -o -z "$DB_TYPE" -o -z "$AVAILABILITY_ZONES" ]
then
  echo "error: please input all required environment variables" >&2
  echo "CONTAINER_PLATFORM $CONTAINER_PLATFORM, CLUSTER $CLUSTER, DB_TYPE $DB_TYPE, AVAILABILITY_ZONES $AVAILABILITY_ZONES" >&2
  exit 1
fi

exec "/firecamp-manageserver" "-container-platform=$CONTAINER_PLATFORM" "-cluster=$CLUSTER" "-dbtype=$DB_TYPE" "-availability-zones=$AVAILABILITY_ZONES"
