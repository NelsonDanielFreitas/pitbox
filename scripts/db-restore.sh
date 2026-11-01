#!/usr/bin/env bash
# F1-14d: restauro dos backups do Postgres local.
#
#   db-restore.sh [--file F] [pitbox|zitadel]      VERIFICA: restaura num contentor descartável
#                                                  (mesma imagem e bootstrap do compose) e mostra as
#                                                  contagens. Não toca no ambiente. É o modo por omissão.
#   CONFIRM=yes db-restore.sh --live [--file F] <pitbox|zitadel>
#                                                  RESTAURA no Postgres do compose: APAGA e recria essa
#                                                  base. Pára o Zitadel antes (se for a base zitadel).
#
# Sem --file usa o dump mais recente de cada base em PITBOX_BACKUP_DIR (por omissão ~/pitbox-backups).
# Dumps cifrados (.age) precisam do `age` e de PITBOX_BACKUP_AGE_IDENTITY.
set -euo pipefail
cd "$(dirname "$0")/.."
PROG=db-restore
. scripts/db-backup-lib.sh

command -v docker >/dev/null || die "docker não disponível"
live=0; file=""; dbs=()
while [ "$#" -gt 0 ]; do
  case "$1" in
    --live) live=1 ;;
    --file) shift; [ "$#" -gt 0 ] || die "--file precisa de um caminho"; file="$1" ;;
    pitbox|zitadel) dbs+=("$1") ;;
    *) die "argumento desconhecido: $1" ;;
  esac
  shift
done
given=${#dbs[@]}
[ "$given" -gt 0 ] || dbs=("${DBS[@]}")
if [ -n "$file" ] && [ "${#dbs[@]}" != 1 ]; then die "--file exige indicar uma só base (pitbox ou zitadel)"; fi
if [ "$live" = 1 ]; then
  [ "$given" = 1 ] || die "--live exige indicar uma só base (pitbox ou zitadel)"
  [ "${CONFIRM:-}" = yes ] || die "--live APAGA a base ${dbs[0]} e restaura o dump. Repete com CONFIRM=yes"
fi

DIR=$(backup_dir)
latest() { ls -1 "$DIR" | grep -E "^$1-[0-9]{8}-[0-9]{6}\.dump(\.age)?$" | sort | tail -1 | sed "s#^#$DIR/#"; }

# Verifica a soma de controlo (se existir) antes de tocar em qualquer base.
check_sum() {
  if [ -f "$1.sha256" ]; then
    (cd "$(dirname "$1")" && sha256sum -c --quiet "$(basename "$1").sha256") || die "soma de controlo de $1 não confere"
  else
    echo "db-restore: AVISO: $1 sem .sha256" >&2
  fi
}

declare -A picked
for db in "${dbs[@]}"; do
  f="${file:-$(latest "$db")}"
  [ -n "$f" ] && [ -f "$f" ] || die "sem dump de $db em $DIR (corre 'make db-backup')"
  check_sum "$f"
  picked[$db]="$f"
done

if [ "$live" = 1 ]; then
  db="${dbs[0]}"
  C=$(pg_container)
  if [ -z "${PITBOX_BACKUP_CONTAINER:-}" ] && [ "$db" = zitadel ]; then
    echo "db-restore: a parar o Zitadel"
    docker compose --project-directory deploy/compose --env-file deploy/compose/.env -f deploy/compose/docker-compose.yml \
      stop zitadel-proxy zitadel-login zitadel-api
  fi
  echo "db-restore: a restaurar $db de ${picked[$db]} (a base atual é APAGADA)"
  restore_db "$C" "$db" "${picked[$db]}"
  row_counts "$C" "$db" | sed 's/^/  /'
  echo "db-restore: OK. Corre 'make up' para voltar a arrancar tudo."
  exit 0
fi

# --- Verificação num contentor descartável (nada do ambiente local é tocado) ---
IMAGE=$(grep -oE 'timescale/timescaledb:[^[:space:]"]+@sha256:[0-9a-f]{64}' deploy/compose/docker-compose.yml | head -1)
[ -n "$IMAGE" ] || die "imagem com digest não encontrada em deploy/compose/docker-compose.yml"
for v in POSTGRES_PASSWORD PITBOX_MIGRATOR_PASSWORD PITBOX_API_PASSWORD ZITADEL_DB_PASSWORD OPENFGA_DB_PASSWORD; do
  export "$v=$(openssl rand -hex 24)"
done
export POSTGRES_USER=postgres
T="pitbox-restore-$$"
trap 'docker rm -f "$T" >/dev/null 2>&1 || true' EXIT
docker run -d --name "$T" --security-opt no-new-privileges --tmpfs /var/lib/postgresql/data \
  -e POSTGRES_USER -e POSTGRES_PASSWORD -e PITBOX_MIGRATOR_PASSWORD -e PITBOX_API_PASSWORD \
  -e ZITADEL_DB_PASSWORD -e OPENFGA_DB_PASSWORD \
  -v "$PWD/deploy/compose/postgres/init:/docker-entrypoint-initdb.d:ro" "$IMAGE" >/dev/null
for _ in $(seq 1 90); do
  docker exec "$T" pg_isready -h 127.0.0.1 -U postgres -d postgres -q 2>/dev/null && break
  sleep 1
done
docker exec "$T" pg_isready -h 127.0.0.1 -U postgres -d postgres -q || die "o contentor de verificação não ficou pronto"

status=0
for db in "${dbs[@]}"; do
  echo "db-restore: a verificar $db (${picked[$db]})"
  restore_db "$T" "$db" "${picked[$db]}"
  counts=$(row_counts "$T" "$db")
  tables=$(printf '%s\n' "$counts" | grep -c . || true)
  rows=$(printf '%s\n' "$counts" | awk -F= '{s+=$2} END{print s+0}')
  if [ "$tables" -eq 0 ]; then echo "db-restore: FALHA: $db restaurada sem tabelas" >&2; status=1; continue; fi
  echo "  $db: $tables tabelas, $rows linhas"
  # Informativo: compara com a base viva, se houver (pode ter mudado desde o backup).
  if live_c=$(pg_container 2>/dev/null) && live_counts=$(row_counts "$live_c" "$db" 2>/dev/null); then
    if [ "$live_counts" = "$counts" ]; then echo "  igual à base viva"; else echo "  difere da base viva (normal se houve escritas depois do backup)"; fi
  fi
done
[ "$status" = 0 ] && echo "db-restore: OK (verificação; o ambiente local não foi tocado)"
exit "$status"
