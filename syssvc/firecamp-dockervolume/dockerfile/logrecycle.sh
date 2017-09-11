#!/bin/sh

logdir=/var/log/firecamp
listinfofiles="$logdir/*.INFO.*"
listerrfiles="$logdir/*.ERROR.*"
listwarningfiles="$logdir/*.WARNING.*"

# the max number of log files
maxfiles=20

while true;
do
  ls -1rt $listinfofiles

  # check and remove the old log files
  fnum=$(ls -1rt $listinfofiles | wc -l)
  if [ $fnum -gt $maxfiles ]; then
    rmnum=`expr $fnum - $maxfiles`
    ls -1rt $listinfofiles | head -n $rmnum | awk '{ system("rm -f " $0) }'

    # only check the ERROR and WARNING log files when recycling the INFO files.
    fnum=$(ls -1rt $listerrfiles | wc -l)
    if [ $fnum -gt $maxfiles ]; then
      rmnum=`expr $fnum - $maxfiles`
      ls -1rt $listerrfiles | head -n $rmnum | awk '{ system("rm -f " $0) }'
    fi

    fnum=$(ls -1rt $listwarningfiles | wc -l)
    if [ $fnum -gt $maxfiles ]; then
      rmnum=`expr $fnum - $maxfiles`
      ls -1rt $listwarningfiles | head -n $rmnum | awk '{ system("rm -f " $0) }'
    fi
  fi

  sleep 10m
done
