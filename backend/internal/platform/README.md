# platform

Infraestrutura partilhada por todos os módulos. Não contém regras de negócio nem importa módulos de domínio.

- `config/` — Leitura e validação de variáveis de ambiente (`caarlos0/env`). Falha no arranque se faltar algo obrigatório.
- `log/` — Configuração do `log/slog` em JSON, com `trace_id` e filtros de dados sensíveis.
- `otel/` — Traces e métricas OpenTelemetry (exportador OTLP).
- `db/` — Pool `pgx`, `WithTeamTx` (abre transação e faz `SET LOCAL app.team_id`/`app.user_id`), helpers de paginação.
- `auth/` — Validação de JWT (JWKS em cache), `Principal`, BFF de login/logout com Zitadel, step-up.
- `authz/` — Cliente OpenFGA: `Check`, `ListObjects`, escrita de relações.
- `interceptors/` — Interceptors Connect: tracing, recovery, erros, limites, protovalidate e contexto de auditoria (autenticação e rate limit a seguir). Ver o README da pasta.
- `outbox/` — Gravar eventos de domínio na mesma transação e publicá-los via River.
- `idempotency/` — Guardar e reutilizar respostas por `request_id`.
- `audit/` — Escrita no `audit.audit_log`.
- `httpserver/` — Servidor HTTP, `/healthz`, `/readyz`, encerramento gracioso.
- `clock/` — Interface `Clock` para não usar `time.Now()` direto (testável).
- `testutil/` — Postgres com testcontainers + `schema.sql`, fixtures de equipas e utilizadores para testes.
