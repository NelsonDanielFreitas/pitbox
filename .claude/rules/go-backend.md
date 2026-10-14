---
paths:
  - "backend/**/*.go"
  - "backend/**/*.sql"
  - "backend/sqlc.yaml"
---

# Regras do backend Go

## Estilo e erros
- `gofmt`/`goimports`; `golangci-lint run` sem avisos.
- Erros com contexto: `fmt.Errorf("create car: %w", err)`; comparar com `errors.Is/As`; erros de domínio em `domain/errors.go` (ex.: `ErrNotFound`, `ErrConflict`, `ErrInvalid`).
- `transport/` converte erros de domínio em códigos Connect: NotFound → `CodeNotFound`, sem permissão → `CodePermissionDenied`, validação → `CodeInvalidArgument`, conflito de versão → `CodeAborted`. Mensagens para o cliente nunca incluem detalhes internos; o detalhe vai para o log.
- Connect v2 (ADR 0012): só um `*connect.Error` mostra a mensagem ao cliente (um `error` normal sai só com o código). Escreve `connect.NewError(code, "mensagem pública fixa").WithCause(err)`; **nunca** `connect.NewError(code, err.Error())` (é o que a ferramenta de migração gera e vazaria detalhes internos). O teste `internal/archtest` só admite um literal ou uma constante do pacote como mensagem, proíbe `connect.Errorf` e os limites de tamanho fora de `httpserver/rpc.go`; uma exceção revista é `//archtest:allow <motivo>`. Embrulha erros com o nome da operação, nunca com valores do pedido (a causa vai para o log).
- Sem `panic` fora de `main` e de inicializações impossíveis de recuperar.

## Contexto e concorrência
- `ctx context.Context` é o primeiro argumento de tudo o que faz I/O. Nunca guardar contextos em structs.
- Deadlines em todas as chamadas externas (Postgres, OpenFGA, Zitadel, HTTP). Goroutines sempre com forma de terminar (ctx ou `errgroup`).
- Testes com `-race`.

## Camadas de cada módulo
- `domain/`: tipos e regras puras. Sem pgx, proto, Connect, slog nem `time.Now()` direto (injetar um `Clock`).
- `app/`: casos de uso. Ordem fixa: validar → `authz.Check` → `db.WithTeamTx` → chamar domínio e repositório → gravar eventos na outbox na mesma transação → auditoria (`audit.RecordTeam`, também dentro da transação).
- `app/ports.go`: interfaces dos repositórios, definidas por quem as usa.
- `store/`: implementação com sqlc; queries em `store/queries/*.sql`; código gerado em `store/sqlcgen/`.
- `transport/`: handlers Connect finos; mapeamento proto ⇄ domínio em `mapping.go`.
- `module.go`: `New(deps) (*Module, error)` regista handlers; `api.go` expõe a interface pública do módulo.

## Base de dados
- Transações: `db.WithTeamTx(ctx, userID, teamID, fn)` para escritas, `WithTeamReadTx` para RPCs `NO_SIDE_EFFECTS` e `WithUserTx` para o que é do utilizador (listar as minhas equipas). Os ids são `db.UserID` e `db.TeamID` (`db.ParseUserID`/`ParseTeamID`; o `UserID` vem de `auth.PrincipalFrom(ctx).UserID`, o `TeamID` de um campo `team_id` do pedido, validado como UUID, e nunca deduzido da sessão: a fixar nos contratos do F1-06/F1-07). `fn` recebe `db.DBTX` (sem `Commit`) e o `Membership` (o papel); `db.ErrNotMember` sai como `NOT_FOUND` no interceptor de erros: não o converter nos handlers. Sem I/O de rede dentro de `fn`.
- Só `platform/db` escreve o contexto de RLS e abre transações: nada de `set_config`, `SET LOCAL`, `SET ROLE`, `app.team_id`/`app.user_id`, `Begin`/`BeginTx` nem `pgxpool` noutro sítio, nem nos `.sql` do sqlc (o `internal/archtest` falha). `domain/`, `app/` e `transport/` não importam pgx.
- Só SQL parametrizado via sqlc/pgx. Nunca concatenar SQL nem usar `fmt.Sprintf` para queries.
- Toda a query de dados de equipa corre dentro de `WithTeamTx`; filtra também por `team_id` explicitamente (defesa em profundidade além do RLS).
- Concorrência otimista: updates com `WHERE id = $1 AND version = $2`; 0 linhas → `ErrConflict`.
- Paginação por cursor (máx. 200 por página); nunca `OFFSET` em tabelas grandes.
- Telemetria em lote com `CopyFrom`.

## API e validação
- Toda a mensagem de entrada tem regras protovalidate no `.proto`; o interceptor valida antes do handler.
- Limites: 4 MiB por mensagem (32 MiB só no ingest), deadline máximo 30 s. O limite é sempre explícito (`connecthttp.WithReadMaxBytes`); **nunca** `WithReadMaxBytes(0)` (ilimitado, o que a ferramenta de migração escreve para preservar a v1). Reflection gRPC só em desenvolvimento (`grpcreflect.Register`).
- Handlers montam-se com `connect.NewServer(interceptors...)` + `connecthttp.Mount`; os interceptors são `connect.ServerInterceptor` e o primeiro argumento é o mais externo (ordem da arquitetura §4.4).
- Escritas aceitam `request_id` (UUIDv7) para idempotência.

## Logs e observabilidade
- `log/slog` em JSON com `trace_id`; níveis Debug/Info/Warn/Error com critério.
- Nunca registar tokens, cookies, palavras-passe, emails, valores de setup ou corpos de pedidos.
- Spans OpenTelemetry nos casos de uso e nas chamadas externas.

## Autenticação
- Todo o RPC exige sessão: o interceptor de autenticação (lugar 5 da cadeia) é *default deny*. Só um procedimento listado explicitamente como público passa sem sessão; uma falha de armazém ou de IdP é `UNAVAILABLE`, nunca `UNAUTHENTICATED` nem acesso.
- O `Principal` (`auth.PrincipalFrom(ctx)`) traz `user_id`, `sub`, `amr` e `auth_time` da sessão; equipas e papéis nunca vêm da sessão (verifica-se no OpenFGA). A identidade nunca viaja numa mensagem de pedido.
- O caminho `Authorization: Bearer` não existe até ao F2-06. Nada de tokens, cookies, `code`, `state`, emails ou *claims* em logs, traces, erros ou auditoria.
- Os segredos (`PITBOX_OIDC_CLIENT_SECRET`, `PITBOX_VALKEY_PASSWORD`, `PITBOX_DATABASE_URL`, `PITBOX_AUTH_KEYS`) nunca em `LogValue`, em erros ou em `fmt`; erros sobre eles não repetem o valor.

## Testes
- Table-driven, `t.Parallel()` quando seguro, `testify/require` para pré-condições.
- `domain/` e `app/` com testes unitários (repositórios falsos); `store/` com `testcontainers-go` e Postgres real com o `schema.sql`.
- Testes que precisam de Valkey ou PostgreSQL usam `testutil.StartValkey` / `StartPostgres` (a imagem e o arranque do compose); com Docker em falta saltam, exceto com `PITBOX_REQUIRE_DOCKER=1` (CI): um teste de segurança saltado não é um teste passado. Testes de segurança têm teste de mutação (estragar o controlo faz falhar um teste).
- Todo o endpoint tem teste de acesso cruzado: utilizador da equipa B a ler/escrever recurso da equipa A → `NOT_FOUND`.
- Código que faz parsing de dados externos tem `FuzzXxx` (`go test -fuzz`).

## Dependências
- Preferir a biblioteca padrão. Dependência nova = justificação na resposta + `govulncheck ./...` limpo.
- Ferramentas (sqlc, buf plugins, golangci-lint) declaradas com a diretiva `tool` no `go.mod` e corridas com `go tool`.
