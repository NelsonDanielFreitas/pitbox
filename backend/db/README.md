# PitBox — Base de dados

Esquema PostgreSQL 16 (com TimescaleDB opcional) do PitBox: 55 tabelas em 10 esquemas, com Row-Level Security por equipa em todas as tabelas de dados.

## Ficheiros

| Ficheiro | Conteúdo |
| --- | --- |
| `schema.sql` | Esquema completo: extensões, papéis, tipos, tabelas, índices, vistas, triggers, RLS e permissões |
| `tests/rls_and_constraints_test.sql` | Testes: isolamento entre equipas, FKs compostas, imutabilidade, triggers e vistas |

## Como correr

```bash
createdb pitbox
psql -d pitbox -v ON_ERROR_STOP=1 -f schema.sql
psql -d pitbox -v ON_ERROR_STOP=1 -f tests/rls_and_constraints_test.sql   # termina com ROLLBACK
```

Em produção, o `schema.sql` serve de estado desejado para o Atlas (`atlas schema apply`), que gera as migrações versionadas.

## Esquemas e tabelas

| Esquema | Tabelas |
| --- | --- |
| `identity` | user_account, team, membership, invitation, device_key, offline_pass, support_grant |
| `garage` | car, engine, engine_mount, tyre_set, part, stock_movement, part_install · vista stock_level |
| `setup` | field_definition, setup, setup_version, favorite, share_link |
| `events` | track, championship, event, session, driver_feedback, lap, lap_incident, race_strategy, pit_stop · vista session_lap_stats |
| `maintenance` | rule, entry, entry_part, checklist_template, checklist_template_item, checklist_run, checklist_run_item · vista engine_rule_status |
| `timing` | provider_link, raw_import, competitor, result, result_lap |
| `telemetry` | firmware_release, device, pairing_code, recording, sample, alert_threshold, alert |
| `sync` | operation, conflict, change_log |
| `platform` | idempotency_key, outbox, notification_preference, push_subscription |
| `audit` | audit_log |

## Regras de segurança incorporadas

- **RLS por equipa**: a aplicação liga-se como `pitbox_app` (sem `BYPASSRLS`) e define, em cada transação, `SET LOCAL app.team_id` e `SET LOCAL app.user_id` depois de o OpenFGA autorizar o pedido. Sem contexto, nenhuma linha é visível.
- **FKs compostas `(team_id, x_id)`**: um registo nunca pode apontar para um recurso de outra equipa, mesmo com um bug na aplicação.
- **Tabelas só de acrescento**: `setup_version`, `stock_movement`, `audit_log` (triggers + `REVOKE`); `pit_stop` só permite fechar a paragem uma vez.
- **Segredos nunca guardados em claro**: convites, links de partilha e códigos de emparelhamento guardam apenas o hash SHA-256.
- **Restrições de tempo na base de dados**: convites ≤ 7 dias, passes offline ≤ 72 h, acesso de suporte ≤ 24 h, códigos de emparelhamento ≤ 5 min.
- **Exclusão temporal**: um motor não pode estar montado em dois carros ao mesmo tempo (`EXCLUDE USING gist`).

## Convenções

- Chaves UUIDv7 (`app.uuid_v7()`), geradas no cliente para criação offline.
- `created_at`, `updated_at`, `version` (incrementado por trigger, para concorrência otimista) e `deleted_at` (apagamento lógico).
- Unidades no nome da coluna: `_ms`, `_s`, `_c`, `_mm`, `_pct`, `_v`, `_ml`, `_cents`.
- Cada alteração em entidades sincronizadas gera uma linha em `sync.change_log`; o cursor do `Pull` é `seq`.
