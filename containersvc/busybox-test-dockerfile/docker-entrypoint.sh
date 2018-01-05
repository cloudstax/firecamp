#!/bin/sh

echo "start sleeping, $SLEEP_TIME"

if [ -n "$SLEEP_TIME" ]; then
  sleep $SLEEP_TIME
else
  sleep 20
fi

echo "end sleeping"
