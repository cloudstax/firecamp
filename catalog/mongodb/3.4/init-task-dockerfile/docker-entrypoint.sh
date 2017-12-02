#!/bin/bash

# This script does 3 things:
# 1) Initialize the MongoDB cluster.
# 2) Create the admin user.
# 3) Tell the firecamp manage server to set MongoDB service initialized.

# could not "set -e", which will exit if any command fails.
# The task may end at any time. For example, node crashes before set service initialized.
# The MongoDB init will retry the task, the mongo admin command may return "already initialized".
# "set -e" will take this as error and exit.

# The mongo commands, refer to https://docs.mongodb.com/manual/reference/command/

#export REGION="us-west-1"
#export CLUSTER="t1"
#export MANAGE_SERVER_URL="firecamp-manageserver.t1-firecamp.com:27040"
#export SERVICE_NAME="mymongo"
#export SERVICE_TYPE="mongodb"
#export OP="?Catalog-Set-Service-Init"
#export ADMIN="dbadmin"
#export ADMIN_PASSWORD="changeme"

# firecamp release 0.9
#export SERVICE_MASTER="mymongo-0.t1-firecamp.com"
#export SERVICE_MEMBERS="mymongo-1.t1-firecamp.com,mymongo-2.t1-firecamp.com"
#export REPLICA_SET_NAME="mymongo"

# firecamp release after 0.9
#export SHARDS="1"
#export REPLICAS_PERSHARD="3"
#export REPLICASET_ONLY="false"
# if SHARDS == 1 && REPLICASET_ONLY == "true"
#export SERVICE_MEMBERS="mymongo-0.t1-firecamp.com,mymongo-1.t1-firecamp.com,mymongo-2.t1-firecamp.com"
# else
#export CONFIG_SERVER_MEMBERS="mymongo-config-0.t1-firecamp.com,mymongo-config-1.t1-firecamp.com,mymongo-config-2.t1-firecamp.com"
#export SHARD_MEMBERS="mymongo-shard0-0.t1-firecamp.com,mymongo-shard0-1.t1-firecamp.com,mymongo-shard0-2.t1-firecamp.com;mymongo-shard1-0.t1-firecamp.com,mymongo-shard1-1.t1-firecamp.com,mymongo-shard1-2.t1-firecamp.com"

# check the environment parameters
if [ -z "$REGION" -o -z "$CLUSTER" -o -z "$MANAGE_SERVER_URL" -o -z "$SERVICE_NAME" -o -z "$SERVICE_TYPE" -o -z "$OP" ]; then
  echo "error: please pass all required environment variables"
  exit 1
fi
# TODO should fetch ADMIN_PASSWORD from the manage server.
# On Set-Service-Initialized, the manage server will delete the ADMIN_PASSWORD.
if [ -z "$ADMIN" ] || [ -z "$ADMIN_PASSWORD" ]; then
  echo "error: please pass the admin user and password"
  exit 1
fi

# InitReplicaSet initializes the replicaset. This function requires 3 parameters.
# 1. replicaset name.
# 2. replicaSets. example: mymongo-0.t1-firecamp.com,mymongo-1.t1-firecamp.com,mymongo-2.t1-firecamp.com
# 3. port, 27019 for config server, 27018 for shard server, 27017 for the single replica set
InitReplicaSet() {
  replSetName=$1
  replicaSets=$2
  port=$3

  echo "InitReplicaSet $replSetName $replicaSets"

  # 1) Initialize the MongoDB ReplicaSet.
  # get all members
  OIFS=$IFS
  IFS=','
  read -a members <<< "${replicaSets}"
  IFS=$OIFS

  # the first member is the initial master
  master=${members[0]}

  # wait till all members are ready
  for m in "${members[@]}"
  do
    /waitdns.sh $m
  done

  # generate the MongoDB Replicaset config
  cfg=""
  i=0
  for m in "${members[@]}"
  do
    if [ "$i" = "0" ]; then
      cfg="{_id: '$replSetName', members: [ {_id: 0, host: '$m:$port'}"
    else
      cfg+=", {_id:$i, host: '$m:$port'}"
    fi
    i=$(( $i + 1 ))
  done
  cfg+=" ] }"

  echo $cfg

  # initialize MongoDB Replicaset
  output=$(mongo --host $master --port $port --eval "JSON.stringify(db.adminCommand({'replSetInitiate' : $cfg}))")
  echo "$output"

  # example output
  #output="MongoDB shell version: 3.2.10 connecting to: f25b11c002e3 {\"info\":\"try querying local.system.replset to see current configuration\",\"ok\":0,\"errmsg\":\"already initialized\",\"code\":23}"
  #output="MongoDB shell version: 3.2.10 connecting to: f25b11c002e3 {"ok":1}"
  #output="MongoDB shell version: 3.2.10 connecting to: firecamp-mongodb-1rep.cluster-firecamp.com/test 2017-04-10T20:13:33.911+0000 I NETWORK [thread1] getaddrinfo(\"firecamp-mongodb-1rep.cluster-firecamp.com\") failed: Name or service not known 2017-04-10T20:13:33.921+0000 E QUERY [thread1] Error: couldn't initialize connection to host firecamp-mongodb-1rep.cluster-firecamp.com, address is invalid : connect@src/mongo/shell/mongo.js:231:14 @(connect):1:6"


  # analyze the initialization results
  succ=$(echo "$output" | grep "\"ok\":1")
  errExistStr="already initialized"
  errExistCodeStr="\"code\":23"
  errExistMsg=$(echo "$output" | grep "$errExistStr")
  errExistCode=$(echo "$output" | grep "$errExistCodeStr")

  #echo "succ $succ"
  #echo "errExistMsg $errExistMsg"
  #echo "errExistCode $errExistCode"

  # take "already initialized" as success
  if [ -z "$succ" ] && ([ -z "$errExistMsg" ] || [ -z "$errExistCode" ])
  then
    echo "init fail"
    # most likely the error is "connect failed". wait some time
    sleep 20
    echo $output >&2
    exit 2
  fi

  echo "MongoDB ReplicaSet $replSetName initialized"

  # wait 10 seconds for MongoDB to stabilize
  sleep 10
}

# CreateRootAdminUser creates the root admin user. The function requires 2 parameters.
# 1. mongoNode: the master instance of the single replica set, or the mongos instance of the shard cluster.
# 2. port, 27019 for config server, 27018 for shard server, 27017 for the single replica set
CreateRootAdminUser() {
  mongoNode=$1
  port=$2

  echo "CreateRootAdminUser to $mongoNode"

  # 2) Create the root admin user.
  # create the admin user
  #mongo  --host $SERVICE_MASTER << EOF
  #use admin;
  #db.createUser(
  #  {
  #    user: "${ADMIN}",
  #    pwd: "${ADMIN_PASSWORD}",
  #    roles: [ { role: "root", db: "admin" } ]
  #  }
  #);
  #EOF
  createUser="{ createUser: \"${ADMIN}\", pwd: \"${ADMIN_PASSWORD}\", roles: [ { role: \"root\", db: \"admin\" } ] }"
  output=$(mongo --host $mongoNode --port $port --eval "JSON.stringify(db.adminCommand($createUser))")
  echo "$output"

  #example outputs
  #output="{\"ok\":1}"
  #output="{\"ok\":0,\"errmsg\":\"User \"dbadmin@admin\" already exists\",\"code\":11000,\"codeName\":\"DuplicateKey\"}"

  succ=$(echo "$output" | grep "\"ok\":1")
  errUserExistStr="already exists"
  errUserExistCodeStr="\"code\":11000"
  errUserExistMsg=$(echo "$output" | grep "$errUserExistStr")
  errUserExistCode=$(echo "$output" | grep "$errUserExistCodeStr")

  # take "already exists" as success
  if [ -z "$succ" ] && ([ -z "$errUserExistMsg" ] || [ -z "$errUserExistCode" ])
  then
    echo "create user failed"
    echo $output >&2
    exit 2
  fi

  echo "root admin user created"
}

# CreateReplicaSetAndUser initializes the replicaset and create the root admin user.
# This function requires 3 parameters.
# 1. replicaset name.
# 2. replicaSets. example: mymongo-0.t1-firecamp.com,mymongo-1.t1-firecamp.com,mymongo-2.t1-firecamp.com
# 3. port, 27019 for config server, 27018 for shard server, 27017 for the single replica set
CreateReplicaSetAndUser() {
  replSetName=$1
  replicaSets=$2
  port=$3

  # get all members
  OIFS=$IFS
  IFS=','
  read -a members <<< "${replicaSets}"
  IFS=$OIFS

  InitReplicaSet $replSetName $replicaSets $port

  # get the latest master. If the original master fails, another standby may become the new master.
  # example output: "primary" : "mymongo-1.t1-firecamp.com:27017",
  currentMaster=$(mongo --host ${members[0]} --port $port --eval "db.isMaster()" | grep primary | awk '{ print $3 }' | awk -F "\"" '{ print $2 }' | awk -F ":" '{ print $1 }')
  if [ -z "$currentMaster" ]
  then
    echo "failed to get the current master"
    mongo --host ${members[0]} --port $port --eval "db.isMaster()"
    exit 2
  fi
  echo "currentMaster: $currentMaster"

  CreateRootAdminUser $currentMaster $port
}

if [ -n "$SERVICE_MASTER" ]; then
  # firecamp release 0.9
  if [ -z "$REPLICA_SET_NAME" -o -z "$SERVICE_MEMEBERS" ]; then
    echo "error: please pass the replica set name and slaves"
    exit 1
  fi

  # a single replica set
  replicaSets=$SERVICE_MASTER","$SERVICE_MEMBERS
  CreateReplicaSetAndUser $REPLICA_SET_NAME $replicaSets 27017
else
  # firecamp release after 0.9
  if [ "$SHARDS" = "1" -a "$REPLICASET_ONLY" = "true" ]; then
    # a single replica set
    if [ -z "$REPLICAS_PERSHARD" -o -z "$SERVICE_MEMBERS" ]; then
      echo "error: please pass the replicas per shard"
      exit 1
    fi

    CreateReplicaSetAndUser $SERVICE_NAME $SERVICE_MEMBERS 27017
  else
    if [ -z "$REPLICAS_PERSHARD" -o -z "$CONFIG_SERVER_MEMBERS" -o -z "$SHARD_MEMBERS" ]; then
      echo "error: please pass the replicas per shard, config server members and shard members"
      exit 1
    fi

    # get all shards
    OIFS=$IFS
    IFS=';'
    read -a shards <<< "${SHARD_MEMBERS}"
    IFS=$OIFS

    shardNumber=${#shards[@]}
    if [ "$shardNumber" != "$SHARDS" ]; then
      echo "error: the number of shards in SHARD_MEMBERS $shardNumber is not the same with SHARDS $SHARDS"
      exit 1
    fi

    # initialize the config server replica set.
    # the config server replica set does not have the local user.
    configServerName=$SERVICE_NAME"-config"
    InitReplicaSet $configServerName $CONFIG_SERVER_MEMBERS 27019

    # initialize the replica set of each shard
    shard=0
    for s in "${shards[@]}"; do
      shardName=$SERVICE_NAME"-shard"$shard
      # create the shard replicaset and shard local user.
      # https://docs.mongodb.com/v3.4/tutorial/deploy-sharded-cluster-with-keyfile-access-control/#create-the-shard-replica-sets
      # https://docs.mongodb.com/v3.4/core/security-users/#shard-local-users
      CreateReplicaSetAndUser $shardName $s 27018
      shard=$(( $shard + 1 ))
    done

    # start mongos
    mongos --configdb $configServerName/$CONFIG_SERVER_MEMBERS &

    # wait 10s for mongos to initialize
    sleep 10

    # add shard to cluster
    shard=0
    for s in "${shards[@]}"; do
      shardName=$SERVICE_NAME"-shard"$shard

      OIFS=$IFS
      IFS=','
      read -a shardmembers <<< "$s"
      IFS=$OIFS

      output=$(mongo --eval "JSON.stringify(sh._adminCommand( { addShard : \"$shardName/${shardmembers[0]}:27018\" } , true ))")
      echo "add shard $shardName/${shardmembers[0]}:27018 result: $output"
      # analyze the addShard results
      succ=$(echo "$output" | grep "\"ok\":1")
      if [ -z "$succ" ]; then
        echo "init fail $output"
        exit 2
      fi

      shard=$(( $shard + 1 ))
    done

    CreateRootAdminUser "localhost" 27017
  fi
fi

# 3) Tell the firecamp manage server to set MongoDB service initialized.
# set MongoDB service initialized
echo "set MongoDB service initialized"

data="{\"Region\":\"$REGION\",\"Cluster\":\"$CLUSTER\",\"ServiceName\":\"$SERVICE_NAME\",\"ServiceType\":\"$SERVICE_TYPE\"}"
curl -X PUT -H "Content-Type: application/json" -d $data "$MANAGE_SERVER_URL/$OP"

# sleep some time for the server to restart all containers
sleep 10
