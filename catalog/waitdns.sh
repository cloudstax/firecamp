#!/bin/sh

# This script waits for the dns lookup.

dnsname=$1

echo "check dns for $dnsname"
date

# When the service is created at the first time, lookup the ip from Route53 needs around 60 seconds.
# Some task, such as init task, needs to talk to the service members. So has to wait till the ip could be looked up.

# wait max 90 seconds.
for i in `seq 1 30`
do
  host $dnsname
  if [ "$?" = "0" ]; then
    break
  fi
  sleep 3
done

date
