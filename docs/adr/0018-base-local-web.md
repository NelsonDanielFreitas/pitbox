# 0018 — Base local web com wa-sqlite e OPFS

- **Estado:** Aceite (Nelson, 2026-10-25)
- **Data:** 2026-10-25
- **Cartão:** F1-10 (#38)
- **Relacionado:** [ADR 0003](0003-app-offline-first-sync-proprio.md), [ADR 0013](0013-sessoes-bff-e-tokens.md), [ADR 0017](0017-app-web-bff-mesma-origem.md); arquitetura §3 e §7.3; threat model TM-DEV-01.

## Contexto

A app é offline-first e a interface lê sempre da base local (ADR 0003). O F1-10 implementa
essa base no browser antes de existir a fila de sync do F1-13. A escolha tem de evitar perda
de dados quando a rede falha, não expor tokens e não transformar uma falha de armazenamento
num ecrã partido.

O pacote `wa-sqlite` do npm está em 1.0.0 (jan/2024). O repositório oficial está em v1.1.2
(ago/2026) e publica os artefactos em `dist/`.

## Decisão

### Fonte e versão

- Usar `wa-sqlite` do repositório oficial, fixado ao commit
  `2bf1c59d89eb6497535a4217bc62fec68a0bb994` (tag v1.1.2), não ao npm antigo.
- A tag declara `"version": "1.1.1"` no `package.json`; o lockfile pode mostrar 1.1.1.
- O VFS escolhido está em `src/examples/` e não é o propósito principal da biblioteca. É
  código nosso para efeitos de suporte: fica coberto pelos testes E2E e só é atualizado com
  revisão. Renovate fica explicitamente desativado para este pacote.
- Alternativa aprovada para o futuro: vendorizar `dist` e o VFS com SHA-256.

### VFS e worker

- SQLite corre num **Worker dedicado**, com uma ligação por tab.
- VFS: **`OPFSCoopSyncVFS`**.
  - É síncrono (`wa-sqlite.mjs`), suporta várias ligações/abas e é transparente na OPFS.
  - Não exige COOP/COEP.
  - A OPFS só está disponível em contexto de Worker.
- O primeiro spike deu `SQLITE_BUSY` no `BEGIN` porque havia
  `PRAGMA busy_timeout=5000`: o busy handler síncrono impedia o lock assíncrono do VFS de
  progredir. O `busy_timeout` foi removido.
- As transações e migrações passam por `withTransaction`: até 10 tentativas ou 2 s, com
  `ROLLBACK`, backoff de 25–250 ms e `LocalDatabaseBusyError` no fim.
- `OPFSWriteAheadVFS` foi rejeitado: depende de `readwrite-unsafe`, limitado ao Chromium, e
  viola o RNF de plataformas (Safari/iOS/Firefox).
- `OPFSAnyContextVFS` foi rejeitado por exigir build async e ter escrita muito lenta.
- `AccessHandlePoolVFS` foi rejeitado por não suportar múltiplas ligações.
- `MemoryVFS` só se usa em testes; nunca há fallback silencioso em produção.

### Ficheiro e ciclo de vida

- Uma base por utilizador: `pitbox/<sha256(user_id)>/db.sqlite3` na OPFS. O hash separa
  contas; não é uma medida de privacidade.
- A base é aberta depois de `/auth/session`; `user_id` nunca vem de URL ou storage.
- No logout, escreve-se `pitbox/<hash>/.logout-pending`, fecha-se o worker, publica-se
  `logout` em `BroadcastChannel("pitbox-db")` e apaga-se a pasta inteira. Outras abas fecham
  a ligação e voltam ao login.
- No arranque só se apagam pastas com `.logout-pending`. Nunca se apaga por falta de sessão:
  rede em baixo ou cookie expirado não podem destruir dados offline.
- Quando o utilizador muda, `queryClient.clear()` e nova base/worker antes de renderizar a
  área autenticada.

### Esquema e migrações

- Nomes locais iguais aos do servidor, sem prefixo de esquema.
- Tabelas iniciais: `app_meta`, `team`, `scale`, `powertrain`, `car`, `engine`,
  `engine_mount`, `tyre_set`, `part`, `stock_movement`, `part_install`, `field_definition`,
  `setup`, `setup_version`.
- IDs novos UUIDv7 no cliente; `created_at`, `updated_at`, `version`, `deleted_at` nas fichas
  editáveis; `setup_version` imutável.
- Migrações forward-only em `src/db/migrations`, com `schema_migrations(version, name,
  checksum, applied_at)` e checksum SHA-256 com line endings normalizados. Cada migração
  corre em `BEGIN IMMEDIATE`, com rollback em erro.
- `PRAGMA foreign_keys=ON`, `trusted_schema=OFF`, `synchronous=FULL` e `quick_check` no
  arranque; sem `busy_timeout`. As transações são repetidas por `withTransaction` quando o
  VFS devolve `SQLITE_BUSY`.

### Leitura e escrita

- Repositórios em `features/<f>/data`; a UI e as rotas não importam `@/db` nem `@/sync`.
- Toda a leitura passa por Zod; uma linha inválida gera `CorruptLocalDataError` e um estado
  recuperável, nunca um crash.
- SQL sempre parametrizado.
- No F1-10 não há fila de escrita offline. As equipas são um bootstrap read-through
  temporário (`ListMyTeams` → tabela local); a UI lê a tabela local. O F1-13 substitui o
  bootstrap pelo `Pull`.

### Segurança

- No browser não há SQLCipher; guardam-se apenas dados necessários, sem tokens, cookies,
  emails, segredos ou custos de peças de um piloto. A cifra nativa é F2-06.
- CSP: `script-src 'self' 'wasm-unsafe-eval'`, `worker-src 'self'`; sem `'unsafe-eval'` nem
  `'unsafe-inline'`. A resposta do worker tem a sua própria CSP e o WASM é servido como
  `application/wasm`.
- Dados locais apagados no logout explícito; TM-DEV-01 fica **parcial** até ao F2-06.

### F1-11: schema local de garagem

- O stock de peças é guardado em **centi** (`min_stock_centi`, `qty_delta_centi`), como no
  proto e no servidor. A migração local `0004` reconstrói `part`, `stock_movement` e
  `part_install` porque as tabelas antigas usavam `REAL` e estavam vazias; a conversão é feita
  com `round(x * 100)` e testada com `PRAGMA foreign_key_check`. A migração `0005`
  acrescenta o índice `stock_movement (team_id, part_id)` usado pela agregação de stock.
- `version` continua a representar a versão do servidor; a concorrência entre abas usa
  `local_rev`, que não vai para o sync.
- `unit_cost_cents` permanece na base local, mas o F1-11 não o lê nem mostra; a regra de
  custos por papel entra no F1-11b/F1-13.
- O stock inicial é um movimento `purchase` na mesma transação da peça; movimentos
  posteriores ficam para o F1-11b.
- A garagem lê com TanStack Query em `networkMode: "always"`; continua a ser um repositório
  local e não faz pedidos de rede.

## Consequências

- A matriz de browsers da especificação §14 (Chrome, Safari e Firefox nas duas últimas
  versões; iOS 16+) tem de ser validada com OPFS; não há fallback para memória.
- O login continua a exigir `/auth/session`; abrir a app sem rede leva ao login até ao passe
  offline do F1-13, mesmo que os dados locais existam.
- O bootstrap de equipas é uma ponte temporária; não há resolução de conflitos nem escrita
  offline antes do F1-13.
- O lock multi-aba e o marcador `.logout-pending` são testados em E2E; não são uma garantia
  de durabilidade contra uma falha de hardware ou remoção do perfil do browser.
- Atualizar `docs/02-arquitetura-tecnica.md`, `docs/threat-model/ameacas.md` e `app/README.md`
  quando a implementação fechar.

## Alternativas consideradas

| Alternativa | Porque não |
| --- | --- |
| npm `wa-sqlite@1.0.0` | SQLite antigo e sem atualizações do VFS |
| `@sqlite.org/sqlite-wasm` 3.54.1 | Contraria a stack/ADR 0003; exige um ADR próprio |
| `OPFSWriteAheadVFS` | Depende de `readwrite-unsafe` (Chromium) e viola o RNF de plataformas |
| `OPFSAnyContextVFS` | Escrita muito lenta; exige build async |
| `AccessHandlePoolVFS` | Uma só ligação; não suporta abas |
| IndexedDB como base principal | Não é SQLite; perde queries/constraints e o formato do sync |
| SQLCipher na web | Fora do âmbito; F2-06 para o nativo |
