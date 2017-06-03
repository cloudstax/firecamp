#!/bin/bash
set -e

# see supported platforms and db types in types.go
#CONTAINER_PLATFORM="ecs"
#DB_TYPE="controldb"

if [ -n "$CONTAINER_PLATFOMR" ] || [ -n "$DB_TYPE" ]
then
  echo "error: please input all required environment variables" >&2
  echo "CONTAINER_PLATFORM $CONTAINER_PLATFORM, DB_TYPE $DB_TYPE" >&2
fi

exec "/openmanage-aws-managehttpserver" "-container-platform=$CONTAINER_PLATFORM" "-dbtype=$DB_TYPE"
