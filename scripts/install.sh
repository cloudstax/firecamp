#!/bin/sh
set -e

echo "protoc controdb.proto"
protoc -I db/controldb/protocols/ db/controldb/protocols/controldb.proto --go_out=plugins=grpc:db/controldb/protocols

echo "install openmanage-init"
cd syssvc/openmanage-init; go install; cd -

echo "install openmanage-cleanup"
cd syssvc/openmanage-cleanup/; go install; cd -

echo "install openmanage-controldb"
cd syssvc/openmanage-controldb; go install; cd -

echo "install openmanage-dockervolume"
cd syssvc/openmanage-dockervolume; go install; cd -

echo "install openmanage-manageserver"
cd syssvc/openmanage-manageserver; go install; cd -

echo "install openmanage-aws-ecs-mongodb-creation"
cd catalog/mongodb/openmanage-aws-ecs-mongodb-creation; go install; cd -

echo "install openmanage-aws-ecs-postgres-creation"
cd catalog/postgres/openmanage-aws-ecs-postgres-creation; go install; cd -

echo "install openmanage-service-deletion"
cd catalog/openmanage-service-deletion; go install; cd -
