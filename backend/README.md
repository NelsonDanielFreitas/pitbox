# backend

Módulo Go do PitBox (`go.mod` próprio, criado no cartão F1-01). Três binários partilham o mesmo código:

| Pasta | Conteúdo |
| --- | --- |
| `cmd/` | `main.go` finos de `api`, `worker` e `ingest` |
| `internal/bootstrap/` | Monta cada binário (config, dependências, módulos, servidor) |
| `internal/platform/` | Infraestrutura partilhada, sem regras de negócio |
| `internal/<módulo>/` | Um módulo por domínio, sempre com `domain/`, `app/`, `store/`, `transport/` |
| `gen/` | Código gerado pelo Buf a partir de `proto/` (nunca editar) |
| `db/` | Esquema, migrações Atlas e testes SQL |
| `authz/` | Modelo OpenFGA e asserções |
| `testdata/` | Ficheiros de teste partilhados (ex.: páginas de live timing para fuzzing) |

Regras completas em `.claude/rules/go-backend.md`.
