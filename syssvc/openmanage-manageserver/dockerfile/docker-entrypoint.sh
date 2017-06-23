#!/bin/bash
set -e

# see supported platforms and db types in types.go
#CONTAINER_PLATFORM="ecs"
#DB_TYPE="clouddb"

if [ -n "$CONTAINER_PLATFOMR" ] || [ -n "$DB_TYPE" ] || [ -n "$AVAILABILITY_ZONES" ]
then
  echo "error: please input all required environment variables" >&2
  echo "CONTAINER_PLATFORM $CONTAINER_PLATFORM, DB_TYPE $DB_TYPE, AVAILABILITY_ZONES $AVAILABILITY_ZONES" >&2
fi

exec "/openmanage-manageserver" "-container-platform=$CONTAINER_PLATFORM" "-dbtype=$DB_TYPE" "-availability-zones=$AVAILABILITY_ZONES"
