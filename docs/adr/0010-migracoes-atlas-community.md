# 0010 — Migrações com Atlas Community (SQL escrito à mão) e teste de desvio

- **Estado:** Aceite
- **Data:** 2026-10-12
- **Cartão:** F0-06

## Contexto

A arquitetura (§4.5) previa o Atlas em modo declarativo: `schema.sql` como estado desejado, `atlas migrate diff` para gerar as migrações e `atlas migrate lint` no CI. Testado com a v1.3.0:

- O `migrate diff` do nosso `schema.sql` falha sem `atlas login`: extensões, funções, triggers, vistas, políticas RLS e papéis/`GRANT` só são suportados na edição Pro (a Community Edition, Apache-2.0, exclui-os de forma explícita). O `schema.sql` usa tudo isto (7 funções, 6 triggers, 3 vistas, 53 políticas).
- O `migrate lint` é Pro desde a v0.38.
- Pro custa 9 $/utilizador/mês e exigiria um segredo de longa duração (`ATLAS_TOKEN`) no CI de cada PR.
- `migrate hash`, `validate`, `apply` e `status` funcionam sem login sobre SQL escrito à mão (resultado idêntico ao do `schema.sql`).

## Decisão

1. Usar o **Atlas Community** (`atlas-community` no mise, fixado e com checksum no `mise.lock`) só para **versionar, validar e aplicar** migrações SQL escritas à mão (`make db-new name=...`). Sem login, sem `ATLAS_TOKEN`.
2. Manter o `schema.sql` como **estado desejado legível**. Cada alteração toca nos dois sítios: `schema.sql` e uma migração nova.
3. Um **teste de desvio** no CI (`make db-test`) garante que as duas fontes não divergem: aplica as migrações numa base, o `schema.sql` noutra (ambas como `pitbox_migrator`, a partir do mesmo bootstrap) e compara `pg_dump --schema-only` (com donos e ACL), papéis, extensões, jobs e privilégios por omissão. Um dump vazio ou suspeito falha o teste (fail closed).
4. **Lint por script** (`scripts/db-lint.py`, só nas migrações novas do PR): falha em `DROP`, `TRUNCATE`, `ALTER ... TYPE`, `RENAME`, `SET NOT NULL` e `CREATE INDEX` sem `CONCURRENTLY` em tabela existente. Exceção explícita e visível no PR: `-- lint:allow <motivo>` na linha ou na anterior (usada no "contract" do expand/contract).
5. **Bootstrap separado das migrações.** O bootstrap (papéis, extensões `timescaledb`, `citext`, `btree_gist`, bases) corre como superutilizador (`deploy/compose/postgres/init`; em produção, OpenTofu ou o administrador da base). As migrações correm como `pitbox_migrator` (sem superutilizador, sem `CREATEROLE`), que assume `pitbox_owner` em cada sessão (`ALTER ROLE ... SET role`), por isso todos os objetos têm esse dono. `ALTER DEFAULT PRIVILEGES` dá `pitbox_app` e `pitbox_readonly` os privilégios certos nas tabelas futuras; o que for sensível leva `REVOKE` na migração e entra na lista de tabelas ilegíveis para o `readonly` em `structure_test.sql`.
6. Uma migração nunca tem `BEGIN`/`COMMIT` (o Atlas envolve cada ficheiro numa transação). Migrações aplicadas nunca se editam (`atlas.sum` + `atlas migrate validate`).
7. `atlas migrate apply` usa `--allow-dirty` porque o bootstrap cria objetos que as migrações não gerem (esquema `fga` do OpenFGA, extensões).

## Alternativas consideradas

| Alternativa | Porque não |
| --- | --- |
| Atlas Pro | Conta SaaS, 9 $/mês e um segredo de longa duração no CI por uma conveniência (o `diff` automático) |
| goose / dbmate / golang-migrate | Trocariam de ferramenta para ganhar pouco: o `atlas.sum` e a tabela de revisões já dão o mesmo |
| Binário Atlas "normal" | Proprietário (Atlas MSA) com funcionalidades Pro desligadas; a Community é Apache-2.0 e não tem nada para "desbloquear" por engano |
| `squawk` como linter | Boa opção futura; é uma dependência nova (licença e versão por confirmar) |

## Consequências

- Perde-se o `migrate diff` automático e o `migrate lint`: o autor escreve o SQL; o teste de desvio e o lint por script compensam.
- Atualizados: arquitetura §4.5 e §14, plano F0-06, `CLAUDE.md`, `.claude/rules/database-and-contracts.md`, READMEs de `backend/db`.
- Reavaliar se o Atlas voltar a incluir estas funcionalidades na Community, ou quando o volume de migrações justificar o `squawk`.
- Em produção a credencial do migrador deve ser de curta duração (F5).
- Bases já existentes (antes do F0-06) têm de ser recriadas com `make reset`: o bootstrap mudou (`pitbox_owner` com LOGIN sem palavra-passe, `ALTER ROLE pitbox_migrator SET role`, extensões).
