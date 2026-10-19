# platform

Infraestrutura partilhada por todos os módulos. Não contém regras de negócio nem importa módulos de domínio.

- `config/` — Leitura e validação de variáveis de ambiente (`caarlos0/env`). Falha no arranque se faltar algo obrigatório.
- `log/` — Configuração do `log/slog` em JSON, com `trace_id` e filtros de dados sensíveis.
- `otel/` — Traces e métricas OpenTelemetry (exportador OTLP).
- `db/` — Pool `pgx` como `pitbox_api` (sem ligar no arranque; limites no servidor; cada ligação verifica o papel). `WithTeamTx`, `WithTeamReadTx` e `WithUserTx` abrem a transação com o contexto de RLS (`app.user_id`, `app.team_id` por `set_config`), verificam a pertença à equipa (`ErrNotMember`) e dão ao caso de uso a interface `DBTX`. Só este pacote escreve o contexto. Paginação: com o primeiro módulo.
- `auth/` — BFF de login/logout com Zitadel (ADR 0013): cookies cifrados e `__Host-`, sessões no Valkey, refresh com lock, `Authenticator`, interceptor de autenticação (default deny), `Principal`. Validação do Bearer (JWKS) no F2-06; step-up no F1-03c.
- `authz/` — Cliente OpenFGA: `Check`, `ListObjects` e `SetTeamRole` (um `Write` atómico por membro).
- `interceptors/` — Interceptors Connect: tracing, recovery, erros (incluindo `apperr.ErrNotFound` → `NOT_FOUND`), limites, autenticação, protovalidate e contexto de auditoria (rate limit a seguir). Ver o README da pasta.
- `outbox/` — Gravar eventos de domínio na mesma transação (`Record`) e publicá-los pelo poller próprio (`Processor`, ordem por chave; ADR 0015). River no F2-04.
- `idempotency/` — Guardar e reutilizar respostas por `request_id`.
- `audit/` — Escrita no `audit.audit_log`: eventos de plataforma do login (`Writer`, equipa NULL) e entradas de equipa dentro da transação do caso de uso (`RecordTeam`: a equipa e o autor vêm do contexto da transação, não de argumentos).
- `apperr/` — Os erros partilhados entre camadas (`ErrNotFound`), para o interceptor de erros os traduzir num só sítio.
- `httpserver/` — Servidor HTTP, `/healthz`, `/readyz`, encerramento gracioso.
- `clock/` — Interface `Clock` para não usar `time.Now()` direto (testável).
- `testutil/` — `StartValkey` e `StartPostgres`: contentores descartáveis com a imagem e o arranque do compose (ACL do Valkey; `schema.sql` como `pitbox_migrator`, ligação como `pitbox_api`). Sem Docker os testes saltam, exceto com `PITBOX_REQUIRE_DOCKER=1` (o CI). Fixtures de equipas e utilizadores: F1-04.
