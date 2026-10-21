# PitBox — Contexto do projeto

Este ficheiro resume tudo o que foi decidido até 11 de outubro de 2026. Lê-o primeiro; os restantes ficheiros têm o detalhe.

## 1. Quem sou e porque existe este projeto

- Engenheiro informático. Trabalhei em backend com FastAPI, .NET e Node.js.
- A tirar o mestrado em Cibersegurança na Universidade do Minho.
- Objetivo de carreira: trabalhar como engenheiro de segurança/plataforma em grandes empresas e infraestruturas (cloud security, application security, platform security). Quero aprender tecnologias com futuro: Go, Rust, gRPC, Kubernetes, Zero Trust, segurança da cadeia de fornecimento.
- Hobby: automodelismo 1/8 TT nitro (todo-o-terreno a combustão) com o meu pai. Eu sou o mecânico, ele é o piloto.
- O PitBox é o meu projeto paralelo durante o mestrado. Tem dois objetivos com o mesmo peso:
  1. Ser útil de verdade nas nossas provas (setups, motores, finais com reabastecimentos, telemetria).
  2. Ser um portfólio profissional que demonstre engenharia de backend, plataforma e cibersegurança ao nível de uma grande empresa, podendo ir para produção.

## 2. O que é o PitBox

Plataforma para equipas de 1/8 TT nitro, com quatro partes que partilham os mesmos dados:

1. **Setups e garagem**: fichas de setup versionadas (diferenciais, amortecedores, geometria, motor, transmissão, pneus, condições da pista), carros, motores, pneus, peças, stock e diário de manutenção.
2. **Gestão de provas**: horário, mangas, checklists, resultados e live timing importado, análise de consistência e do efeito das alterações de setup.
3. **Modo final**: ecrã para o mecânico durante a final, com voltas por depósito, aviso de reabastecimento antes da janela, registo de paragens com um toque e funcionamento 100% sem rede.
4. **Telemetria**: módulo ESP32-S3 no carro (temperatura da cabeça, rotações, tensão do recetor, IMU), gravação em cartão e envio por LoRa 868 MHz para um recetor na box ligado à app por BLE.

## 3. Decisões técnicas já tomadas (não mudar sem um ADR)

| Área | Decisão |
| --- | --- |
| Forma do backend | Monólito modular em Go: um binário `pitbox-api` + `pitbox-worker` + `pitbox-ingest`, módulos isolados por domínio; microserviços só quando houver razão |
| Linguagens | Go (backend), TypeScript (app), Rust (firmware e componentes de baixo nível), Python (análise de dados) |
| API | Protobuf + Buf; Connect (`connect-go`), que fala gRPC, gRPC-Web e HTTP/JSON |
| Camadas por módulo | `transport → app → domain`; `store` implementa as interfaces de `app/ports.go`; `domain` não importa nada de infraestrutura |
| Base de dados | PostgreSQL 16 + TimescaleDB para telemetria; esquema por módulo; RLS por equipa; FKs compostas `(team_id, x_id)`; UUIDv7; sqlc + pgx; migrações com Atlas |
| Identidade | Zitadel (OIDC + PKCE, passkeys, MFA); padrão BFF com cookie `__Host-` HttpOnly na web; AppAuth no telemóvel |
| Autorização | OpenFGA por chamada + RLS no Postgres (dupla barreira) |
| App | React + Vite + TypeScript strict, TanStack Router/Query, Tailwind + Radix, SQLite local (wa-sqlite / SQLCipher), PWA + Capacitor |
| Offline | Offline-first; protocolo de sync próprio (`SyncService` Push/Pull, op_id idempotente, assinatura ECDSA por dispositivo, passe offline de 72 h) |
| Firmware | Rust com `esp-hal` + Embassy em ESP32-S3; secure boot v2, flash cifrada, firmware assinado Ed25519; LoRa autenticado com ChaCha20-Poly1305 |
| Infraestrutura | Fase 1: Cloud Run + serviços geridos na UE, OpenTofu, Cloudflare à frente. Fase 2: Kubernetes gerido + Istio ambient + Kyverno |
| Supply chain | GitHub Actions com ações fixadas por SHA, Syft (SBOM), Trivy, cosign, proveniência SLSA, Renovate |
| Observabilidade | OpenTelemetry, Grafana Cloud, Sentry; alertas por SLO |
| Ambiente de desenvolvimento | WSL2 Ubuntu 24.04; código em `~/` (nunca em `/mnt/c`); mise para versões; Docker Compose para serviços; `usbipd-win` para o ESP32 |
| Gestão | Monorepo no GitHub; GitHub Projects com os cartões do plano; um cartão = um branch + PR |

## 4. Ficheiros deste projeto

| Ficheiro | Conteúdo |
| --- | --- |
| `docs/01-especificacao-funcional.md` | O quê: perfis, autenticação, funcionalidades, requisitos não funcionais, RGPD, roadmap |
| `docs/02-arquitetura-tecnica.md` | Como: app, backend, identidade, autorização, dados, sync, telemetria, infraestrutura, segurança, CI/CD, custos, estrutura do repositório |
| `docs/03-plano-de-trabalho.md` | 80 cartões em 7 fases (F0 a F6), com tecnologias por fase e critério "Pronto quando" |
| `backend/db/schema/schema.sql` | Esquema PostgreSQL completo (58 tabelas, RLS, triggers), testado em PostgreSQL 16 |
| `backend/db/tests/rls_and_constraints_test.sql` | Testes de isolamento entre equipas, FKs compostas, imutabilidade e vistas |
| `backend/db/README.md` | Resumo da base de dados e regras de segurança incorporadas |

Os originais editáveis estão nos documentos "PitBox — Especificação Funcional", "PitBox — Arquitetura Técnica" e "PitBox — Plano de Trabalho" no Claude.

## 5. Estrutura do backend acordada

```text
backend/
├─ cmd/{api,worker,ingest}/main.go     # finos; chamam internal/bootstrap
├─ gen/                                 # gerado pelo Buf
├─ db/{schema,migrations,tests}/
├─ authz/{model.fga,model.fga.yaml}
└─ internal/
   ├─ bootstrap/
   ├─ platform/{config,log,otel,db,auth,authz,interceptors,outbox,idempotency,audit,httpserver,clock,testutil}/
   └─ <módulo>/{module.go,api.go,domain/,app/,store/{queries,sqlcgen},transport/}
      # módulos: identity, garage, setup, maintenance, events, timing, telemetry, sync
```

Regras: um módulo nunca importa `domain`, `app` ou `store` de outro (só `api.go` ou eventos da outbox); autorização e transação vivem em `app`; código gerado nunca é editado à mão.

## 6. Estado atual

- Documentação concluída: especificação, arquitetura, plano, esquema da base de dados.
- Código: ainda não começou. Próximo passo: **Fase 0, cartão F0-01** (criar o monorepo), seguido do primeiro módulo vertical (`identity`: criar equipa e listar as minhas equipas).

## 7. Correções pendentes ao plano

- Falta separar a autenticação em cartões próprios. Proposta:
  - **F1-03a**: login, logout e BFF
  - **F1-03b**: registo, verificação de email, criação do `user_account` no primeiro login
  - **F1-03c**: passkeys, MFA obrigatório para donos, códigos de recuperação, recuperação de conta, gestão de sessões e step-up
  - Login móvel (AppAuth) dentro do F2-06; passe offline de 72 h dentro do F1-13.
- No WSL, acrescentar ao F0-02 a configuração `.wslconfig` (memória, `networkingMode=mirrored`), `core.autocrlf=input` e `usbipd-win`.

## 8. Questões em aberto

- O regulamento das provas onde corremos permite telemetria a bordo em treinos, mangas e finais?
- Que plataformas de live timing usam essas provas, que exportações oferecem e o que os termos permitem?
- A plataforma abre a outras equipas na v1.0 ou só por convite?
- Se o módulo de telemetria vier a ser vendido ou distribuído na UE, o Cyber Resilience Act aplica-se (obrigações de reporte desde 11/09/2026; requisitos completos e marcação CE a partir de 11/12/2027). Enquanto for só para uso próprio, não.
- Limites exatos da banda 868 MHz (sub-banda, duty cycle, potência) a confirmar na ETSI EN 300 220 e na ANACOM antes de produção.
