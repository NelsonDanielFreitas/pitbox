# PitBox — Base de dados

Esquema PostgreSQL 16 + TimescaleDB do PitBox: 58 tabelas em 10 esquemas, com Row-Level Security por equipa em todas as tabelas de dados.

## Ficheiros

| Ficheiro | Conteúdo |
| --- | --- |
| `schema/schema.sql` | Estado desejado, legível: tipos, tabelas, índices, vistas, triggers, RLS, permissões e hypertable. Não cria papéis nem extensões (bootstrap) |
| `migrations/` | Migrações versionadas (Atlas Community) e `atlas.sum`; a `0001_initial` é uma cópia do `schema.sql` |
| `tests/rls_and_constraints_test.sql` | Isolamento entre equipas, FKs compostas, imutabilidade, triggers, vistas, idempotência por utilizador, CHECKs |
| `tests/structure_test.sql` | Toda a tabela tem RLS (exceto lista explícita) e política, donos, papéis, privilégios de `pitbox_app` e `pitbox_readonly`, telemetria |
| `tests/default_privileges_test.sql` | Uma tabela criada por migração futura herda os privilégios certos; `pitbox_owner` sem palavra-passe |
| `tests/drift_snapshot.sql` | Papéis, extensões e jobs usados pelo teste de desvio |

## Como correr

```bash
make db-test            # contentores descartáveis (mesma imagem do compose): migrações + schema.sql + testes + desvio
make db-validate        # atlas.sum e sintaxe (offline)
make db-lint            # migrações novas sem operações destrutivas (offline)
make db-new name=add_x  # migração nova, vazia
```

Não precisa de `make up`: o `db-test` cria dois Postgres próprios, com palavras-passe aleatórias e portas só em `127.0.0.1`.

## Fluxo para alterar a base de dados (ADR 0010)

1. `make db-new name=<nome>` e escrever o SQL da migração (sem `BEGIN`/`COMMIT`; o Atlas já usa uma transação).
2. Fazer **a mesma alteração** em `schema/schema.sql`.
3. `atlas migrate hash --env local` só se editaste uma migração **ainda não commitada**; nunca editar uma já aplicada.
4. Acrescentar testes em `tests/`; `make db-test` tem de passar (apanha desvios entre 1 e 2).
5. Operações destrutivas (`DROP`, `RENAME`, `ALTER ... TYPE`, ...) só no passo "contract" do expand/contract, com `-- lint:allow <motivo>`.

**Bootstrap vs migrações.** Papéis, extensões e bases são criados como superutilizador por `deploy/compose/postgres/init/` (em produção, pelo OpenTofu). As migrações correm como `pitbox_migrator` (sem superutilizador), que assume `pitbox_owner`. Uma tabela nova sensível a `pitbox_readonly` leva `REVOKE SELECT` na migração e entra na lista de `tests/structure_test.sql`.

Aplicar manualmente (por exemplo no compose): `DATABASE_URL=postgres://pitbox_migrator:<palavra-passe>@127.0.0.1:5432/pitbox?sslmode=disable atlas migrate apply --env local --allow-dirty` (o `--allow-dirty` é preciso porque o bootstrap já criou o esquema `fga` e as extensões).

## Esquemas e tabelas

| Esquema | Tabelas |
| --- | --- |
| `identity` | user_account, team, membership, invitation, ownership_transfer, device_key, offline_pass, support_grant |
| `garage` | scale, powertrain, car, engine, engine_mount, tyre_set, part, stock_movement, part_install · vista stock_level |
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
- **Telemetria com RLS e sem compressão** (ADR 0011): hypertable com retenção de 24 meses.
- **Exclusão temporal**: um motor não pode estar montado em dois carros ao mesmo tempo (`EXCLUDE USING gist`).

## Convenções

- Chaves UUIDv7 (`app.uuid_v7()`), geradas no cliente para criação offline.
- `created_at`, `updated_at`, `version` (incrementado por trigger, para concorrência otimista) e `deleted_at` (apagamento lógico).
- Unidades no nome da coluna: `_ms`, `_s`, `_c`, `_mm`, `_pct`, `_v`, `_ml`, `_cents`.
- Cada alteração em entidades sincronizadas gera uma linha em `sync.change_log`; o cursor do `Pull` é `seq`.
