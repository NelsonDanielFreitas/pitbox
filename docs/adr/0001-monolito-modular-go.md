# 0001 — Monólito modular em Go em vez de microserviços na v1

- **Estado:** Aceite (Nelson, 2026-10-11)
- **Data:** 2026-10-11 (decisão); registado em 2026-10-12
- **Cartão:** F0-07 (registo); origem: `docs/00-contexto-do-projeto.md` §3 e arquitetura §1, §2

## Contexto

A equipa é de 1–2 pessoas e o produto tem de ser operável a baixo custo. O backend tem várias áreas (identity, garage, setup, maintenance, events, timing, telemetry, sync) que precisam de fronteiras claras, mas não de escalas independentes na v1.

## Decisão

Um monólito modular em Go: três binários (`pitbox-api`, `pitbox-worker`, `pitbox-ingest`) sobre o mesmo código, com um módulo por domínio, cada um com o seu esquema na base de dados. Camadas `transport → app → domain`, `store` implementa `app/ports.go`; um módulo nunca importa `domain`, `app` ou `store` de outro (só `api.go` ou eventos na outbox). Separar um módulo num serviço é uma mudança de deploy, não uma reescrita.

## Alternativas consideradas

| Alternativa | Porque não |
| --- | --- |
| Microserviços desde o início | Custo operacional (rede, deploys, observabilidade distribuída) sem ganho para 1–2 pessoas |
| .NET ou Node (experiência existente) | O objetivo inclui aprender Go; o ecossistema cloud-native e o gRPC são de primeira classe em Go |

## Consequências

- As regras de dependência entre camadas e módulos são não negociáveis (`CLAUDE.md`); a forma de as vigiar automaticamente (linters, testes de arquitetura) ainda não está decidida.
- `pitbox-ingest` fica isolado do `pitbox-api` porque processa ficheiros externos volumosos (ver threat model).
- Rever quando houver mais de 3 processos com escalas diferentes (gatilho do ADR 0008).
