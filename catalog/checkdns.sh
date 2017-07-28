#!/bin/sh

# This script waits for the dns update.

SERVICE_MEMBER=$1

echo "check dns for $SERVICE_MEMBER"
date

# When the service is created at the first time, Route53 looks need around 60 seconds
# to get the dns entry. So the cluster would require at least 60 seconds to initialize.

# TODO if container fails over to another node, the dns check will get the previous
# address. This looks not cause any issue. Could consider to wait till the dns lookup
# get the local address.

# wait max 120 seconds.
for i in `seq 1 40`
do
  host $SERVICE_MEMBER
  if [ "$?" = "0" ]; then
    break
  fi
  sleep 3
done

date
