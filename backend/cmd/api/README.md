# cmd/api

Servidor Connect/gRPC + BFF web (`pitbox-api`).

Só contém `main.go`: trata dos sinais (SIGINT/SIGTERM) e chama `internal/bootstrap.RunAPI`.

## Correr localmente

```bash
make up            # Jaeger (e o resto) em deploy/compose
make run-api       # 127.0.0.1:8081, traces para o Jaeger (http://127.0.0.1:16686)
curl -i 127.0.0.1:8081/healthz
make smoke-api     # prova ponta a ponta: /healthz, trace no Jaeger e paragem limpa
```

## Configuração (variáveis de ambiente)

| Variável | Omissão | Notas |
| --- | --- | --- |
| `PITBOX_ENV` | `local` | `local`, `preview`, `staging` ou `production`; outro valor é erro |
| `PITBOX_HTTP_ADDR` | `127.0.0.1:8081` | Em contentor/Cloud Run usar `:8080` (definido no deploy, F5) |
| `PITBOX_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `PITBOX_SHUTDOWN_DRAIN` | `0s` | Serve "não pronto" antes de fechar (alguns segundos em Kubernetes) |
| `PITBOX_SHUTDOWN_TIMEOUT` | `6s` | `drain + timeout + 2 s` de flush de telemetria ≤ 10 s (prazo do Cloud Run) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | (vazio) | Sem endpoint, os traces ficam desligados. `http://` só com `PITBOX_ENV=local` |
| `OTEL_SERVICE_NAME`, `OTEL_TRACES_SAMPLER`, ... | | Variáveis padrão do OpenTelemetry, lidas pelo SDK |

A configuração inválida faz o processo sair com erro no arranque (fail closed). No log só aparecem os campos de uma lista permitida (`config.Config.LogValue`).

## Endpoints

- `GET /healthz`: liveness (200 `ok`), sem detalhes internos.
- `GET /readyz`: readiness; passa a 503 quando a paragem começa.

Nenhum dos dois cria traces. Os handlers Connect entram a partir do F1-02.
