#!/usr/bin/env bash
# F0-06: testes da base de dados em contentores descartáveis (ADR 0010, 0011).
#
#  1. migrations : sobe um Postgres como o do compose (mesmo digest e mesmo bootstrap) e aplica
#                  backend/db/migrations com o Atlas, ligado como pitbox_migrator.
#  2. schema     : noutro contentor igual, aplica backend/db/schema/schema.sql.
#  3. testes     : SQL de RLS, restrições, estrutura e privilégios na base migrada, como pitbox_api.
#  4. desvio     : as duas bases têm de ser idênticas (pg_dump + papéis + jobs). Falha se algum dump
#                  vier vazio: fail closed.
#
# Sem segredos: palavras-passe aleatórias por execução, portas só em 127.0.0.1 (efémeras).
set -euo pipefail
cd "$(dirname "$0")/.."

fail() { echo "db-test: $*" >&2; exit 1; }
command -v docker >/dev/null || fail "docker não disponível"
command -v atlas >/dev/null || fail "atlas não disponível (mise install)"

# Mesma imagem (tag + digest) que o compose: o Renovate atualiza-a num sítio só.
IMAGE=$(grep -oE 'timescale/timescaledb:[^[:space:]"]+@sha256:[0-9a-f]{64}' deploy/compose/docker-compose.yml | head -1)
[ -n "$IMAGE" ] || fail "imagem com digest não encontrada em deploy/compose/docker-compose.yml"

# Palavras-passe aleatórias vão por variáveis de ambiente (não aparecem na linha de comandos).
for v in POSTGRES_PASSWORD PITBOX_MIGRATOR_PASSWORD PITBOX_API_PASSWORD ZITADEL_DB_PASSWORD OPENFGA_DB_PASSWORD; do
  export "$v=$(openssl rand -hex 24)"
done
export POSTGRES_USER=postgres

RUN_ID="pitbox-dbtest-$$"
MIG="$RUN_ID-migrations"
SCH="$RUN_ID-schema"
WORK=$(mktemp -d)
cleanup() { docker rm -f "$MIG" "$SCH" >/dev/null 2>&1 || true; rm -rf "$WORK"; }
trap cleanup EXIT

start() {
  docker run -d --name "$1" --security-opt no-new-privileges --tmpfs /var/lib/postgresql/data \
    -e POSTGRES_USER -e POSTGRES_PASSWORD -e PITBOX_MIGRATOR_PASSWORD -e PITBOX_API_PASSWORD \
    -e ZITADEL_DB_PASSWORD -e OPENFGA_DB_PASSWORD \
    -v "$PWD/deploy/compose/postgres/init:/docker-entrypoint-initdb.d:ro" \
    -p 127.0.0.1::5432 "$IMAGE" >/dev/null
}
wait_ready() {
  # -h 127.0.0.1: o servidor temporário do init só escuta no socket Unix.
  for _ in $(seq 1 90); do
    docker exec "$1" pg_isready -h 127.0.0.1 -U postgres -d postgres -q 2>/dev/null && return 0
    sleep 1
  done
  docker logs --tail 40 "$1" >&2 || true
  fail "$1 não ficou pronto"
}
# psql dentro do contentor (socket Unix: confiança local da imagem); $1=contentor $2=utilizador, stdin=SQL
psql_as() { docker exec -i "$1" psql -X -q -v ON_ERROR_STOP=1 -U "$2" -d pitbox "${@:3}"; }

echo "== 0. Verificações offline"
atlas migrate validate --env local
python3 -I scripts/db-lint.py "${DB_LINT_BASE:-main}"

echo "== 1. Contentores ($IMAGE)"
start "$MIG"; start "$SCH"
wait_ready "$MIG"; wait_ready "$SCH"

echo "== 2. Migrações (Atlas, como pitbox_migrator)"
PORT=$(docker port "$MIG" 5432/tcp | head -1 | sed 's/.*://')
[ -n "$PORT" ] || fail "porta do contentor desconhecida"
export DATABASE_URL="postgres://pitbox_migrator:${PITBOX_MIGRATOR_PASSWORD}@127.0.0.1:${PORT}/pitbox?sslmode=disable"
# --allow-dirty: o bootstrap já criou objetos que as migrações não gerem (esquema fga do OpenFGA,
# extensões); sem a flag o Atlas recusa uma base "não vazia".
atlas migrate apply --env local --allow-dirty
atlas migrate status --env local | tee "$WORK/status.txt"
grep -q "Status:.*OK" "$WORK/status.txt" || fail "o Atlas não reporta a base como atualizada"
unset DATABASE_URL

echo "== 3. schema.sql (como pitbox_migrator)"
psql_as "$SCH" pitbox_migrator --single-transaction < backend/db/schema/schema.sql

echo "== 4. Testes SQL (base migrada, como pitbox_api)"
for t in rls_and_constraints_test structure_test login_test; do
  echo "-- $t"
  psql_as "$MIG" pitbox_api < "backend/db/tests/$t.sql"
done
echo "-- default_privileges_test"
psql_as "$MIG" postgres < backend/db/tests/default_privileges_test.sql
echo "-- os mesmos testes sobre schema.sql"
for t in rls_and_constraints_test structure_test login_test; do psql_as "$SCH" pitbox_api < "backend/db/tests/$t.sql"; done

echo "== 5. Desvio entre migrações e schema.sql"
snapshot() {
  # pg_dump do próprio contentor (mesma versão do servidor); sem --no-owner: o dono é parte do que se verifica.
  # Fora: internos do TimescaleDB (chunks e jobs variam) e a tabela de revisões do Atlas.
  docker exec "$1" pg_dump -U postgres -d pitbox --schema-only \
    --exclude-schema='_timescaledb_*' --exclude-schema='timescaledb_*' --exclude-schema='atlas_schema_revisions' \
    | grep -Ev '^\\(un)?restrict ' > "$2.dump"
  psql_as "$1" postgres < backend/db/tests/drift_snapshot.sql > "$2.state"
  [ "$(wc -l < "$2.dump")" -gt 1000 ] || fail "dump de $1 suspeito (${2}.dump tem poucas linhas)"
  grep -q 'CREATE TABLE identity.team ' "$2.dump" || fail "dump de $1 não contém identity.team"
  grep -q '^role|pitbox_owner' "$2.state" || fail "estado de $1 sem papéis"
}
snapshot "$MIG" "$WORK/mig"
snapshot "$SCH" "$WORK/sch"
for k in dump state; do
  if ! diff -u "$WORK/mig.$k" "$WORK/sch.$k" > "$WORK/diff.$k"; then
    head -80 "$WORK/diff.$k" >&2
    fail "desvio ($k): as migrações e o schema.sql produzem bases diferentes"
  fi
done
echo "sem desvio ($(wc -l < "$WORK/mig.dump") linhas de DDL comparadas)"

echo "db-test: OK"
