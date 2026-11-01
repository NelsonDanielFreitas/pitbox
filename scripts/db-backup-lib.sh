#!/usr/bin/env bash
# Funções partilhadas por db-backup.sh, db-restore.sh e db-backup-test.sh (não se executa).
# F1-14d: backup e restauro do Postgres local (bases `pitbox`, com o esquema `fga` do OpenFGA, e
# `zitadel`). Tudo corre DENTRO do contentor do Postgres (o pg_dump/pg_restore têm a mesma versão
# do servidor); os ficheiros vivem no anfitrião, numa pasta fora do volume Docker.

DBS=(pitbox zitadel)

die() { echo "${PROG:-db-backup}: $*" >&2; exit 1; }

# Pasta dos backups: 700, nunca dentro do repositório (os dumps têm emails e hashes).
backup_dir() {
  local dir="${PITBOX_BACKUP_DIR:-$HOME/pitbox-backups}"
  case "$(realpath -m "$dir")/" in
    "$(realpath -m "$PWD")"/*) die "PITBOX_BACKUP_DIR ($dir) está dentro do repositório: usa uma pasta fora dele" ;;
  esac
  (umask 077; mkdir -p "$dir"); chmod 700 "$dir"
  printf '%s' "$dir"
}

# Contentor do Postgres: PITBOX_BACKUP_CONTAINER (testes) ou o serviço `postgres` do compose local.
pg_container() {
  if [ -n "${PITBOX_BACKUP_CONTAINER:-}" ]; then printf '%s' "$PITBOX_BACKUP_CONTAINER"; return; fi
  local ids
  ids=$(docker ps -q --filter label=com.docker.compose.project=pitbox --filter label=com.docker.compose.service=postgres)
  [ "$(printf '%s\n' "$ids" | grep -c .)" = 1 ] || die "contentor postgres do compose não encontrado (corre 'make up')"
  printf '%s' "$ids"
}

valid_db() { local d; for d in "${DBS[@]}"; do [ "$d" = "$1" ] && return 0; done; return 1; }

# Cifra opcional com age: PITBOX_BACKUP_AGE_RECIPIENT (chave pública) no backup; no restauro,
# PITBOX_BACKUP_AGE_IDENTITY (ficheiro da chave privada, fora do repositório).
age_needed() { [ -n "${PITBOX_BACKUP_AGE_RECIPIENT:-}" ]; }

# Imprime o dump em claro de $1 (decifra se terminar em .age).
cat_dump() {
  case "$1" in
    *.age)
      command -v age >/dev/null || die "o ficheiro está cifrado e o 'age' não está instalado"
      [ -n "${PITBOX_BACKUP_AGE_IDENTITY:-}" ] || die "define PITBOX_BACKUP_AGE_IDENTITY (ficheiro da chave privada age)"
      age -d -i "$PITBOX_BACKUP_AGE_IDENTITY" "$1" ;;
    *) cat "$1" ;;
  esac
}

# Contagem de linhas por tabela (esquemas de dados; fora os internos do TimescaleDB).
row_counts() { # $1=contentor $2=base
  docker exec -i "$1" psql -X -q -At -U postgres -d "$2" <<'SQL'
SELECT table_schema || '.' || table_name || '=' ||
       (xpath('/row/c/text()', query_to_xml(format('SELECT count(*) AS c FROM %I.%I', table_schema, table_name), false, true, '')))[1]::text
FROM information_schema.tables
WHERE table_type = 'BASE TABLE'
  AND table_schema NOT IN ('pg_catalog', 'information_schema')
  AND table_schema NOT LIKE '\_timescaledb%' AND table_schema NOT LIKE 'timescaledb%'
ORDER BY 1;
SQL
}

# Dono de cada base (tem de coincidir com o bootstrap em deploy/compose/postgres/init).
db_owner() { case "$1" in pitbox) echo pitbox_owner ;; zitadel) echo zitadel ;; esac; }

# Recria a base $2 em $1 e restaura o dump (stdin do ficheiro $3). Apaga o que lá estava.
# TimescaleDB: a extensão tem de existir e o restauro tem de ficar entre
# timescaledb_pre_restore() e timescaledb_post_restore(); sem isso falham as hypertables.
restore_db() { # $1=contentor $2=base $3=ficheiro
  local c="$1" db="$2" file="$3" owner
  owner=$(db_owner "$db")
  docker exec -i "$c" psql -X -q -v ON_ERROR_STOP=1 -U postgres -d postgres \
    -c "DROP DATABASE IF EXISTS \"$db\" WITH (FORCE)" -c "CREATE DATABASE \"$db\" OWNER \"$owner\""
  if [ "$db" = pitbox ]; then
    docker exec -i "$c" psql -X -q -v ON_ERROR_STOP=1 -U postgres -d "$db" \
      -c "CREATE EXTENSION IF NOT EXISTS timescaledb" -c "SELECT timescaledb_pre_restore()" >/dev/null
  fi
  # Sem --no-owner/--no-acl: donos e privilégios (RLS, GRANTs) fazem parte do que se restaura; os
  # papéis existem porque o bootstrap do compose os cria. --exit-on-error: nada de restauro parcial.
  if ! cat_dump "$file" | docker exec -i "$c" pg_restore -U postgres -d "$db" --exit-on-error; then
    die "pg_restore falhou para $db"
  fi
  if [ "$db" = pitbox ]; then
    docker exec -i "$c" psql -X -q -v ON_ERROR_STOP=1 -U postgres -d "$db" -c "SELECT timescaledb_post_restore()" >/dev/null
  fi
}
