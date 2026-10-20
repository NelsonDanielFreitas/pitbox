# identity

Equipas, membros, convites, papéis, dispositivos e passes offline.

| Pasta / ficheiro | Responsabilidade |
| --- | --- |
| `module.go` | `New(deps)`: cria o módulo e regista os handlers |
| `api.go` | Interface pública usada por outros módulos |
| `domain/` | Entidades e regras puras (sem pgx, proto ou Connect) |
| `app/` | Casos de uso: validar → `authz.Check` → `WithTeamTx` → domínio + repositório → outbox → auditoria; `ports.go` com as interfaces |
| `store/` | Repositórios com sqlc: `queries/*.sql` → `sqlcgen/` (gerado) |
| `transport/` | Handlers Connect e mapeamento proto ⇄ domínio |

## Retenção de dados (F2)

- `identity.invitation.email` é um dado pessoal: fica `NULL` 30 dias depois de o convite
  expirar ou ser aceite, mantendo a linha e o papel para auditoria.
- As linhas já publicadas de `platform.outbox` são purgadas após 30 dias; as por publicar
  nunca são apagadas.

Implementação, job agendado e testes: issue #120 (F2). O F1-06 só define os prazos.
