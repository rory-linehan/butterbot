#!/usr/bin/env bash

docker build -t jyro-0:5000/butterbot:latest . && \
docker push jyro-0:5000/butterbot:latest && \
kubectl -n jyro run --image jyro-0:5000/butterbot:latest butterbot-test