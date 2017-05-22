#!/bin/sh

protoc -I protocols/ protocols/controldb.proto --go_out=plugins=grpc:protocols
