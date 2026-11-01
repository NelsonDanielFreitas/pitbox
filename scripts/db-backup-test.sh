#!/usr/bin/env bash
# F1-14d: prova automática de backup e restauro, só com contentores descartáveis (não toca no compose).
#
#  1. origem   : Postgres como o do compose + schema.sql + dados (inclui uma hypertable e RLS);
#  2. backup   : db-backup.sh gera dumps 600 numa pasta 700 fora do repositório, com .sha256;
#  3. restauro : db-restore.sh --live para um SEGUNDO contentor com a base já ocupada (é apagada);
#                contagens por tabela e DDL iguais à origem, RLS e privilégios mantidos;
#  4. verifica : db-restore.sh (modo por omissão) diz "igual à base viva";
#  5. negativos: dump adulterado, pasta dentro do repositório, restauro sem CONFIRM e KEEP inválido falham;
#  6. rotação  : PITBOX_BACKUP_KEEP=1 deixa um dump por base;
#  7. cifra    : com `age` instalado, ida e volta cifrada (saltado se não houver).
set -euo pipefail
cd "$(dirname "$0")/.."
fail() { echo "db-backup-test: $*" >&2; exit 1; }
command -v docker >/dev/null || fail "docker não disponível"

IMAGE=$(grep -oE 'timescale/timescaledb:[^[:space:]"]+@sha256:[0-9a-f]{64}' deploy/compose/docker-compose.yml | head -1)
[ -n "$IMAGE" ] || fail "imagem com digest não encontrada em deploy/compose/docker-compose.yml"
for v in POSTGRES_PASSWORD PITBOX_MIGRATOR_PASSWORD PITBOX_API_PASSWORD ZITADEL_DB_PASSWORD OPENFGA_DB_PASSWORD; do
  export "$v=$(openssl rand -hex 24)"
done
export POSTGRES_USER=postgres

RUN="pitbox-bkptest-$$"; SRC="$RUN-src"; DST="$RUN-dst"
WORK=$(mktemp -d)
cleanup() { docker rm -f "$SRC" "$DST" >/dev/null 2>&1 || true; rm -rf "$WORK"; }
trap cleanup EXIT
start() {
  docker run -d --name "$1" --security-opt no-new-privileges --tmpfs /var/lib/postgresql/data \
    -e POSTGRES_USER -e POSTGRES_PASSWORD -e PITBOX_MIGRATOR_PASSWORD -e PITBOX_API_PASSWORD \
    -e ZITADEL_DB_PASSWORD -e OPENFGA_DB_PASSWORD \
    -v "$PWD/deploy/compose/postgres/init:/docker-entrypoint-initdb.d:ro" "$IMAGE" >/dev/null
}
ready() {
  for _ in $(seq 1 90); do docker exec "$1" pg_isready -h 127.0.0.1 -U postgres -d postgres -q 2>/dev/null && return 0; sleep 1; done
  fail "$1 não ficou pronto"
}
psql_pg() { docker exec -i "$1" psql -X -q -At -v ON_ERROR_STOP=1 -U postgres -d "${2:-pitbox}" "${@:3}"; }

echo "== 1. Origem com dados"
start "$SRC"; start "$DST"; ready "$SRC"; ready "$DST"
docker exec -i "$SRC" psql -X -q -v ON_ERROR_STOP=1 -U pitbox_migrator -d pitbox --single-transaction < backend/db/schema/schema.sql
# Dados coerentes (sem desligar FKs): utilizadores, equipa, dispositivo, gravação e amostras na hypertable.
psql_pg "$SRC" pitbox <<'SQL'
INSERT INTO identity.user_account (idp_subject, email, display_name) VALUES ('sub-1','a@example.test','A'),('sub-2','b@example.test','B');
INSERT INTO identity.team (name, created_by) SELECT 'Equipa teste', id FROM identity.user_account WHERE idp_subject = 'sub-1';
INSERT INTO telemetry.device (team_id, serial_number, hardware_rev) SELECT id, 'SN-1', 'A' FROM identity.team;
INSERT INTO telemetry.recording (team_id, device_id, started_at) SELECT team_id, id, now() FROM telemetry.device;
INSERT INTO telemetry.sample (team_id, device_id, recording_id, ts, rpm)
  SELECT team_id, device_id, id, now() - (i || ' min')::interval, i FROM telemetry.recording, generate_series(1,5) i;
SQL
psql_pg "$SRC" zitadel <<'SQL'
CREATE TABLE public.canary (x int, y text); INSERT INTO public.canary VALUES (1,'um'),(2,'dois'),(3,'três');
ALTER TABLE public.canary OWNER TO zitadel;
SQL
[ "$(psql_pg "$SRC" pitbox -c 'SELECT count(*) FROM identity.user_account')" = 2 ] || fail "semente de user_account falhou"

export PITBOX_BACKUP_DIR="$WORK/backups"
export PITBOX_BACKUP_CONTAINER="$SRC"

echo "== 2. Backup"
bash scripts/db-backup.sh
[ "$(stat -c %a "$PITBOX_BACKUP_DIR")" = 700 ] || fail "a pasta dos backups não é 700"
for f in "$PITBOX_BACKUP_DIR"/*; do
  [ "$(stat -c %a "$f")" = 600 ] || fail "$f não é 600"
done
[ "$(ls "$PITBOX_BACKUP_DIR"/*.dump | wc -l)" = 2 ] || fail "esperava 2 dumps"
[ "$(ls "$PITBOX_BACKUP_DIR"/*.sha256 | wc -l)" = 2 ] || fail "esperava 2 somas de controlo"

echo "== 3. Restauro para uma base já ocupada (segundo contentor)"
# Ocupa o destino com lixo que tem de desaparecer.
psql_pg "$DST" pitbox -c "CREATE TABLE public.lixo(x int)"
export PITBOX_BACKUP_CONTAINER="$DST"
CONFIRM=yes bash scripts/db-restore.sh --live pitbox >/dev/null
CONFIRM=yes bash scripts/db-restore.sh --live zitadel >/dev/null
source scripts/db-backup-lib.sh
for db in pitbox zitadel; do
  [ "$(row_counts "$SRC" "$db")" = "$(row_counts "$DST" "$db")" ] || { diff <(row_counts "$SRC" "$db") <(row_counts "$DST" "$db") >&2 || true; fail "contagens de $db diferem"; }
  [ -n "$(row_counts "$DST" "$db")" ] || fail "$db restaurada sem tabelas"
done
[ "$(psql_pg "$DST" pitbox -c "SELECT to_regclass('public.lixo') IS NULL")" = t ] || fail "o restauro não apagou a base anterior"
[ "$(psql_pg "$DST" pitbox -c 'SELECT count(*) FROM telemetry.sample')" = 5 ] || fail "hypertable sem as 5 linhas"
[ "$(psql_pg "$DST" pitbox -c "SELECT count(*) FROM timescaledb_information.hypertables")" -ge 1 ] || fail "hypertables perdidas"
# DDL, donos e privilégios iguais (pg_dump --schema-only inclui GRANTs, donos e políticas de RLS).
snap() { docker exec "$1" pg_dump -U postgres -d "$2" --schema-only --exclude-schema='_timescaledb_*' --exclude-schema='timescaledb_*' | grep -Ev '^\\(un)?restrict '; }
for db in pitbox zitadel; do
  diff <(snap "$SRC" "$db") <(snap "$DST" "$db") > "$WORK/ddl.diff" || { head -40 "$WORK/ddl.diff" >&2; fail "DDL de $db difere (donos, GRANTs ou RLS)"; }
done
[ "$(psql_pg "$DST" pitbox -c "SELECT relrowsecurity FROM pg_class WHERE oid = 'identity.team'::regclass")" = t ] || fail "RLS perdido em identity.team"
echo "restauro igual à origem"

echo "== 4. Verificação (modo por omissão) contra a base viva"
export PITBOX_BACKUP_CONTAINER="$SRC"
bash scripts/db-restore.sh | tee "$WORK/verify.txt"
[ "$(grep -c 'igual à base viva' "$WORK/verify.txt")" = 2 ] || fail "a verificação não reportou 'igual à base viva' nas duas bases"

echo "== 5. Casos negativos"
d=$(ls "$PITBOX_BACKUP_DIR"/pitbox-*.dump | tail -1)
cp "$d" "$WORK/original.dump"
printf 'x' >> "$d"
if bash scripts/db-restore.sh pitbox >/dev/null 2>&1; then fail "dump adulterado foi aceite"; fi
cp "$WORK/original.dump" "$d"
if bash scripts/db-restore.sh --live pitbox >/dev/null 2>&1; then fail "--live sem CONFIRM=yes foi aceite"; fi
if PITBOX_BACKUP_DIR="$PWD/backups-no-repo" bash scripts/db-backup.sh >/dev/null 2>&1; then fail "pasta dentro do repositório foi aceite"; fi
[ ! -e "$PWD/backups-no-repo" ] || { rmdir "$PWD/backups-no-repo"; fail "criou uma pasta dentro do repositório"; }
if PITBOX_BACKUP_KEEP=0 bash scripts/db-backup.sh >/dev/null 2>&1; then fail "KEEP=0 foi aceite"; fi
if bash scripts/db-backup.sh outra >/dev/null 2>&1; then fail "base desconhecida foi aceite"; fi

echo "== 6. Rotação (KEEP=1)"
sleep 1
PITBOX_BACKUP_KEEP=1 bash scripts/db-backup.sh >/dev/null
[ "$(ls "$PITBOX_BACKUP_DIR"/pitbox-*.dump | wc -l)" = 1 ] || fail "rotação não deixou 1 dump de pitbox"
[ "$(ls "$PITBOX_BACKUP_DIR"/zitadel-*.dump | wc -l)" = 1 ] || fail "rotação não deixou 1 dump de zitadel"
[ "$(ls "$PITBOX_BACKUP_DIR"/*.sha256 | wc -l)" = 2 ] || fail "somas de controlo órfãs"

echo "== 7. Cifra com age"
if command -v age >/dev/null && command -v age-keygen >/dev/null; then
  age-keygen -o "$WORK/key.txt" 2>/dev/null
  chmod 600 "$WORK/key.txt"
  recipient=$(age-keygen -y "$WORK/key.txt")
  export PITBOX_BACKUP_DIR="$WORK/enc"
  PITBOX_BACKUP_AGE_RECIPIENT="$recipient" PITBOX_BACKUP_AGE_IDENTITY="$WORK/key.txt" bash scripts/db-backup.sh >/dev/null
  ls "$PITBOX_BACKUP_DIR"/*.dump.age >/dev/null || fail "sem dumps .age"
  if grep -aq 'PGDMP' "$PITBOX_BACKUP_DIR"/pitbox-*.dump.age; then fail "o dump cifrado contém texto em claro"; fi
  PITBOX_BACKUP_AGE_IDENTITY="$WORK/key.txt" bash scripts/db-restore.sh pitbox | tee "$WORK/enc.txt" >/dev/null
  grep -q 'igual à base viva' "$WORK/enc.txt" || fail "restauro cifrado não coincide"
  if bash scripts/db-restore.sh pitbox >/dev/null 2>&1; then fail "restauro cifrado sem chave foi aceite"; fi
else
  echo "age não instalado: passo de cifra saltado"
fi
echo "db-backup-test: OK"
