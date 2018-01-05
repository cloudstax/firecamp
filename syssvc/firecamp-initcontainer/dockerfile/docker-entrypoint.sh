#!/bin/bash
set -e

#OP="init|stop"
#CLUSTER="mycluster"
#SERVICE_NAME="myservice"

if [ -z "$OP" -o -z "$CLUSTER" -o -z "$SERVICE_NAME" ]
then
  echo "error: please input all required environment variables"
  echo "OP $OP, CLUSTER $CLUSTER, SERVICE_NAME $SERVICE_NAME"
  exit 1
fi

hname=$(hostname)
echo $hname
MEMBER_INDEX=0
if [[ $hname =~ -([0-9]+)$ ]]
then
  MEMBER_INDEX=${BASH_REMATCH[1]}

  /firecamp-initcontainer -op=$OP -cluster=$CLUSTER -service-name=$SERVICE_NAME -member-index=$MEMBER_INDEX
else
  echo "error: invalid hostname $hname"
  exit 1
fi

