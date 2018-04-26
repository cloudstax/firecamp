#!/bin/sh
set -xe

cd syssvc/firecamp-dockervolume; go install; cd -
cd syssvc/firecamp-dockerlog; go install; cd -

cd syssvc/firecamp-manageserver; go install; cd -

cd syssvc/firecamp-service-cli; go install; cd -

cd syssvc/firecamp-swarminit; go install; cd -

cd syssvc/firecamp-selectmember; go install; cd -
cd syssvc/firecamp-getserviceconf; go install; cd -

cd $GOPATH/bin; tar -zcf firecamp-service-cli.tgz firecamp-service-cli; cd -
cd $GOPATH/bin; tar -zcf firecamp-swarminit.tgz firecamp-swarminit; cd -

cd containersvc/k8s/firecamp-initcontainer/; go install; cd -
cd containersvc/k8s/firecamp-stopcontainer/; go install; cd -

# tools
cd syssvc/tools/firecamp-volume-replace; go install; cd -
cd $GOPATH/bin; tar -zcf firecamp-volume-replace.tgz firecamp-volume-replace; cd -

# example commands
cd syssvc/examples/firecamp-service-creation-example; go install; cd -
