# 0016 — Motorização e escala como atributos extensíveis do carro

- **Estado:** Aceite (Nelson, 2026-10-20)
- **Data:** 2026-10-20
- **Cartão:** F1-07a (#114)
- **Relacionado:** [ADR 0001](0001-monolito-modular-go.md),
  [ADR 0005](0005-openfga-rls-dupla-barreira.md),
  [ADR 0006](0006-postgres-timescaledb.md),
  [ADR 0010](0010-migracoes-atlas-community.md); arquitetura §7.2 e §8;
  F1-07 (#35), F1-13, F6-01 (#115), F6-02 (#116), F6-03 (#117), F6-04 (#118),
  F6-05 (#119), F6-06 (#125).

## Contexto

A v0.1 do PitBox é 1/8 TT nitro: motores com depósitos, finais com reabastecimentos,
telemetria de cabeça do motor e rotações. A Fase 6 alarga o produto a **outras escalas e
motorizações** — elétricos (1/8 e-buggy, 1/10, …) e combustão noutras escalas (1/10 nitro,
1/5 gasolina) — e a lista não é fechada. Escalas e motorizações novas têm de entrar sem
reescrever a garagem, a manutenção e o modo final, e sem mudar os contratos.

Hoje `garage.car` não tem esta informação e `garage.engine` pressupõe combustão. É barato
introduzir as costuras agora, antes de existirem dados e app; depois é uma migração de
dados e de clientes.

Há duas restrições estruturais a respeitar:

- **A app é offline-first e lê sempre da base local** (regra 7 do `CLAUDE.md`): nenhum ecrã
  chama a API. O modo final tem de funcionar sem rede. Os catálogos são dados de referência
  globais, por isso não viajam no `Pull` por equipa/`change_seq` já desenhado.
- **Os catálogos são dados de referência globais**, não dados de uma equipa. Não têm
  `team_id`, não têm RLS e só as migrações os alteram.

## Decisão

### 1. Escala: catálogo global

`garage.scale`:

```sql
code        text        PRIMARY KEY CHECK (code ~ '^1/[1-9][0-9]?$'),
name        text        NOT NULL,
active      boolean     NOT NULL DEFAULT true,
sort_order  smallint    NOT NULL DEFAULT 0,
created_at  timestamptz NOT NULL DEFAULT now(),
```

`garage.car.scale` é FK para `garage.scale(code)`, `NOT NULL DEFAULT '1/8'`.

Uma escala nova é uma linha nova. O `code` é imutável: corrige-se com uma linha nova e
desativando a antiga; não se faz `UPDATE` ao `code`. As linhas nunca se apagam, só ficam
`active = false` (a FK é `NO ACTION`, por isso uma linha referida nem pode ser apagada).

### 2. Motorização: família fechada + catálogo aberto

- **Família fechada:** enum `garage.powertrain_family` (`combustion`, `electric`). A família
  decide o comportamento (depósitos/reabastecimento vs baterias/cargas). Uma família nova é
  uma alteração estrutural (enum + código), não um simples dado.
- **Catálogo aberto:** `garage.powertrain`:

```sql
code        text        PRIMARY KEY CHECK (code ~ '^[a-z][a-z0-9_]{0,31}$'),
family      garage.powertrain_family NOT NULL,
name        text        NOT NULL,
active      boolean     NOT NULL DEFAULT true,
sort_order  smallint    NOT NULL DEFAULT 0,
created_at  timestamptz NOT NULL DEFAULT now(),
```

`garage.car.powertrain` é FK para `garage.powertrain(code)`, `NOT NULL DEFAULT 'nitro'`.
Uma motorização nova dentro de uma família é uma linha nova; não altera o esquema de `car`
nem os contratos.

### 3. Colunas no carro (expand)

```sql
ALTER TABLE garage.car
  ADD COLUMN scale      text NOT NULL DEFAULT '1/8',
  ADD COLUMN powertrain text NOT NULL DEFAULT 'nitro';

ALTER TABLE garage.car
  ADD CONSTRAINT car_scale_fk      FOREIGN KEY (scale)      REFERENCES garage.scale (code)      NOT VALID,
  ADD CONSTRAINT car_powertrain_fk FOREIGN KEY (powertrain) REFERENCES garage.powertrain (code) NOT VALID;

ALTER TABLE garage.car VALIDATE CONSTRAINT car_scale_fk;
ALTER TABLE garage.car VALIDATE CONSTRAINT car_powertrain_fk;
```

O `DEFAULT` constante é metadata-only (sem reescrever a tabela). As FKs são `NOT VALID` e
validadas depois, para não bloquear escritas. Os carros existentes ficam `1/8`/`nitro`; o
comportamento de 1/8 nitro não muda.

### 4. Sementes e semântica de `active`

Na v0.1 semeiam-se **só** `1/8` (ativa) e `nitro` (combustão, ativa). Outras escalas e
motorizações entram por migrações de dados na Fase 6 (F6-06 e seguintes); não se fixam já
códigos que a Fase 6 pode querer nomear de outra forma.

**Inativo** significa “não pode ser escolhido para um carro novo nem para uma mudança de
valor”:

- `CreateCar` valida o valor escolhido (ou o `DEFAULT`) contra o catálogo ativo.
- `UpdateCar` só valida o catálogo quando o valor **muda**. Editar o nome de um carro que tem
  uma escala/motorização entretanto desativada continua a funcionar.
- A app mostra o valor antigo (o catálogo local inclui inativos com a flag), mas o formulário
  de escolha só oferece ativos.
- Num `CreateCar`/`UpdateCar` online, um valor novo/mudado inativo ou inexistente é
  `INVALID_ARGUMENT`.
- Num push offline (F1-13), o RPC do sync não falha por isso: a **operação** fica
  `rejected` em `sync.op_status`, com o motivo; o pormenor do formato fica para o F1-13. A
  app mostra a operação rejeitada e pede para escolher outro valor.
- O `code` não se muda e as linhas não se apagam; uma substituição é uma linha nova + a
  antiga desativada.

### 5. Contratos do F1-07

- `Car.scale` e `Car.powertrain` como `string` no `Car`, `CreateCarRequest` e
  `UpdateCarRequest`.
- `CreateCarRequest`: `optional string scale` e `optional string powertrain`; ausentes →
  `1/8`/`nitro`.
- Protovalidate:
  - `scale`: `^1/[1-9][0-9]?$`, `max_len = 8`;
  - `powertrain`: `^[a-z][a-z0-9_]{0,31}$`, `max_len = 32`.
- **Não** há enum de proto: uma escala/motorização nova não obriga a regenerar clientes.
- O servidor valida o catálogo **na mesma transação** do caso de uso: tem de existir e, para
  um valor novo/mudado, estar ativo; senão `INVALID_ARGUMENT` com mensagem fixa (o catálogo é
  global, não é um recurso de equipa — não faz sentido `NOT_FOUND`).
- Não se expõe `powertrain_family` no `Car` na v0.1: a app tem o catálogo local e deriva a
  família de lá. Se um cliente externo precisar, um campo só de leitura pode ser acrescentado
  sem quebrar o contrato.
- `UpdateCar` usa os mesmos padrões e a mesma validação condicional; a concorrência é a
  `version` otimista do F1-07.

### 6. Offline e sincronização

Os catálogos são globais; o `Pull` é por equipa. A decisão é:

- **`catalog_version` é um hash do conteúdo dos catálogos**, calculado na leitura. Não há
  coluna `updated_at`, nem trigger `touch_catalog`. O hash é SHA-256 sobre uma forma
  canónica: linhas ordenadas por `code` e campos numa ordem fixa (`code`, `name`, `active`,
  `sort_order`; em `powertrain` também `family`), com comprimentos prefixados para não haver
  ambiguidade. O valor é opaco para o cliente e compara-se por igualdade.
- O hash e o snapshot são produzidos por `garage/api.go` (a interface pública do módulo,
  regra 2). A camada de sync do F1-13 chama essa interface **em processo**, com o pool da
  aplicação e sem contexto de equipa (os catálogos são globais), e transporta o snapshot e o
  hash no `PullResponse`. Não há `CatalogService` nem RPC de catálogo chamado por ecrãs.
- O `PullResponse` do F1-13 inclui `catalog_version` (o hash) e, quando o cliente envia uma
  versão diferente, o snapshot dos catálogos. O cliente guarda a versão global do catálogo
  separada do cursor `change_seq` da equipa.
- Quando a versão muda, a **camada de sync** (nunca um ecrã) substitui as tabelas locais do
  SQLite numa transação e atualiza a versão. A UI lê sempre do SQLite.
- A app traz um **snapshot de build** dos catálogos iniciais e do hash, para o primeiro
  arranque sem rede. O sync substitui-o quando o hash muda.
- Teste futuro (F1-13): catálogo local desatualizado → o sync recebe o hash novo, atualiza o
  SQLite e a UI vê a escala/motorização nova sem chamar a API.
- O hash é recalculado a cada leitura (é barato: dezenas de linhas); não há estado de versão
  na base nem trigger para manter.

### 7. Permissões e RLS

- `garage.scale` e `garage.powertrain` são globais: sem `team_id` e **sem RLS**, como
  `telemetry.firmware_release`.
- `pitbox_app` e `pitbox_readonly` têm só `SELECT`; `INSERT`/`UPDATE`/`DELETE` são revogados
  explicitamente.
- No `schema.sql`, o `REVOKE` fica **depois** do bloco `GRANT ... ON ALL TABLES` (como
  `platform.outbox`/`telemetry.firmware_release`), senão o `GRANT` em bloco anulava-o.
- Na migração, o `REVOKE` vem depois da criação das tabelas (os *default privileges* dariam
  DML).
- No `structure_test.sql`, os catálogos entram na lista explícita de tabelas sem RLS e nas
  asserções de privilégios de `pitbox_app` (o precedente é `telemetry.firmware_release`).
- Uma tabela nova deixa de ser legível por `pitbox_readonly` só se for sensível; os catálogos
  são dados de referência e ficam legíveis.

### 8. Energia e manutenção

- As regras de manutenção e a estratégia da final ficam atrás de uma interface escolhida pela
  **família** (`EnergyModel`/`MaintenanceCounter`), com só a implementação de combustão na
  v0.1.
- Nenhuma regra fora dessa implementação pressupõe “depósito”.
- Nenhum `if scale == "1/8"`: valores por omissão por escala vêm do catálogo/configuração.
- `garage.engine` continua específico de combustão; F6-02 acrescenta bateria, motor brushless
  e ESC. No F1-07, montar um motor de combustão num carro elétrico (e vice-versa) é recusado
  pelo caso de uso, com teste. Quando os componentes tiverem família (F6-02), a regra
  generaliza-se: componente e carro têm de partilhar a família.
- `identity.team.category` continua texto livre, descritivo, sem lógica. A equipa pode ter
  carros de escalas diferentes; o carro é a autoridade.

## Alternativas consideradas

| Alternativa | Porque não |
| --- | --- |
| Escala/motorização só na equipa | Uma equipa pode ter várias escalas; bloqueia o caso real |
| Enum fixo no Postgres e no proto | Cada escala/motorização nova obriga a migração e a regenerar clientes |
| Tabelas separadas por tipo (nitro/electric) | Duplica garagem, setups, manutenção e modo final; queries cruzadas |
| Texto livre ou JSON | Sem integridade referencial, typos, valores inconsistentes, sem catálogo |
| Um catálogo único escala+motorização | São duas dimensões independentes; um carro tem as duas |
| Catálogo lido por um ecrã da app | Viola a regra 7 (offline-first, a app lê da base local) e não funciona sem rede |
| Catálogo só no build da app | Uma escala/motorização nova obrigaria a uma release da app |
| Catálogo por equipa | Duplica dados globais; não há razão para isolar referência |
| Tabela própria `catalog_version` | Mais uma tabela global para um valor derivável; o hash do conteúdo calculado na leitura chega |

## Consequências

- Migração **expand**: catálogos, colunas com `DEFAULT` constante e FKs `NOT VALID` +
  `VALIDATE`; sem reescrever `car` e sem mudar o comportamento de 1/8 nitro.
- Carros existentes ficam `1/8`/`nitro`.
- Uma escala/motorização nova é uma migração de dados; uma família nova é uma migração de
  enum + código + comportamento.
- O `PullResponse` e a camada de sync ganham `catalog_version` (hash do conteúdo)/catálogo
  local (F1-13). O `garage/api.go` expõe o snapshot/hash à camada de sync. A app ganha um
  snapshot de build.
- O F1-07 herda os campos, a validação condicional e a compatibilidade motor/carro; F6-01 a
  F6-06 implementam templates, elétricos, modo final, telemetria e as outras escalas.
- Os catálogos são auditáveis por revisão de migrações; a app não os pode alterar.
- Contagens: `structure_test.sql` passa de 57 para 59 tabelas (dois catálogos); a
  documentação de 56 para 58. A `ownership_transfer` do F1-06 já está contada.

## Prova / testes

- `catalog_test.sql` (como `postgres`, `BEGIN/ROLLBACK`): insere uma escala e uma
  motorização novas, cria um carro com elas e confirma que funciona — sem alterar o esquema
  nem os contratos. O hash do conteúdo é testado no `garage/api.go`/F1-13 (não há trigger nem
  `updated_at`).
- `rls_and_constraints_test.sql` (como `pitbox_app`): carro sem os campos fica `1/8`/`nitro`;
  escala/motorização inexistente → `foreign_key_violation`; `INSERT/UPDATE/DELETE` nos
  catálogos → `insufficient_privilege`.
- `structure_test.sql`: exceções sem RLS e contagem; `SELECT` sim, DML não em
  `pitbox_app`; seeds `1/8`/`nitro`.
- F1-07: `CreateCar` com valor ativo; `CreateCar` com inativo/inexistente →
  `INVALID_ARGUMENT`; `UpdateCar` com o mesmo valor inativo (a mudar outro campo) funciona;
  `UpdateCar` para um valor inativo → `INVALID_ARGUMENT`; montagem motor/carro por família.
- F1-13: catálogo local desatualizado é atualizado pela camada de sync; nenhum ecrã chama a
  API.
- `make db-test`, `make lint`, `buf breaking` (sem alterações de proto no F1-07a).

## Segurança (STRIDE)

| Ameaça | Mitigação |
| --- | --- |
| **T** valor inválido parte a lógica | padrão/tamanho no contrato, catálogo ativo validado no servidor, FK na base |
| **T** equipa/alteração altera o catálogo global | catálogos só de leitura para `pitbox_app`/`pitbox_readonly`; DML revogado; só migrações revistas |
| **D** migração bloqueia a tabela de carros | `DEFAULT` constante (metadata-only), FKs `NOT VALID` + `VALIDATE` |
| **I** catálogos expõem dados | dados de referência, sem PII; a RLS dos carros continua a isolar equipas |
| **E** catálogo usado como autorização | a escala/motorização é descritiva/comportamental; as permissões continuam no OpenFGA + RLS |
| **R** alteração de catálogo sem rasto | só migrações; a app não escreve; a auditoria de carros regista os valores |
| **T** catálogo local adulterado na app | o servidor valida sempre na escrita; o catálogo local é só para escolha/leitura |

## Migração e ordem

1. Criar enum, catálogos e seeds `1/8`/`nitro`.
2. Adicionar as colunas ao `car` com `DEFAULT`.
3. Criar as FKs `NOT VALID` e validar.
4. `REVOKE INSERT, UPDATE, DELETE` dos catálogos a `pitbox_app` (e, por clareza,
   `pitbox_readonly`); `SELECT` fica.
5. `schema.sql`: catálogos antes de `garage.car`, FKs inline, seeds e `REVOKE` **depois** do
   bloco de permissões.
6. Testes de estrutura, RLS e catálogo; `make db-test`.

## Aceitação

Aceite pelo Nelson em 2026-10-20, com três ajustes:

1. `catalog_version` é um **hash do conteúdo** calculado na leitura, sem `updated_at` nem
   trigger `touch_catalog`.
2. Um valor desativado num push offline fica **`rejected`** na operação (`sync.op_status`),
   não como erro do RPC; o pormenor fica para o F1-13.
3. O catálogo é servido à camada de sync por **`garage/api.go`** (regra 2), não por um
   `CatalogService`/RPC de ecrã.

Com a aceitação, segue-se a migração, o `schema.sql` e os testes SQL.
