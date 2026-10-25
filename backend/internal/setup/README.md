# setup

Fichas de setup versionadas, comparação e links de partilha públicos.

| Pasta / ficheiro | Responsabilidade |
| --- | --- |
| `module.go` | `New(deps)`: cria o módulo e regista `SetupService` e `SharedSetupService` |
| `domain/` | Esquema v1 (`schema_v1.json` com `go:embed`), entidades, comparação e erros puros |
| `app/` | Casos de uso: validar → `authz.Check` → `WithTeamTx` → domínio + repositório → auditoria |
| `store/` | Repositórios sqlc e leitura pública por `setup.open_share_link` |
| `transport/` | Handlers Connect, conversão de `Struct` e mapeamento proto ⇄ domínio |

O esquema v1 é a fonte única das regras: qualquer mudança a campos ou intervalos cria um
`schema_version` novo, o servidor aceita todas as versões que conhece, um campo nunca é
removido e o servidor é atualizado antes da app. Os vetores de comparação em
`testdata/compare/` são partilhados com o Vitest no F1-12. Os tuplos por recurso do OpenFGA
ficam para a partilha direta (`shared_with`); nesta fase a autorização é `member`/`mechanic`
na equipa + RLS.
