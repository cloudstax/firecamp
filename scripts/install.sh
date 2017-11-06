#!/bin/sh
set -xe

protoc -I db/controldb/protocols/ db/controldb/protocols/controldb.proto --go_out=plugins=grpc:db/controldb/protocols

cd syssvc/firecamp-controldb; go install; cd -

cd syssvc/firecamp-dockervolume; go install; cd -

cd syssvc/firecamp-manageserver; go install; cd -

cd syssvc/firecamp-service-cli; go install; cd -

cd syssvc/firecamp-swarminit; go install; cd -

cd $GOPATH/bin; tar -zcf firecamp-service-cli.tgz firecamp-service-cli; cd -
cd $GOPATH/bin; tar -zcf firecamp-swarminit.tgz firecamp-swarminit; cd -

export TOPWD="$(pwd)"
cd ${TOPWD}/vendor/lambda-python-requests
cp ${TOPWD}/packaging/aws-cloudformation/redis.py .
zip -r $GOPATH/bin/lambdaredis.zip .
rm -f redis.py
cd -

# these 3 commands are example commands
cd syssvc/examples/firecamp-init; go install; cd -
cd syssvc/examples/firecamp-cleanup; go install; cd -
cd syssvc/examples/firecamp-service-creation-example; go install; cd -
