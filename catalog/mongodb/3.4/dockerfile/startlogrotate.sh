#!/bin/sh

# "apt install cron" requires 46MB additional disk space.
# not necessary to use cron.
# write a single script to rotate mongodb log.

while true;
do
  /mongologrotate.sh
  sleep 30m
done
