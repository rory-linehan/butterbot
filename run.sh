#!/usr/bin/env bash

# bash run.sh jyro-0:5000/

repo=${1:-}

docker build -t ${repo}butterbot:latest .
if [ ! -z "${repo}" ]; then
  docker push ${repo}butterbot:latest
fi
kubectl -n jyro run --image ${repo}butterbot:latest butterbot-test