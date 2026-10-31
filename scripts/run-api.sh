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

. scripts/api-env.sh

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
