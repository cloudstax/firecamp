#!/bin/bash

# This script does 3 things:
# 1) Initialize the MongoDB ReplicaSet.
# 2) Create the admin user.
# 3) Tell the openmanage manage server to set MongoDB service initialized.

# could not "set -e", which will exit if any command fails.
# The task may end at any time. For example, node crashes before set service initialized.
# The MongoDB init will retry the task, the mongo admin command may return "already initialized".
# "set -e" will take this as error and exit.

# The mongo commands, refer to https://docs.mongodb.com/manual/reference/command/

#export REGION="us-west-1"
#export CLUSTER="default"
#export MANAGE_SERVER_URL="openmanage-manageserver.cluster-openmanage.com:27040"
#export SERVICE_NAME="openmanage-mongodb-1rep"
#export SERVICE_MASTER="openmanage-mongodb-1rep-0.cluster-openmanage.com"
#export SERVICE_MEMBERS="openmanage-mongodb-1rep-1.cluster-openmanage.com,openmanage-mongodb-1rep-2.cluster-openmanage.com"
#export REPLICA_SET_NAME="rep-rs0"
#export SERVICE_TYPE="mongodb"
#export OP="Catalog-Set-Service-Init"

# check the environment parameters
if [ -z "$REGION" ] || [ -z "$CLUSTER" ] || [ -z "$MANAGE_SERVER_URL" ] || [ -z "$SERVICE_NAME" ] || [ -z "$SERVICE_MASTER" ] || [ -z "$REPLICA_SET_NAME" ] || [ -z "$SERVICE_TYPE" ] || [ -z "$OP" ]
then
  echo "error: please pass all required environment variables" >&2
  exit 1
fi
# TODO should fetch ADMIN_PASSWORD from the manage server.
# On Set-Service-Initialized, the manage server will delete the ADMIN_PASSWORD.
if [ -z "$ADMIN_USER" ] || [ -z "$ADMIN_PASSWORD" ]
then
  echo "error: please pass the admin user and password" >&2
  exit 1
fi

# 1) Initialize the MongoDB ReplicaSet.
# get all members
OIFS=$IFS
IFS=','
read -a members <<< "${SERVICE_MEMBERS}"
IFS=$OIFS

# generate the MongoDB Replicaset config
cfg="{_id: '$REPLICA_SET_NAME', members: [ {_id: 0, host: '$SERVICE_MASTER'}"
i=1
for m in "${members[@]}"
do
  cfg+=", {_id:$i, host: '$m'}"
  i=$(( $i + 1 ))
done
cfg+=" ] }"

echo $cfg

# initialize MongoDB Replicaset
output=$(mongo --host $SERVICE_MASTER --eval "JSON.stringify(db.adminCommand({'replSetInitiate' : $cfg}))")
echo "$output"

# example output
#output="MongoDB shell version: 3.2.10 connecting to: f25b11c002e3 {\"info\":\"try querying local.system.replset to see current configuration\",\"ok\":0,\"errmsg\":\"already initialized\",\"code\":23}"
#output="MongoDB shell version: 3.2.10 connecting to: f25b11c002e3 {"ok":1}"
#output="MongoDB shell version: 3.2.10 connecting to: openmanage-mongodb-1rep.cluster-openmanage.com/test 2017-04-10T20:13:33.911+0000 I NETWORK [thread1] getaddrinfo(\"openmanage-mongodb-1rep.cluster-openmanage.com\") failed: Name or service not known 2017-04-10T20:13:33.921+0000 E QUERY [thread1] Error: couldn't initialize connection to host openmanage-mongodb-1rep.cluster-openmanage.com, address is invalid : connect@src/mongo/shell/mongo.js:231:14 @(connect):1:6"


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
  # most likely the DNS record is not updated yet. wait some time
  sleep 10
  echo $output >&2
  exit 2
fi

echo "MongoDB ReplicaSet initialized"

# 2) Create the admin user.
# wait 10 seconds for MongoDB to stabilize
sleep 10

echo "start creating the admin user"

# create the admin user
#mongo  --host $SERVICE_MASTER << EOF
#use admin;
#db.createUser(
#  {
#    user: "${ADMIN_USER}",
#    pwd: "${ADMIN_PASSWORD}",
#    roles: [ { role: "root", db: "admin" } ]
#  }
#);
#EOF
createUser="{ createUser: \"${ADMIN_USER}\", pwd: \"${ADMIN_PASSWORD}\", roles: [ { role: \"root\", db: \"admin\" } ] }"
output=$(mongo --host $SERVICE_MASTER --eval "JSON.stringify(db.adminCommand($createUser))")
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

echo "admin user $ADMIN_USER created"


# 3) Tell the openmanage manage server to set MongoDB service initialized.
# set MongoDB service initialized
echo "set MongoDB service initialized"

data="{\"Region\":\"$REGION\",\"Cluster\":\"$CLUSTER\",\"ServiceName\":\"$SERVICE_NAME\",\"ServiceType\":\"$SERVICE_TYPE\"}"
curl -X PUT -H "Content-Type: application/json" -d $data "$MANAGE_SERVER_URL/$OP"

# sleep some time for the server to restart all containers
sleep 10
