#!/usr/bin/env bash
# Corre o pitbox-api local (127.0.0.1:8081) com traces para o Jaeger.
#
# Se `make zitadel-provision` já correu, liga também o login: lê do deploy/compose/.env SÓ as
# chaves de que a API precisa (nunca exporta o ficheiro inteiro: tem as palavras-passe do
# Postgres, do Zitadel, ...), e dá-lhe a palavra-passe do utilizador ACL do Valkey e a da base
# de dados como pitbox_api. Sem essas chaves, a API arranca sem autenticação (recusa tudo).
set -euo pipefail
cd "$(dirname "$0")/.."
ENV_FILE=deploy/compose/.env

get() { grep -E "^$1=" "$ENV_FILE" 2>/dev/null | head -1 | cut -d= -f2- || true; }

export PITBOX_ENV=local
export OTEL_EXPORTER_OTLP_ENDPOINT="http://127.0.0.1:${OTLP_HTTP_PORT:-4318}"

if [ -f "$ENV_FILE" ] && [ -n "$(get PITBOX_OIDC_CLIENT_ID)" ] && [ -n "$(get PITBOX_OIDC_CLIENT_SECRET)" ]; then
  export PITBOX_OIDC_CLIENT_ID="$(get PITBOX_OIDC_CLIENT_ID)"
  export PITBOX_OIDC_CLIENT_SECRET="$(get PITBOX_OIDC_CLIENT_SECRET)"
  export PITBOX_AUTH_KEYS="$(get PITBOX_AUTH_KEYS)"
  export PITBOX_SYNC_PASS_KEYS="$(get PITBOX_SYNC_PASS_KEYS)"
  if [ -z "${PITBOX_PUBLIC_URL:-}" ]; then
    PITBOX_PUBLIC_URL="$(get PITBOX_PUBLIC_URL)"
    PITBOX_PUBLIC_URL="${PITBOX_PUBLIC_URL:-http://localhost:8081}"
  fi
  export PITBOX_PUBLIC_URL
  export PITBOX_OIDC_ISSUER="http://localhost:$(get ZITADEL_PORT)"; PITBOX_OIDC_ISSUER="${PITBOX_OIDC_ISSUER%:}"
  [ "$PITBOX_OIDC_ISSUER" = "http://localhost" ] && PITBOX_OIDC_ISSUER="http://localhost:8080"
  export PITBOX_VALKEY_ADDR="127.0.0.1:$(get VALKEY_PORT)"; [ "$PITBOX_VALKEY_ADDR" = "127.0.0.1:" ] && PITBOX_VALKEY_ADDR="127.0.0.1:6379"
  export PITBOX_VALKEY_PASSWORD="$(get VALKEY_API_PASSWORD)"
  port="$(get POSTGRES_PORT)"; port="${port:-5432}"
  export PITBOX_DATABASE_URL="postgres://pitbox_api:$(get PITBOX_API_PASSWORD)@127.0.0.1:${port}/pitbox?sslmode=disable"
  if [ -n "$(get PITBOX_OPENFGA_STORE_ID)" ]; then
    export PITBOX_OPENFGA_ADDR="$(get PITBOX_OPENFGA_ADDR)"; PITBOX_OPENFGA_ADDR="${PITBOX_OPENFGA_ADDR:-127.0.0.1:8090}"
    export PITBOX_OPENFGA_PRESHARED_KEY="$(get OPENFGA_PRESHARED_KEY)"
    export PITBOX_OPENFGA_STORE_ID="$(get PITBOX_OPENFGA_STORE_ID)"
    export PITBOX_OPENFGA_MODEL_ID="$(get PITBOX_OPENFGA_MODEL_ID)"
    echo "run-api: openfga ligado (store $(get PITBOX_OPENFGA_STORE_ID))" >&2
  fi
  echo "run-api: login ligado (issuer $PITBOX_OIDC_ISSUER)" >&2
else
  echo "run-api: sem login (corre 'make zitadel-provision' para o ligar); os RPC são todos recusados" >&2
fi

# Aviso (não bloqueia): uma branch com migrações novas precisa de `make db-migrate`, senão a
# API arranca mas os RPC falham com "column ... does not exist". Só lê o estado (migrate status).
warn_pending_migrations() {
  command -v atlas >/dev/null 2>&1 || return 0
  local password port pending
  password="$(get PITBOX_MIGRATOR_PASSWORD)"
  [ -n "$password" ] || return 0
  port="$(get POSTGRES_PORT)"; port="${port:-5432}"
  pending="$(DATABASE_URL="postgres://pitbox_migrator:${password}@127.0.0.1:${port}/pitbox?sslmode=disable" \
    timeout 10 atlas migrate status --env local 2>/dev/null | sed -n 's/^ *-- Pending Files: *\([0-9][0-9]*\).*/\1/p' | head -1 || true)"
  if [ -n "$pending" ] && [ "$pending" -gt 0 ]; then
    echo "run-api: AVISO: $pending migração(ões) por aplicar na base local; corre 'make db-migrate' (sem isto os RPC podem falhar)" >&2
  fi
}
warn_pending_migrations

cd backend && exec go run ./cmd/api
