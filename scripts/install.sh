#!/bin/sh
set -xe

protoc -I db/controldb/protocols/ db/controldb/protocols/controldb.proto --go_out=plugins=grpc:db/controldb/protocols

cd syssvc/firecamp-controldb; go install; cd -

# TODO remove the dockervolume install when switch from rpm to plugin
cd syssvc/firecamp-dockervolume; go install; cd -

cd syssvc/firecamp-manageserver; go install; cd -

cd syssvc/firecamp-service-cli; go install; cd -

# these 3 commands are example commands
cd syssvc/examples/firecamp-init; go install; cd -
cd syssvc/examples/firecamp-cleanup; go install; cd -
cd syssvc/examples/firecamp-service-creation-example; go install; cd -
