# garage

Carros, motores e montagens, pneus, peças e stock.

| Pasta / ficheiro | Responsabilidade |
| --- | --- |
| `module.go` | `New(deps)`: cria o módulo e regista os quatro serviços |
| `api.go` | `CarDirectory` e `CatalogReader` para outros módulos/sync; sem `domain` nem `store` |
| `domain/` | Entidades e regras puras (`Car`, `Engine`, `TyreSet`, `Part`, montagens, movimentos) |
| `app/` | Casos de uso: validar → `authz.Check` → `WithTeamTx` → domínio/store → idempotência → outbox → auditoria |
| `store/` | Repositórios sqlc (`queries/*.sql` → `sqlcgen/`) |
| `transport/` | Handlers Connect e mapeamento proto ⇄ domínio |

Leituras aceitam owner/mechanic/pilot; escritas exigem owner/mechanic. Outra equipa ou guest recebem `NOT_FOUND`; o piloto recebe `PERMISSION_DENIED` ao escrever e não vê `unit_cost_cents`. A autorização usa a relação `team` do OpenFGA + RLS/FKs, sem tuplos por recurso.
