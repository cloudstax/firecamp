#!/bin/bash
set -ex

#CLUSTER="mycluster"
#SERVICE_NAME="myservice"
#POD_NAME="myservice-0"
#DB_TYPE="k8sdb"
#K8S_NAMESPACE="default"
#TESTMODE="false"

if [ -z "$CLUSTER" -o -z "$SERVICE_NAME" -o -z "$POD_NAME" -o -z "$K8S_NAMESPACE" -o -z "$DB_TYPE" ]
then
  echo "error: please input all required environment variables"
  echo "CLUSTER $CLUSTER, SERVICE_NAME $SERVICE_NAME, POD_NAME $POD_NAME, K8S_NAMESPACE $K8S_NAMESPACE, DB_TYPE $DB_TYPE"
  exit 1
fi

/firecamp-initcontainer -cluster=$CLUSTER -service-name=$SERVICE_NAME -member-name=$POD_NAME -dbtype=$DB_TYPE

