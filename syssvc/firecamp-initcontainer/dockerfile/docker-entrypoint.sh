#!/bin/bash
set -ex

#OP="init|stop"
#CLUSTER="mycluster"
#SERVICE_NAME="myservice"
#POD_NAME="myservice-0"

if [ -z "$OP" -o -z "$CLUSTER" -o -z "$SERVICE_NAME" -o -z "$POD_NAME" ]
then
  echo "error: please input all required environment variables"
  echo "OP $OP, CLUSTER $CLUSTER, SERVICE_NAME $SERVICE_NAME, POD_NAME $POD_NAME"
  exit 1
fi

MEMBER_INDEX=$(echo $POD_NAME | awk -F "-" '{ print $2 }')

/firecamp-initcontainer -op=$OP -cluster=$CLUSTER -service-name=$SERVICE_NAME -member-index=$MEMBER_INDEX

