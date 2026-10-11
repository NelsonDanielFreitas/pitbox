# timing

Conectores de live timing, importação de resultados, competidores.

| Pasta / ficheiro | Responsabilidade |
| --- | --- |
| `module.go` | `New(deps)`: cria o módulo e regista os handlers |
| `api.go` | Interface pública usada por outros módulos |
| `domain/` | Entidades e regras puras (sem pgx, proto ou Connect) |
| `app/` | Casos de uso: validar → `authz.Check` → `WithTeamTx` → domínio + repositório → outbox → auditoria; `ports.go` com as interfaces |
| `store/` | Repositórios com sqlc: `queries/*.sql` → `sqlcgen/` (gerado) |
| `transport/` | Handlers Connect e mapeamento proto ⇄ domínio |
