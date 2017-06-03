#!/bin/bash
set -e

#CONTAINER_PLATFORM="ecs"

if [ -n "$CONTAINER_PLATFOMR" ]
then
  echo "error: please input all required environment variables" >&2
  echo "CONTAINER_PLATFORM $CONTAINER_PLATFORM" >&2
fi

exec "/openmanage-aws-controldb" "-container-platform=$CONTAINER_PLATFORM"
