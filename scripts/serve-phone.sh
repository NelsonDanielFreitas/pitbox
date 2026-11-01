#!/usr/bin/env bash
# Perfil telemóvel (ADR 0029, F1-14c): expõe a app e o Zitadel na tailnet com `tailscale serve`.
#
#   serve-phone.sh on    liga:   https://<host>        -> vite preview  (127.0.0.1:4173)
#                                https://<host>:8443   -> zitadel-proxy (127.0.0.1:$ZITADEL_PORT)
#   serve-phone.sh off   corta tudo (`tailscale serve reset`) e mostra o estado
#
# NUNCA usa `tailscale funnel`: Funnel publica o serviço na internet. Só `serve`, que fica
# restrito aos dispositivos da tua tailnet. Nada mais é exposto: Postgres, Valkey, OpenFGA,
# Jaeger, Mailpit, SeaweedFS e a porta direta do pitbox-api ficam só em 127.0.0.1.
# Nenhuma credencial do Tailscale passa por aqui (a sessão do tailscaled é do utilizador).
set -euo pipefail
cd "$(dirname "$0")/.."
ENV_FILE=deploy/compose/.env

get() { grep -E "^$1=" "$ENV_FILE" 2>/dev/null | head -1 | cut -d= -f2- || true; }

command -v tailscale >/dev/null 2>&1 || { echo "serve-phone: 'tailscale' não encontrado (ver deploy/README.md)" >&2; exit 1; }

case "${1:-}" in
  on)
    host="$(python3 -I scripts/tailnet.py host)" || exit 1
    [ -n "$host" ] || { echo "serve-phone: define PITBOX_TAILNET_HOST em $ENV_FILE (ver deploy/README.md)" >&2; exit 1; }
    # O nome tem de ser o desta máquina: um valor errado serviria o certificado de outro nome
    # (ou nenhum) e registaria redirect URIs que nunca respondem.
    self="$(tailscale status --json | python3 -I -c 'import json,sys; d=json.load(sys.stdin); print(d["BackendState"], d["Self"]["DNSName"].rstrip("."))')"
    state="${self%% *}"; name="${self#* }"
    [ "$state" = "Running" ] || { echo "serve-phone: o tailscale não está ligado (estado: $state)" >&2; exit 1; }
    [ "$name" = "$host" ] || { echo "serve-phone: PITBOX_TAILNET_HOST ($host) não é o nome desta máquina na tailnet ($name)" >&2; exit 1; }
    port="$(get ZITADEL_PORT)"; port="${port:-8080}"
    (exec 3<>"/dev/tcp/127.0.0.1/4173") 2>/dev/null || echo "serve-phone: AVISO: nada a escutar em 127.0.0.1:4173 (corre 'make app-preview')" >&2
    (exec 3<>"/dev/tcp/127.0.0.1/${port}") 2>/dev/null || echo "serve-phone: AVISO: nada a escutar em 127.0.0.1:${port} (corre 'make up')" >&2
    tailscale serve --bg --yes --https=443 "http://127.0.0.1:4173"
    tailscale serve --bg --yes --https=8443 "http://127.0.0.1:${port}"
    tailscale serve status
    echo "serve-phone: só na tailnet. Abre https://${host} no telemóvel (com a app Tailscale ligada)."
    ;;
  off)
    tailscale serve reset
    tailscale serve status
    ;;
  *)
    echo "uso: serve-phone.sh on|off" >&2
    exit 2
    ;;
esac
