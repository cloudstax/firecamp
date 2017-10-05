#!/bin/bash

# https://redis.io/topics/cluster-tutorial
#
# This script initializes the Redis cluster. It does below steps:
# 1) wait till getting all nodes' IP.
# 2) /redis-trib.rb create master1:port master2:port master3:port, to initialize the master cluster.
# 3) /redis-trib.rb add-node --slave --master-id <masterid> slaveip:port masterip:port.
# 4) send the Set-Redis-Init request to the manage server.
#
# By the above steps, we could deploy the master/slave to the target availability zone.

# could not "set -e", which will exit if any command fails.
# The task may end at any time. For example, node crashes before set service initialized.
# The service init will retry the task, some command may fail. "set -e" will take this as error and exit.
# Instead, the script itself will handle the error.

#export REGION="us-west-1"
#export CLUSTER="c1"
#export MANAGE_SERVER_URL="firecamp-manageserver.c1-firecamp.com:27040"
#export SERVICE_NAME="myredis"
#export SERVICE_TYPE="redis"
#export SERVICE_PORT="6379"
#export OP="Catalog-Set-Redis-Init"

#export SHARDS="3"
#export REPLICAS_PERSHARD="2"
#export REDIS_MASTERS="myredis-0.c1-firecamp.com,myredis-1.c1-firecamp.com,myredis-2.c1-firecamp.com"
#export REDIS_SLAVES="myredis-3.c1-firecamp.com,myredis-4.c1-firecamp.com,myredis-5.c1-firecamp.com"
# The service masters should be myredis-shard0-0.c1-firecamp.com,myredis-shard1-0.c1-firecamp.com,myredis-shard2-0.c1-firecamp.com
# The service slaves should be myredis-shard0-1.c1-penmanage.com,myredis-shard1-1.c1-firecamp.com,myredis-shard2-1.c1-firecamp.com

# check the environment parameters
if [ -z "$REGION" -o -z "$CLUSTER" -o -z "$MANAGE_SERVER_URL" -o -z "$SERVICE_NAME" -o -z "$SERVICE_TYPE" -o -z "$SERVICE_PORT" -o -z "$OP" ]
then
  echo "error: please pass all required environment variables"
  exit 1
fi
# REDIS_SLAVES is allowed to be empty
if [ -z "$SHARDS" -o -z "$REPLICAS_PERSHARD" -o -z "$REDIS_MASTERS" ]
then
  echo "error: please pass all required redis variables"
  exit 1
fi

# get master list and slave list
OIFS=$IFS
IFS=','
read -a masters <<< "${REDIS_MASTERS}"
read -a slaves <<< "${REDIS_SLAVES}"
IFS=$OIFS

totalnodes=$(( ${#masters[@]} + ${#slaves[@]} ))

AddSlaveNodes() {
  # add slave into cluster
  i=0
  for s in "${slaves[@]}"
  do
    # check if the slave is already added
    nodes=$(/redis-cli -h $s cluster nodes | wc -l)
    if [ "$nodes" = "$totalnodes" ]; then
      echo "node $s is already added, check next node"
      continue
    fi

    # get slave ip
    res=$(host $s)
    ip=$(echo $res | awk '{ print $4 }')
    if [ $ip = "" ]; then
      echo "failed to resolve slave ip for $s"
      exit 2
    fi

    # get master id and ip
    midx=`expr $i / $REPLICAS_PERSHARD`
    m=${masters[$midx]}
    res=$(host $m)
    mip=$(echo $res | awk '{ print $4 }')
    if [ $mip = "" ]; then
      echo "failed to resolve master ip for $m"
      exit 2
    fi

    echo "add slave $s $ip to master $m $mip"
    masterid=$(/redis-cli -h $mip cluster nodes | grep "myself" | awk '{ print $1 }')

    # add slave to cluster
    echo "/redis-trib.rb add-node --slave --master-id $masterid $ip:$SERVICE_PORT $mip:$SERVICE_PORT"
    /redis-trib.rb add-node --slave --master-id $masterid $ip:$SERVICE_PORT $mip:$SERVICE_PORT

    if [ "$?" != "0" ]; then
      echo "add $s $ip to cluster failed"
      exit 2
    fi

    i=$(( $i + 1 ))
  done
}

SetServiceInit() {
  # get the node to redis id mapping
  nodeids=""
  for m in "${masters[@]}"
  do
    id=$(/redis-cli -h $m cluster nodes | grep "myself" | awk '{ print $1 }')
    if [ "$nodeids" = "" ]; then
      nodeids="\"$m $id master\""
    else
      nodeids="$nodeids, \"$m $id master\""
    fi
  done

  for m in "${slaves[@]}"
  do
    myrole=$(/redis-cli -h $m cluster nodes | grep "myself")
    id=$(echo "$myrole" | awk '{ print $1 }')
    master=$(echo "$myrole" | awk '{ print $4 }')
    nodeids="$nodeids, \"$m $id slave $master\""
  done

  echo "$nodeids"

  # set service initialized

curl -X PUT "$MANAGE_SERVER_URL/$OP" \
  -H "Content-Type: application/json" \
  -d @- <<EOF
{
  "Region": "$REGION",
  "Cluster": "$CLUSTER",
  "ServiceName": "$SERVICE_NAME",
  "NodeIds": [$nodeids]
}
EOF

  # sleep some time for the server to restart all containers
  sleep 20
}

# wait till all nodes' DNS are ready
masterips=""
for m in "${masters[@]}"
do
  /waitdns.sh $m
  res=$(host $m)
  ip=$(echo $res | awk '{ print $4 }')
  if [ $ip = "" ]; then
    echo "failed to resolve ip for $m"
    exit 2
  fi
  masterips="$masterips $ip:$SERVICE_PORT"
done

echo "$masterips"

for m in "${slaves[@]}"
do
  /waitdns.sh $m
done

# sleep some time for the ip routing update
sleep 20

# check if the redis cluster is already created
nodes=$(/redis-cli -h ${masters[0]} cluster nodes | wc -l)
if [ "$nodes" = "$totalnodes" ]; then
  # all masters and slaves are created, set service init
  echo "the cluster is already created, set service initialized"
  SetServiceInit
  exit 0
fi

# cluster is not fully created, check if the cluster has created with the masters
if [ "$nodes" != "${#masters[@]}" ]; then
  if [ "$nodes" != "1" ]; then
    # TODO fix the partially created cluster
    echo "cluster is partially created, expect masters: ${masters[@]} $masterips"
    /redis-cli -h ${masters[0]} cluster nodes
    exit 9
  fi

  # create the redis cluster
  echo "redis-trib.rb create $masterips"
  printf 'yes' | /redis-trib.rb create $masterips

  if [ "$?" != "0" ]; then
    echo "create cluster failed"
    exit 2
  fi
else
  echo "cluster is already created with masters: ${masters[@]} $masterips"
  /redis-cli -h ${masters[0]} cluster nodes
fi

# add slaves to the cluster
AddSlaveNodes

# set service initialized
SetServiceInit

