#!/bin/sh
set -ex

export TOPWD="$(pwd)"

org=$1
version=$2

system="firecamp"

if [ "$FIRECAMP_BUCKET" = "" ]; then
    bucket="$org"
else
    bucket="$FIRECAMP_BUCKET/"
fi

cd ./packaging/aws-cloudformation
aws s3 sync . s3://$bucket$system/releases/$version/templates/ --exclude \*.sh --delete
cd -
