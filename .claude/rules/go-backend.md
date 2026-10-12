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
- Connect v2 (ADR 0012): só um `*connect.Error` mostra a mensagem ao cliente (um `error` normal sai só com o código). Escreve `connect.NewError(code, "mensagem pública fixa").WithCause(err)`; **nunca** `connect.NewError(code, err.Error())` (é o que a ferramenta de migração gera e vazaria detalhes internos).
- Sem `panic` fora de `main` e de inicializações impossíveis de recuperar.

## Contexto e concorrência
- `ctx context.Context` é o primeiro argumento de tudo o que faz I/O. Nunca guardar contextos em structs.
- Deadlines em todas as chamadas externas (Postgres, OpenFGA, Zitadel, HTTP). Goroutines sempre com forma de terminar (ctx ou `errgroup`).
- Testes com `-race`.

## Camadas de cada módulo
- `domain/`: tipos e regras puras. Sem pgx, proto, Connect, slog nem `time.Now()` direto (injetar um `Clock`).
- `app/`: casos de uso. Ordem fixa: validar → `authz.Check` → `db.WithTeamTx` → chamar domínio e repositório → gravar eventos na outbox na mesma transação → auditoria.
- `app/ports.go`: interfaces dos repositórios, definidas por quem as usa.
- `store/`: implementação com sqlc; queries em `store/queries/*.sql`; código gerado em `store/sqlcgen/`.
- `transport/`: handlers Connect finos; mapeamento proto ⇄ domínio em `mapping.go`.
- `module.go`: `New(deps) (*Module, error)` regista handlers; `api.go` expõe a interface pública do módulo.

## Base de dados
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

## Testes
- Table-driven, `t.Parallel()` quando seguro, `testify/require` para pré-condições.
- `domain/` e `app/` com testes unitários (repositórios falsos); `store/` com `testcontainers-go` e Postgres real com o `schema.sql`.
- Todo o endpoint tem teste de acesso cruzado: utilizador da equipa B a ler/escrever recurso da equipa A → `NOT_FOUND`.
- Código que faz parsing de dados externos tem `FuzzXxx` (`go test -fuzz`).

## Dependências
- Preferir a biblioteca padrão. Dependência nova = justificação na resposta + `govulncheck ./...` limpo.
- Ferramentas (sqlc, buf plugins, golangci-lint) declaradas com a diretiva `tool` no `go.mod` e corridas com `go tool`.
