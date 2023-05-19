#!/usr/bin/env bash

repo=${1:-}

docker build -t ${repo}butterbot:latest .
if [ ! -z "${repo}" ]; then
  docker push ${repo}butterbot:latest
fi
kubectl -n jyro run --image ${repo}butterbot:latest butterbot-test --command -- ./butterbot --log-level=debug