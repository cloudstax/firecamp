#!/bin/sh

# option 1, iptables
# move this to docker-entrypoint.sh
if [ -f "$clustercfgfile" -a "$(id -u)" = '0' ]; then
  # start the routing update script in the background
  echo "periodically update the routing in the background"
  /routing.sh $clustercfgfile &
fi


# clustercfgfile line format: "serviceMemberName clusterAnnounceIP"
clustercfgfile=$1

# check and update iptables periodically
while true
do
  # update the iptables to route the member's clusterAnnounceIP to the correct destination.
  # please refer to catalog/redis/README.md for why this is required.
  while read line
  do
    if [ "$line" = "" ]; then
      continue
    fi

    member=$(echo $line | awk '{ print $1 }')
    announceIP=$(echo $line | awk '{ print $2 }')

    res=$(host $member)
    memberip=$(echo $res | awk '{ print $4 }')

    res=$(iptables -t nat -L | grep $announceIP)
    if [ "$res" != "" ]; then
      oldip=$(echo $res | awk '{ print $6 }' | awk -F ":" '{ print $2 }')
      if [ "$oldip" = "$memberip" ]; then
        # member ip is not changed, check next member
        continue
      fi
      # member ip changed, delete the current rule
      echo "service member $member $announceIP, delete the rule to the old destination $oldip"
      iptables -t nat -D PREROUTING -d $announceIP -j DNAT --to-destination $oldip
    fi

    # add the iptables rule for member
    echo "service member $member $announceIP, add the rule to the destination $memberip"
    iptables -t nat -A PREROUTING -d $announceIP -j DNAT --to-destination $memberip
  done < $clustercfgfile

  sleep 5
done


# option 2: update redis-node.conf way
# clustercfgfile line format: "serviceMemberName redisClusterID"
if [ -f "$clustercfgfile" ]; then
  # cluster is already initialized, load the cluster member to master mapping.
  date
  while read line
  do
    member=$(echo $line | awk '{ print $1 }')
    host $member
  done < $clustercfgfile

  # wait some time to make sure DNS is fully refreshed
  sleep 20

  date
  # check whether need to update the redis-cluster.conf
  # TODO this is only the temporary workaround, please refer to catalog/redis/README.md for the detail reason.
  while read line
  do
    member=$(echo $line | awk '{ print $1 }')
    id=$(echo $line | awk '{ print $2 }')

    # cluster not initialized: "370f5cfebc73610336506b3d1347c1be589c1df6 :0@0 myself,master - 0 0 0 connected"
    # cluster initialized: "9f44a459523d55948389ad514970e0443e627491 172.31.64.45:6379@16379 myself,master - 0 0 1 connected 0-5460"
    oldmasterip=$(grep "$id" $redisnodefile | awk '{ print $2 }' | awk -F ':' '{ print $1 }')
    if [ "$oldmasterip" != "" ]; then
      # cluster initialized, check whether need to update the master ip address
      host $member
      res=$(host $member)
      newmasterip=$(echo $res | awk '{ print $4 }')
      echo "service member $member $id old ip $oldmasterip new ip $newmasterip"

      if [ "$oldmasterip" != "$newmasterip" ]; then
        # update the master ip address
        sed -i "s/${oldmasterip}/${newmasterip}/g" $redisnodefile
      fi
    fi

  done < $clustercfgfile
fi

