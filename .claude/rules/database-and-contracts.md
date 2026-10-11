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
- `backend/db/schema/schema.sql` é o estado desejado. Alterações: editar o esquema → `make db-diff name=...` → rever a migração gerada pelo Atlas.
- Migrações compatíveis para a frente (expand/contract): adicionar → migrar dados → mudar código → só depois remover. Nunca `DROP` ou `ALTER ... TYPE` destrutivo numa só migração.
- Tabela nova de equipa: `team_id NOT NULL`, `UNIQUE (team_id, id)`, FKs compostas, RLS ativo com política `team_isolation`, colunas `created_at`, `updated_at`, `version`, `deleted_at` quando editável, trigger `touch` e trigger `change_log` se for sincronizada com a app.
- Tabelas só de acrescento (auditoria, versões, movimentos): trigger `forbid_mutation` + `REVOKE UPDATE, DELETE`.
- Segredos guardados apenas como hash (SHA-256, 32 bytes) com `CHECK (length(...) = 32)`.
- Atualizar `backend/db/tests/` com testes para cada regra nova (RLS, restrições, triggers). `make db-test` tem de passar.

## Contratos Protobuf
- Pacote `pitbox.<módulo>.v1`; um serviço por agregado principal.
- Campos nunca reutilizados; remoções com `reserved`. `buf breaking` contra `main` tem de passar.
- Toda a mensagem de pedido tem regras protovalidate (obrigatório, intervalos, formatos, tamanhos máximos de strings e listas).
- Escritas com `request_id`; listas com paginação por cursor (`page_size` máx. 200, `page_token`).
- Comentários em cada serviço, RPC e campo não óbvio (geram documentação).

## Autorização (OpenFGA)
- `backend/authz/model.fga` é a fonte de verdade das relações; `model.fga.yaml` tem asserções para cada relação nova, incluindo casos negativos.
- Cada novo tipo de recurso segue o padrão `team` → `viewer`/`editor` por papel.
- Mudanças ao modelo exigem asserções a passar no CI (`fga model test`).
