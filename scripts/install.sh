#!/bin/sh
set -xe

protoc -I db/controldb/protocols/ db/controldb/protocols/controldb.proto --go_out=plugins=grpc:db/controldb/protocols

cd syssvc/openmanage-controldb; go install; cd -

# TODO remove the dockervolume install when switch from rpm to plugin
cd syssvc/openmanage-dockervolume; go install; cd -

cd syssvc/openmanage-manageserver; go install; cd -

cd syssvc/openmanage-service-cli; go install; cd -

# these 3 commands are example commands
cd syssvc/examples/openmanage-init; go install; cd -
cd syssvc/examples/openmanage-cleanup; go install; cd -
cd syssvc/examples/openmanage-service-creation-example; go install; cd -
