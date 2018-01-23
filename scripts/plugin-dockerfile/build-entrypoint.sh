#!/bin/sh

codedir=/go/src/github.com/cloudstax/firecamp

ls -al /go/src/github.com/cloudstax/

# the build process will mount the source code dir to $codedir
if [ ! -d "$codedir" ]; then
  echo "$codedir not exist"
  exit 1
fi

echo "build firecamp-dockervolume"
cd $codedir/syssvc/firecamp-dockervolume
go install --ldflags '-extldflags "-static"'

echo "build firecamp-dockerlog"
cd $codedir/syssvc/firecamp-dockerlog
go install --ldflags '-extldflags "-static"'

ls /go/bin/
