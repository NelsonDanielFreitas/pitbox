# platform

Infraestrutura partilhada por todos os módulos. Não contém regras de negócio nem importa módulos de domínio.

- `config/` — Leitura e validação de variáveis de ambiente (`caarlos0/env`). Falha no arranque se faltar algo obrigatório.
- `log/` — Configuração do `log/slog` em JSON, com `trace_id` e filtros de dados sensíveis.
- `otel/` — Traces e métricas OpenTelemetry (exportador OTLP).
- `db/` — Pool `pgx` como `pitbox_api` (sem ligar no arranque; limites no servidor). `WithTeamTx` (abre transação e faz `SET LOCAL app.team_id`/`app.user_id`) e a paginação: F1-04.
- `auth/` — BFF de login/logout com Zitadel (ADR 0013): cookies cifrados e `__Host-`, sessões no Valkey, refresh com lock, `Authenticator`, interceptor de autenticação (default deny), `Principal`. Validação do Bearer (JWKS) no F2-06; step-up no F1-03c.
- `authz/` — Cliente OpenFGA: `Check`, `ListObjects`, escrita de relações.
- `interceptors/` — Interceptors Connect: tracing, recovery, erros, limites, protovalidate e contexto de auditoria (autenticação e rate limit a seguir). Ver o README da pasta.
- `outbox/` — Gravar eventos de domínio na mesma transação e publicá-los via River.
- `idempotency/` — Guardar e reutilizar respostas por `request_id`.
- `audit/` — Escrita no `audit.audit_log`: eventos de plataforma do login (equipa NULL); os casos de uso escrevem as suas entradas na própria transação (F1-04+).
- `httpserver/` — Servidor HTTP, `/healthz`, `/readyz`, encerramento gracioso.
- `clock/` — Interface `Clock` para não usar `time.Now()` direto (testável).
- `testutil/` — `StartValkey` e `StartPostgres`: contentores descartáveis com a imagem e o arranque do compose (ACL do Valkey; `schema.sql` como `pitbox_migrator`, ligação como `pitbox_api`). Sem Docker os testes saltam, exceto com `PITBOX_REQUIRE_DOCKER=1` (o CI). Fixtures de equipas e utilizadores: F1-04.
