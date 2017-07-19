#!/bin/bash
set -e

#CONTAINER_PLATFORM="ecs"

if [ -z "$CONTAINER_PLATFOMR" ]
then
  echo "error: please input all required environment variables" >&2
  echo "CONTAINER_PLATFORM $CONTAINER_PLATFORM" >&2
  exit 1
fi

exec "/openmanage-controldb" "-container-platform=$CONTAINER_PLATFORM"
