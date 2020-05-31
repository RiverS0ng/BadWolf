#!/bin/bash
export GOPATH
GOPATH="`pwd`"
cd src/badwolf
dep ensure
dep status
