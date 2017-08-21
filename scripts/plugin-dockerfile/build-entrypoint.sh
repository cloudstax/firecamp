#!/bin/sh

codedir=/go/src/github.com/cloudstax/openmanage

ls -al /go/src/github.com/cloudstax/

# the build process will mount the source code dir to $codedir
if [ ! -d "$codedir" ]; then
  echo "$codedir not exist"
  exit 1
fi

echo "build openmanage-dockervolume"
cd $codedir/syssvc/openmanage-dockervolume
go install --ldflags '-extldflags "-static"'

echo "build openmanage-dockerlogs"
cd $codedir/syssvc/openmanage-dockerlogs
go install --ldflags '-extldflags "-static"'

ls /go/bin/
