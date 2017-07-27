#!/bin/sh

# This script waits for the dns update.

SERVICE_MEMBER=$1

echo "check dns for $SERVICE_MEMBER"
date

# wait max 120 seconds
for i in `seq 1 40`
do
  host $SERVICE_MEMBER
  if [ "$?" = "0" ]; then
    break
  fi
  sleep 3
done

date
