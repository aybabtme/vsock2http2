#!/usr/bin/env bash

protoc -I example/grpcping example/grpcping/api.proto --go_out=plugins=grpc:example/grpcping
