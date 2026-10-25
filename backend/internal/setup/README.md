# setup

Fichas de setup versionadas, comparação e links de partilha públicos.

| Pasta / ficheiro | Responsabilidade |
| --- | --- |
| `module.go` | `New(deps)`: cria o módulo e regista `SetupService` e `SharedSetupService` |
| `domain/` | Esquema v1 (`schema_v1.json` com `go:embed`), entidades, comparação e erros puros |
| `app/` | Casos de uso: validar → `authz.Check` → `WithTeamTx` → domínio + repositório → auditoria |
| `store/` | Repositórios sqlc e leitura pública por `setup.open_share_link` |
| `transport/` | Handlers Connect, conversão de `Struct` e mapeamento proto ⇄ domínio |

O esquema v1 é a fonte única das regras; o servidor aceita as versões que conhece e nunca
remove campos. Os tuplos por recurso do OpenFGA ficam para a partilha direta (`shared_with`);
nesta fase a autorização é `member`/`mechanic` na equipa + RLS.
