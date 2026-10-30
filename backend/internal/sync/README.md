# sync

Push/Pull de operações offline, conflitos e change log.

| Pasta / ficheiro | Responsabilidade |
| --- | --- |
| `module.go` | `New(deps)`: cria o módulo e regista os handlers |
| `api.go` | Interface pública usada por outros módulos |
| `domain/` | Entidades e regras puras (sem pgx, proto ou Connect) |
| `app/` | Casos de uso: validar → `authz.Check` → `WithTeamTx` → domínio + repositório → outbox → auditoria; `ports.go` com as interfaces |
| `store/` | Repositórios com sqlc: `queries/*.sql` → `sqlcgen/` (gerado) |
| `transport/` | Handlers Connect e mapeamento proto ⇄ domínio |

## Estado (F1-13a)

`SyncService` (`proto/pitbox/sync/v1`) com registo de dispositivos, passe offline de 72 h, `Push` idempotente com merge por campo e conflitos, e `Pull` por cursor `(xid, seq)`; decisões no [ADR 0025](../../../docs/adr/0025-formato-de-assinatura-e-cursor-do-sync.md). Cada módulo regista os seus aplicadores (`platform/syncapply`); hoje: `garage.car`, `setup.setup` e `setup.setup_version`. O módulo só arranca com `PITBOX_SYNC_PASS_KEYS` (ver `cmd/api/README.md`). A parte da app é o F1-13b.
