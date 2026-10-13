---
paths:
  - "backend/db/**/*"
  - "proto/**/*.proto"
  - "buf.yaml"
  - "buf.gen.yaml"
  - "backend/authz/**/*"
---

# Regras de base de dados, contratos e autorização

## Base de dados
- `backend/db/schema/schema.sql` é o estado desejado e `backend/db/migrations/` o que se aplica (Atlas Community, ADR 0010; `migrate diff` e `lint` são Pro). Alterações: `make db-new name=...`, escrever o SQL e fazer a mesma alteração em `schema.sql`; `make db-test` apanha desvios.
- Nunca editar uma migração já aplicada (`atlas.sum`); sem `BEGIN`/`COMMIT` nas migrações. Papéis e extensões são bootstrap (`deploy/compose/postgres/init/`), não migrações.
- `make db-lint` falha em `DROP`, `TRUNCATE`, `RENAME`, `ALTER ... TYPE`, `SET NOT NULL` e `CREATE INDEX` sem `CONCURRENTLY` em tabela existente; a exceção é `-- lint:allow <motivo>` (só no "contract").
- Tabela nova: `REVOKE SELECT ... FROM pitbox_readonly` se for sensível e atualizar a lista em `tests/structure_test.sql`. Telemetria: sem compressão (ADR 0011).
- Migrações compatíveis para a frente (expand/contract): adicionar → migrar dados → mudar código → só depois remover. Nunca `DROP` ou `ALTER ... TYPE` destrutivo numa só migração.
- Tabela nova de equipa: `team_id NOT NULL`, `UNIQUE (team_id, id)`, FKs compostas, RLS ativo com política `team_isolation`, colunas `created_at`, `updated_at`, `version`, `deleted_at` quando editável, trigger `touch` e trigger `change_log` se for sincronizada com a app.
- Tabelas só de acrescento (auditoria, versões, movimentos): trigger `forbid_mutation` + `REVOKE UPDATE, DELETE`.
- Segredos guardados apenas como hash (SHA-256, 32 bytes) com `CHECK (length(...) = 32)`.
- Função `SECURITY DEFINER`: dona `pitbox_owner`, `SET search_path = pg_catalog, pg_temp`, nomes qualificados, `REVOKE ALL ... FROM PUBLIC` e `GRANT EXECUTE` só a quem precisa; em `schema.sql` vai **depois** do `GRANT EXECUTE ON ALL FUNCTIONS` em bloco. Nova função destas = entrada na lista fixa de `tests/structure_test.sql` (secção 9). Com esse `search_path` os operadores de `citext` não se veem: comparar com `lower()`.
- Atualizar `backend/db/tests/` com testes para cada regra nova (RLS, restrições, triggers). `make db-test` tem de passar.

## Contratos Protobuf
- Pacote `pitbox.<módulo>.v1`; um serviço por agregado principal.
- Campos nunca reutilizados; remoções com `reserved`. `buf breaking` contra `main` tem de passar.
- Toda a mensagem de pedido tem regras protovalidate (obrigatório, intervalos, formatos, tamanhos máximos de strings e listas).
- Leituras novas declaram `option idempotency_level = NO_SIDE_EFFECTS;` desde o primeiro dia (um RPC novo não quebra o `buf breaking`; mudar um existente quebra, e é por isso que `ListMyTeams` ainda não o tem). Sem essa marca, o RPC conta como escrita e leva o contexto de auditoria.
- Escritas com `request_id`; listas com paginação por cursor (`page_size` máx. 200, `page_token`).
- Comentários em cada serviço, RPC e campo não óbvio (geram documentação).

## Autorização (OpenFGA)
- `backend/authz/model.fga` é a fonte de verdade das relações; `model.fga.yaml` tem asserções para cada relação nova, incluindo casos negativos.
- Cada novo tipo de recurso segue o padrão `team` → `viewer`/`editor` por papel.
- Mudanças ao modelo exigem asserções a passar no CI (`fga model test`).
