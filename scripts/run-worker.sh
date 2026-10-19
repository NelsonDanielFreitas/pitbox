#!/usr/bin/env bash
# Corre o pitbox-worker (poller da outbox) contra a infraestrutura local.
#
# Lê do deploy/compose/.env SÓ as chaves de que o worker precisa (nunca exporta o ficheiro
# inteiro: tem as palavras-passe do Postgres, do Zitadel, ...). O worker liga-se como
# pitbox_api, tal como a API.
set -euo pipefail
cd "$(dirname "$0")/.."
ENV_FILE=deploy/compose/.env

get() { grep -E "^$1=" "$ENV_FILE" 2>/dev/null | head -1 | cut -d= -f2- || true; }

export PITBOX_ENV=local
export OTEL_SERVICE_NAME=pitbox-worker
export OTEL_EXPORTER_OTLP_ENDPOINT="http://127.0.0.1:${OTLP_HTTP_PORT:-4318}"

if [ -f "$ENV_FILE" ]; then
  port="$(get POSTGRES_PORT)"; port="${port:-5432}"
  export PITBOX_DATABASE_URL="postgres://pitbox_api:$(get PITBOX_API_PASSWORD)@127.0.0.1:${port}/pitbox?sslmode=disable"
  addr="$(get PITBOX_OPENFGA_ADDR)"; export PITBOX_OPENFGA_ADDR="${addr:-127.0.0.1:8090}"
  export PITBOX_OPENFGA_PRESHARED_KEY="$(get OPENFGA_PRESHARED_KEY)"
  if [ -n "$(get PITBOX_OPENFGA_STORE_ID)" ]; then
    export PITBOX_OPENFGA_STORE_ID="$(get PITBOX_OPENFGA_STORE_ID)"
    export PITBOX_OPENFGA_MODEL_ID="$(get PITBOX_OPENFGA_MODEL_ID)"
    echo "run-worker: openfga ligado (store $(get PITBOX_OPENFGA_STORE_ID))" >&2
  fi
  echo "run-worker: base de dados ligada (127.0.0.1:${port})" >&2
else
  echo "run-worker: falta $ENV_FILE (corre 'make init' e 'make up')" >&2
fi

cd backend && exec go run ./cmd/worker
