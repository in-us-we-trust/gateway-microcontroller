#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

docker build -t gateway-microcontroller:local .
docker rm -f gateway-microcontroller 2>/dev/null || true
docker run --rm --name gateway-microcontroller \
  --env-file .env \
  -p 8080:8080 \
  -v "$(pwd)/debug-chunks:/app/debug-chunks" \
  -v "$(pwd)/certs:/certs:ro" \
  gateway-microcontroller:local
