#!/bin/bash
set -e
docker build --load -t webtor-local:latest /srv/self-hosted
docker rm -f webtor-test
docker run -d --name webtor-test -p 8080:8080 -p 8092:8092 -v /srv/self-hosted/data:/data -v /srv/self-hosted/postgres:/pgdata -v /srv/self-hosted/data/redis:/var/lib/redis --env-file /srv/self-hosted/custom.env webtor-local:latest
