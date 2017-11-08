#!/bin/sh
set -xe

export TOPWD="$(pwd)"
cd ${TOPWD}/vendor/lambda-python-requests
cp ${TOPWD}/packaging/aws-cloudformation/redis/redis.py .
zip -r $GOPATH/bin/firecamp-lambda-redis.zip .
rm -f redis.py
cd -


