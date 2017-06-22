#!/bin/sh
set -xe

protoc -I db/controldb/protocols/ db/controldb/protocols/controldb.proto --go_out=plugins=grpc:db/controldb/protocols

cd syscmds/openmanage-init; go install; cd -

cd syscmds/openmanage-cleanup; go install; cd -

cd syscmds/openmanage-controldb; go install; cd -

cd syscmds/openmanage-dockervolume; go install; cd -

cd syscmds/openmanage-manageserver; go install; cd -

cd syscmds/openmanage-service-cli; go install; cd -

cd syscmds/openmanage-service-creation-example; go install; cd -
