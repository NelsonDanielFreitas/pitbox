# 0006 — PostgreSQL + TimescaleDB em vez de uma base de séries temporais dedicada

- **Estado:** Aceite (Nelson, 2026-10-11)
- **Data:** 2026-10-11 (decisão); registado em 2026-10-12
- **Cartão:** F0-07 (registo); origem: arquitetura §1, §7

## Contexto

Os dados são sobretudo relacionais (setups, garagem, provas), com um volume de séries temporais (telemetria a ~200 Hz) que justifica uma extensão, não uma segunda base.

## Decisão

PostgreSQL 16 com TimescaleDB para `telemetry.sample`; um esquema por módulo, RLS por equipa, FKs compostas, UUIDv7, sqlc + pgx. Migrações com Atlas.

**O que mudou desde a arquitetura original** (e que este ADR incorpora por ligação):

- **Migrações escritas à mão, sem `migrate diff` nem `migrate lint`** do Atlas (a §4.5 e a §14 diziam o contrário): ver [ADR 0010](0010-migracoes-atlas-community.md).
- **Sem compressão** na telemetria enquanto houver RLS, TimescaleDB obrigatória na migração (a alternativa "Cloud SQL + tabelas particionadas" da §11.1 deixa de valer sem novo ADR) e agregações contínuas sem RLS por resolver no F4: ver [ADR 0011](0011-telemetria-sem-compressao.md).

## Alternativas consideradas

| Alternativa | Porque não |
| --- | --- |
| PostgreSQL + InfluxDB | Duas tecnologias, duas políticas de acesso, e o RLS por equipa teria de ser replicado |
| PostgreSQL com tabelas particionadas | É a alternativa de recurso; exigiria um ADR novo (ver ADR 0011) |

## Consequências

- Uma só barreira de isolamento a manter (RLS) para dados relacionais e de telemetria.
- O volume de telemetria sem compressão é uma estimativa a confirmar (ver ADR 0011, gatilho de revisão aos ~20 GB).
