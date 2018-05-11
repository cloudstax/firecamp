#!/bin/bash
set -e

# see supported platforms in api/common/types.go
#CONTAINER_PLATFORM="ecs"
#CLUSTER="t1"
#AVAILABILITY_ZONES="us-east-1a,us-east-1b,us-east-1c"

if [ -z "$CONTAINER_PLATFORM" -o -z "$CLUSTER" -o -z "$AVAILABILITY_ZONES" ]
then
  echo "error: please input all required environment variables" >&2
  echo "CONTAINER_PLATFORM $CONTAINER_PLATFORM, CLUSTER $CLUSTER, AVAILABILITY_ZONES $AVAILABILITY_ZONES" >&2
  exit 1
fi

exec "/firecamp-catalogservice" "-container-platform=$CONTAINER_PLATFORM" "-cluster=$CLUSTER" "-availability-zones=$AVAILABILITY_ZONES"
