#!/usr/bin/env bash
# F1-14d: backup do Postgres local (bases `pitbox` e `zitadel`) para uma pasta FORA do volume Docker.
#
#   db-backup.sh [pitbox|zitadel]   (sem argumento: as duas)
#
# Ambiente (opcional):
#   PITBOX_BACKUP_DIR            pasta dos backups (por omissão ~/pitbox-backups; 700; recusa o repositório)
#   PITBOX_BACKUP_KEEP           quantos manter por base (por omissão 14)
#   PITBOX_BACKUP_AGE_RECIPIENT  chave pública age: cifra os dumps (precisa do binário `age`)
#
# Cada dump é `pg_dump -Fc`, escrito com permissões 600, verificado com `pg_restore --list` e acompanhado
# de um .sha256. Os dumps têm emails e hashes: nunca vão para o Git (o .gitignore e o gitleaks cobrem-no).
#
# ATENÇÃO: o dump do Zitadel só é utilizável com a ZITADEL_MASTERKEY do deploy/compose/.env (e a app
# com as chaves PITBOX_AUTH_KEYS / PITBOX_SYNC_PASS_KEYS). Guarda o .env noutro sítio seguro, NÃO ao lado
# dos dumps: este script não o copia.
set -euo pipefail
cd "$(dirname "$0")/.."
PROG=db-backup
. scripts/db-backup-lib.sh

command -v docker >/dev/null || die "docker não disponível"
if age_needed; then command -v age >/dev/null || die "PITBOX_BACKUP_AGE_RECIPIENT definido mas o 'age' não está instalado"; fi
KEEP="${PITBOX_BACKUP_KEEP:-14}"
case "$KEEP" in ''|*[!0-9]*|0) die "PITBOX_BACKUP_KEEP tem de ser um inteiro positivo" ;; esac

DIR=$(backup_dir)
C=$(pg_container)
if [ "$#" -gt 1 ]; then die "uso: db-backup.sh [pitbox|zitadel]"; fi
if [ "$#" = 1 ]; then valid_db "$1" || die "base desconhecida: $1 (pitbox ou zitadel)"; targets=("$1"); else targets=("${DBS[@]}"); fi

stamp=$(date +%Y%m%d-%H%M%S)
for db in "${targets[@]}"; do
  out="$DIR/$db-$stamp.dump"; age_needed && out="$out.age"
  tmp="$out.partial"
  rm -f "$tmp"
  trap 'rm -f "$tmp"' EXIT
  (
    umask 077
    if age_needed; then
      docker exec -i "$C" pg_dump -U postgres -Fc -d "$db" | age -r "$PITBOX_BACKUP_AGE_RECIPIENT" -o "$tmp"
    else
      docker exec -i "$C" pg_dump -U postgres -Fc -d "$db" > "$tmp"
    fi
  )
  # Fail closed: um dump vazio ou ilegível não conta como backup.
  [ -s "$tmp" ] || die "$db: dump vazio"
  if age_needed; then
    age_ident="${PITBOX_BACKUP_AGE_IDENTITY:-}"
    if [ -n "$age_ident" ]; then
      age -d -i "$age_ident" "$tmp" | docker exec -i "$C" pg_restore --list >/dev/null || die "$db: dump ilegível"
    fi
  else
    docker exec -i "$C" pg_restore --list < "$tmp" >/dev/null || die "$db: dump ilegível (pg_restore --list falhou)"
  fi
  chmod 600 "$tmp"
  mv "$tmp" "$out"
  trap - EXIT
  (cd "$DIR" && umask 077 && sha256sum "$(basename "$out")" > "$(basename "$out").sha256")
  chmod 600 "$out.sha256"
  echo "db-backup: $db -> $out ($(wc -c < "$out") bytes)"

  # Rotação: só ficheiros com o nosso padrão, os mais antigos primeiro.
  mapfile -t old < <(ls -1 "$DIR" | grep -E "^$db-[0-9]{8}-[0-9]{6}\.dump(\.age)?$" | sort | head -n -"$KEEP")
  for f in "${old[@]}"; do
    [ -n "$f" ] || continue
    rm -f -- "$DIR/$f" "$DIR/$f.sha256"
    echo "db-backup: rotação: removido $f"
  done
done
echo "db-backup: OK. Lembra-te: o .env (ZITADEL_MASTERKEY, chaves da API) tem de ser guardado à parte."
