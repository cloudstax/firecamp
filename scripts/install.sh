#!/bin/sh
set -xe

protoc -I db/controldb/protocols/ db/controldb/protocols/controldb.proto --go_out=plugins=grpc:db/controldb/protocols

cd syssvc/openmanage-init; go install; cd -

cd syssvc/openmanage-cleanup; go install; cd -

cd syssvc/openmanage-controldb; go install; cd -

cd syssvc/openmanage-dockervolume; go install; cd -

cd syssvc/openmanage-manageserver; go install; cd -

cd syssvc/openmanage-catalogservice-cli; go install; cd -

cd syssvc/openmanage-service-cli; go install; cd -
