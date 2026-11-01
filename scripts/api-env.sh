#!/usr/bin/env bash
# Ambiente do pitbox-api local, partilhado por run-api.sh e api-smoke.sh (não se executa).
#
# Lê do deploy/compose/.env SÓ as chaves de que a API precisa (nunca exporta o ficheiro
# inteiro: tem as palavras-passe do Postgres, do Zitadel, ...). O Valkey local exige
# autenticação (ACL): sem a palavra-passe a API não liga e a configuração parcial é recusada
# de propósito (tudo ou nada), por isso as credenciais vão todas juntas.
# Define LOGIN_CONFIGURED=1 se o login ficou configurado (precisa de `make zitadel-provision`).
ENV_FILE=deploy/compose/.env

get() { grep -E "^$1=" "$ENV_FILE" 2>/dev/null | head -1 | cut -d= -f2- || true; }

export PITBOX_ENV=local
export OTEL_EXPORTER_OTLP_ENDPOINT="http://127.0.0.1:${OTLP_HTTP_PORT:-4318}"

if [ -f "$ENV_FILE" ] && [ -n "$(get PITBOX_OIDC_CLIENT_ID)" ] && [ -n "$(get PITBOX_OIDC_CLIENT_SECRET)" ]; then
  export PITBOX_OIDC_CLIENT_ID="$(get PITBOX_OIDC_CLIENT_ID)"
  export PITBOX_OIDC_CLIENT_SECRET="$(get PITBOX_OIDC_CLIENT_SECRET)"
  export PITBOX_AUTH_KEYS="$(get PITBOX_AUTH_KEYS)"
  export PITBOX_SYNC_PASS_KEYS="$(get PITBOX_SYNC_PASS_KEYS)"
  # Perfil telemóvel (ADR 0029): PITBOX_TAILNET_HOST no .env, validado por scripts/tailnet.py
  # (uma regra só). Sem ele, tudo como sempre (localhost).
  tailnet_host="$(python3 -I scripts/tailnet.py host)" || exit 1
  zitadel_port="$(get ZITADEL_PORT)"; zitadel_port="${zitadel_port:-8080}"
  if [ -n "$tailnet_host" ]; then
    export PITBOX_PUBLIC_URL="https://${tailnet_host}"
    export PITBOX_OIDC_ISSUER="https://${tailnet_host}:8443"
    # O PC não alcança o próprio endereço da tailnet: a API fala com o Traefik local, com o
    # Host do issuer. Só em PITBOX_ENV=local (a API recusa-o noutros ambientes).
    export PITBOX_OIDC_LOCAL_DIAL="127.0.0.1:${zitadel_port}"
  else
    if [ -z "${PITBOX_PUBLIC_URL:-}" ]; then
      PITBOX_PUBLIC_URL="$(get PITBOX_PUBLIC_URL)"
      PITBOX_PUBLIC_URL="${PITBOX_PUBLIC_URL:-http://localhost:8081}"
    fi
    export PITBOX_PUBLIC_URL
    export PITBOX_OIDC_ISSUER="http://localhost:${zitadel_port}"
  fi
  export PITBOX_VALKEY_ADDR="127.0.0.1:$(get VALKEY_PORT)"; [ "$PITBOX_VALKEY_ADDR" = "127.0.0.1:" ] && PITBOX_VALKEY_ADDR="127.0.0.1:6379"
  export PITBOX_VALKEY_PASSWORD="$(get VALKEY_API_PASSWORD)"
  port="$(get POSTGRES_PORT)"; port="${port:-5432}"
  export PITBOX_DATABASE_URL="postgres://pitbox_api:$(get PITBOX_API_PASSWORD)@127.0.0.1:${port}/pitbox?sslmode=disable"
  if [ -n "$(get PITBOX_OPENFGA_STORE_ID)" ]; then
    export PITBOX_OPENFGA_ADDR="$(get PITBOX_OPENFGA_ADDR)"; PITBOX_OPENFGA_ADDR="${PITBOX_OPENFGA_ADDR:-127.0.0.1:8090}"
    export PITBOX_OPENFGA_PRESHARED_KEY="$(get OPENFGA_PRESHARED_KEY)"
    export PITBOX_OPENFGA_STORE_ID="$(get PITBOX_OPENFGA_STORE_ID)"
    export PITBOX_OPENFGA_MODEL_ID="$(get PITBOX_OPENFGA_MODEL_ID)"
    echo "api-env: openfga ligado (store $(get PITBOX_OPENFGA_STORE_ID))" >&2
  fi
  LOGIN_CONFIGURED=1
  echo "api-env: login ligado (issuer $PITBOX_OIDC_ISSUER)" >&2
else
  LOGIN_CONFIGURED=0
  echo "api-env: sem login (corre 'make zitadel-provision' para o ligar); os RPC são todos recusados" >&2
fi

