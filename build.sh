#!/usr/bin/env bash

go get gopkg.in/yaml.v2 github.com/davecgh/go-spew/spew
go build -o butterbot butterbot.go && \
sudo chmod +x butterbot
