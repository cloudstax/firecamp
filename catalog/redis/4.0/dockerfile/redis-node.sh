#!/bin/sh

# clustercfgfile line format: "serviceMemberName redisClusterID role(master/slave) masterIdofSlave"
clustercfgfile=$1
redisnodefile=$2

# cluster is already initialized, load the cluster member to master mapping.
date
while read line
do
  member=$(echo $line | awk '{ print $1 }')
  host $member
done < $clustercfgfile

# wait some time to make sure DNS is fully refreshed
sleep 10

date
# check whether need to update the redis-node.conf
while read line
do
  if [ "$line" = "" ]; then
    continue
  fi

  member=$(echo $line | awk '{ print $1 }')
  id=$(echo $line | awk '{ print $2 }')
  role=$(echo $line | awk '{ print $3 }')

  # get the member's current ip
  newip=""
  for i in `seq 1 3`
  do
    host $member
    res=$(host $member)
    newip=$(echo $res | awk '{ print $4 }')
    if [ "$newip" != "" ]; then
      break
    fi
    sleep 3
  done
  if [ "$newip" = "" ]; then
    echo "failed to lookup ip for $member"
    exit 1
  fi

  # cluster not initialized: "9f44a459523d55948389ad514970e0443e627491 :0@0 myself,master - 0 0 0 connected"
  # cluster initialized: "9f44a459523d55948389ad514970e0443e627491 172.31.64.45:6379@16379 myself,master - 0 0 1 connected 0-5460"
  # "9d3f958bd4a69d5e19b267c02c84c1dea24af5ba 172.31.68.178:6379@16379 slave 9f44a459523d55948389ad514970e0443e627491 0 1502477471000 3 connected"
  # when all nodes restart, redis may set the master ip to null. During the restart,
  # some containers may be started later. So some redis node may not be able to talk
  # with some other node, and change to the wrong/empty ip or wrong role.
  while read noderole
  do
    r=$(echo "$noderole" | awk '{ print $1 }')
    # update the ip of the current id only
    if [ "$r" = "$id" ]; then
      ip=$(echo "$noderole" | awk '{ print $2 }' | awk -F ':' '{ print $1 }')

      if [ "$ip" = "" ]; then
        echo "member has no ip set, set $noderole to new ip $newip for $line"
        newrole="${noderole/:0@0/$newip:6379@16379}"
        sed -i "s/${noderole}/${newrole}/g" $redisnodefile
      elif [ "$ip" != "$newip" ]; then
        echo "update $noderole to new ip $newip for $line"
        sed -i "s/${ip}/${newip}/g" $redisnodefile
      else
        echo "ip not changed, current $noderole, new ip $newip for $line"
      fi

      break
    fi
  done < $redisnodefile

done < $clustercfgfile

exit 0

