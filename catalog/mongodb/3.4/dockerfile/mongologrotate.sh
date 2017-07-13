#!/bin/sh

# rotate mongodb log, https://docs.mongodb.com/manual/tutorial/rotate-log-files/
# logrotate is not used, as mongodb will not be able to know the log file is changed.
# using copytruncate could avoid it. but there is a window between copy and truncate,
# some mongod logs may get lost.
# write this simple script to send SIGUSR1 to mongod directly.

logdir=/var/log/mongodb
logfile=$logdir/mongod.log

# the max number of log files
maxfiles=30

# max file size: 64MB
maxsize=67108864

fsize=$(stat -c%s $logfile)
if [ $fsize -gt $maxsize ]; then
  # ask mongod to rotate the log file
  pkill --signal SIGUSR1 mongod

  # tar the rotated log file and remove the original log file
  ls -1 -I "*.gz" -I "mongod.log" $logdir | awk '{ system("tar -Pzcf /var/log/mongodb/" $0 ".gz" " /var/log/mongodb/" $0 " ; rm -f /var/log/mongodb/" $0) }'

  # check and remove the old log files
  fnum=$(ls -1U $logdir | wc -l)
  if [ $fnum -gt $maxfiles ]; then
    rmnum=`expr $fnum - $maxfiles`
    ls -1rt $logdir | head -n $rmnum | awk '{ system("rm -f /var/log/mongodb/" $0) }'
  fi

fi

