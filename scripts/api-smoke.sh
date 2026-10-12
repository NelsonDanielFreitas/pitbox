#!/usr/bin/env bash
# F1-01: prova ponta a ponta do pitbox-api contra o Jaeger local (precisa de `make up`).
#  1. arranca o binário com a configuração local (porta livre, OTLP -> Jaeger);
#  2. /healthz responde 200 e NÃO cria trace; um pedido normal (404) cria;
#  3. o trace aparece na API de consulta do Jaeger;
#  4. SIGTERM -> paragem limpa (exit 0, "shutdown complete" no log).
set -euo pipefail
cd "$(dirname "$0")/.."

fail() { echo "api-smoke: $*" >&2; exit 1; }

OTLP_PORT="${OTLP_HTTP_PORT:-4318}"
JAEGER_UI_PORT="${JAEGER_UI_PORT:-16686}"
JAEGER="http://127.0.0.1:${JAEGER_UI_PORT}"
SERVICE="pitbox-api-smoke-$$" # nome único: só vemos os traces desta execução

curl -fsS -o /dev/null "$JAEGER/" || fail "Jaeger não responde em $JAEGER (corre 'make up')"

WORK=$(mktemp -d)
PID=""
cleanup() {
  [ -n "$PID" ] && kill -KILL "$PID" 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT

(cd backend && go build -o "$WORK/api" ./cmd/api)

PITBOX_ENV=local PITBOX_HTTP_ADDR=127.0.0.1:0 \
  OTEL_SERVICE_NAME="$SERVICE" OTEL_EXPORTER_OTLP_ENDPOINT="http://127.0.0.1:${OTLP_PORT}" \
  "$WORK/api" >"$WORK/log" 2>&1 &
PID=$!

ADDR=""
for _ in $(seq 1 50); do
  ADDR=$(python3 -I -c '
import json, sys
for line in open(sys.argv[1]):
    try:
        m = json.loads(line)
    except ValueError:
        continue
    if m.get("msg") == "listening":
        print(m["addr"]); break
' "$WORK/log")
  [ -n "$ADDR" ] && break
  kill -0 "$PID" 2>/dev/null || { cat "$WORK/log" >&2; fail "o processo terminou ao arrancar"; }
  sleep 0.1
done
[ -n "$ADDR" ] || { cat "$WORK/log" >&2; fail "a API não arrancou"; }
echo "API em $ADDR (serviço $SERVICE)"

code=$(curl -s -o /dev/null -w '%{http_code}' "http://$ADDR/healthz")
[ "$code" = 200 ] || fail "/healthz devolveu $code"
echo "/healthz: 200"
curl -s -o /dev/null "http://$ADDR/nao-existe" # 404 com trace

# O exportador envia em lote (alguns segundos): esperar pelo trace. A API de consulta do
# Jaeger v2 é a /api/v3 (stream de objetos JSON); sem resultados devolve 404 "No traces found".
START=$(date -u -d '-2 minutes' +%Y-%m-%dT%H:%M:%SZ)
count_traces() {
  local end
  end=$(date -u -d '+2 minutes' +%Y-%m-%dT%H:%M:%SZ)
  curl -s "$JAEGER/api/v3/traces?query.service_name=${SERVICE}&query.start_time_min=${START}&query.start_time_max=${end}&query.search_depth=20" |
    python3 -I -c '
import json, sys
text, i, ids = sys.stdin.read(), 0, set()
dec = json.JSONDecoder()
while i < len(text):
    while i < len(text) and text[i].isspace():
        i += 1
    if i >= len(text):
        break
    obj, i = dec.raw_decode(text, i)
    for rs in (obj.get("result") or {}).get("resourceSpans", []):
        for ss in rs.get("scopeSpans", []):
            for sp in ss.get("spans", []):
                ids.add(sp["traceId"])
print(len(ids))
'
}
found=0
for _ in $(seq 1 30); do
  n=$(count_traces)
  if [ "$n" -ge 1 ]; then found=$n; break; fi
  sleep 1
done
[ "$found" -eq 1 ] || fail "esperava exatamente 1 trace (o 404; o /healthz não deve criar trace), vi $found"
echo "Jaeger: 1 trace (o 404); /healthz não criou trace"

kill -TERM "$PID"
rc=0; wait "$PID" || rc=$?
PID=""
[ "$rc" -eq 0 ] || { cat "$WORK/log" >&2; fail "a paragem devolveu $rc"; }
grep -q '"msg":"shutdown complete"' "$WORK/log" || fail "sem 'shutdown complete' no log"
if grep -q '"msg":"opentelemetry error"' "$WORK/log"; then cat "$WORK/log" >&2; fail "erros do exportador OTLP"; fi
echo "api-smoke: OK"
