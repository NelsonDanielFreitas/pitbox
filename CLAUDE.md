# PitBox

Plataforma para equipas de automodelismo 1/8 TT nitro: setups, motores, provas, modo final com reabastecimentos e telemetria de um módulo ESP32 no carro. É um projeto pessoal (eu sou o mecânico, o meu pai o piloto) e, ao mesmo tempo, um portfólio de engenharia de backend, plataforma e cibersegurança, pensado para ir para produção.

@docs/00-contexto-do-projeto.md

## Documentação (ler quando relevante; não está carregada por omissão)

- `docs/01-especificacao-funcional.md` — o que o sistema faz (perfis, autenticação, funcionalidades, RNF, RGPD)
- `docs/02-arquitetura-tecnica.md` — como está construído (módulos, identidade, autorização, dados, sync, telemetria, infra, segurança)
- `docs/03-plano-de-trabalho.md` — cartões F0-01 a F5-14 com "Pronto quando"
- `docs/adr/` — decisões de arquitetura; `docs/threat-model/` — STRIDE; `docs/runbooks/` — um por alerta
- `backend/db/schema/schema.sql` — estado desejado da base de dados (55 tabelas, RLS)

Antes de implementar uma funcionalidade, lê a secção da especificação e da arquitetura correspondente e o cartão do plano.

## Como trabalhar neste repositório

- Comunica comigo em **português de Portugal**. Código, identificadores, comentários e commits em **inglês**.
- Estou a aprender Go e Rust: quando usares um padrão ou armadilha da linguagem, explica-o em 2–3 frases.
- Todo o trabalho pertence a um cartão do plano. Diz qual (ex.: "F1-07"). Se não houver, propõe um cartão com "O que fazer" e "Pronto quando".
- Tarefas não triviais: primeiro um plano curto (ficheiros, abordagem, riscos de segurança); avança depois de eu confirmar. Tarefas pequenas e claras: avança logo.
- Segue as decisões existentes. Para mudar uma decisão estrutural, propõe um ADR em `docs/adr/NNNN-titulo.md` (contexto, decisão, alternativas, consequências) e espera pela minha confirmação.
- Nunca inventes nomes de pacotes, funções ou flags. Na dúvida sobre uma versão ou API, verifica (documentação, `go doc`, `cargo doc`, `--help`) ou diz que é preciso confirmar.
- Não faças commit, push ou alterações destrutivas (apagar ficheiros, `git reset --hard`, migrações destrutivas) sem eu pedir.
- No fim de cada tarefa: corre os testes e linters relevantes, mostra o resultado, faz uma revisão de segurança curta (ver "Checklist de segurança") e indica o próximo passo.

## Estrutura do monorepo

```text
pitbox/
├─ CLAUDE.md  README.md  Makefile  mise.toml  .editorconfig  .gitignore
├─ .claude/rules/          # regras por área (carregadas quando mexes nesses ficheiros)
├─ .github/                # workflows, CODEOWNERS, renovate.json, PR template
├─ docs/                   # contexto, especificação, arquitetura, plano, adr/, threat-model/, runbooks/
├─ proto/pitbox/<módulo>/v1/*.proto   # contratos — fonte de verdade da API
├─ buf.yaml  buf.gen.yaml  # gera para backend/gen e app/src/gen
├─ backend/                # módulo Go (go.mod próprio)
│  ├─ cmd/{api,worker,ingest}/main.go
│  ├─ internal/{bootstrap,platform/*,<módulo>/{domain,app,store,transport}}
│  ├─ gen/                 # código gerado (Buf) — nunca editar
│  ├─ db/{schema,migrations,tests}/
│  └─ authz/{model.fga,model.fga.yaml}
├─ app/                    # React + Vite + TS + Capacitor (web, Android e iOS)
├─ firmware/               # workspace Rust: car-module/, box-receiver/, protocol/
├─ analysis/               # Python (uv): notebooks e scripts de análise
└─ deploy/{compose,tofu,helm}/
```

Cada pasta principal tem um `README.md` com o seu propósito; pastas só com `.gitkeep` são esqueleto à espera de código (apaga o `.gitkeep` quando criares o primeiro ficheiro).

Módulos do backend: `identity`, `garage`, `setup`, `maintenance`, `events`, `timing`, `telemetry`, `sync`.

## Stack (versões estáveis atuais; confirmar antes de fixar)

- **Backend**: Go 1.27, Connect (`connectrpc.com/connect`), protovalidate, pgx v5 + sqlc, Atlas, River, slog, OpenTelemetry
- **Dados**: PostgreSQL 16 + TimescaleDB, Valkey, armazenamento S3 (MinIO em local)
- **Identidade/autorização**: Zitadel (OIDC + PKCE, passkeys, MFA), BFF com cookie `__Host-`, OpenFGA + RLS
- **App**: React 19, Vite, TypeScript strict, TanStack Router/Query, Tailwind + Radix, Zod, wa-sqlite/SQLCipher, Capacitor 8, XState
- **Firmware**: Rust, `esp-hal` + Embassy, ESP32-S3, SX1262 (LoRa 868 MHz), postcard
- **Plataforma**: Docker Compose (local), Cloud Run + OpenTofu (fase 1), Cloudflare, GitHub Actions, Syft, Trivy, cosign, SLSA, Renovate
- **Ambiente**: WSL2 Ubuntu 24.04, mise para versões, `usbipd-win` para o ESP32

## Comandos

```bash
make up            # levanta Postgres, Valkey, Zitadel, OpenFGA, MinIO, Jaeger (deploy/compose)
make down | reset  # para | apaga volumes e recria
make gen           # buf generate + sqlc generate
make lint          # buf lint, golangci-lint, eslint, cargo clippy
make test          # testes de todo o repositório
make db-diff name=<nome>   # atlas migrate diff a partir de backend/db/schema
make db-test       # testes SQL de RLS e restrições

# Por área
cd backend  && go test -race ./... && golangci-lint run && govulncheck ./...
cd app      && pnpm test && pnpm lint && pnpm typecheck && pnpm e2e
cd firmware && cargo clippy -- -D warnings && cargo test -p protocol
buf lint && buf breaking --against '.git#branch=main'
```

Se um alvo do Makefile ainda não existir, cria-o quando a tarefa o exigir e diz-mo.

## Regras de arquitetura (não negociáveis)

1. Dependências só para dentro: `transport → app → domain`; `store` implementa `app/ports.go`. `domain` não importa pgx, proto, Connect nem nada de infraestrutura.
2. Um módulo nunca importa `domain`, `app` ou `store` de outro módulo. Comunicação só por `<módulo>/api.go` ou por eventos na outbox.
3. Cada caso de uso: `authz.Check(...)` primeiro, depois `db.WithTeamTx(...)` (faz `SET LOCAL app.team_id` e `app.user_id`).
4. A aplicação liga-se ao Postgres como `pitbox_app` (sem BYPASSRLS). Migrações usam outro papel.
5. Toda a tabela de equipa tem `team_id NOT NULL`, RLS, `UNIQUE (team_id, id)` e FKs compostas `(team_id, x_id)`.
6. Contratos primeiro: mudar a API começa no `.proto`; `buf breaking` tem de passar ou a mudança vai para `v2`.
7. A app lê sempre da base local; nenhum ecrã chama a API diretamente. O modo final funciona 100% offline.
8. O módulo de telemetria só lê sensores: nunca está no caminho do sinal de controlo do carro.
9. Código gerado (`backend/gen`, `*/sqlcgen`, `app/src/gen`) nunca é editado à mão.

## Checklist de segurança (aplicar em cada alteração)

Referências: OWASP Top 10:2025, OWASP API Security Top 10 (2023), OWASP ASVS 5.0 nível 2, OWASP MASVS, NIST SP 800-63B-4, NIST SP 800-207/207A, NIST SSDF, SLSA v1.2, RGPD.

1. **Autorização**: quem pode chamar? Há teste com utilizador de outra equipa (tem de receber `NOT_FOUND`) e com papel sem permissão (`PERMISSION_DENIED`)?
2. **Entrada**: validada (protovalidate/Zod), com limites de tamanho, quantidade e tempo?
3. **Saída**: sem campos a mais, sem dados de outras equipas, sem detalhes internos em erros?
4. **Falhas**: erro, timeout ou entrada malformada falham de forma segura (fail closed)? Sem `panic` em produção.
5. **Auditoria**: ações de escrita ficam em `audit.audit_log` com autor e request ID?
6. **Segredos e dados pessoais**: nada em logs, traces, mensagens de erro, código ou commits. Segredos só via gestor de segredos; tokens guardados apenas como hash.
7. **Dependências**: cada dependência nova justificada; `govulncheck`, `pnpm audit`/osv-scanner, `cargo audit` limpos.
8. **Supply chain**: ações do CI fixadas por SHA, permissões mínimas, imagens assinadas.

Pensar no futuro: TLS com troca de chaves híbrida pós-quântica (X25519MLKEM768, por omissão no `crypto/tls` do Go moderno), agilidade criptográfica (algoritmos configuráveis), passkeys, credenciais de curta duração, identidade de workload em vez de chaves estáticas.

## Git e fluxo de trabalho

- Um cartão = um branch `f<fase>-<nn>-<descricao>` (ex.: `f1-07-garage-backend`) = um PR para `main`.
- Commits pequenos em Conventional Commits, em inglês: `feat(garage): add engine mount history`.
- `main` está protegida: só entra por PR com CI verde.
- Cada PR descreve: cartão, o que mudou, como foi testado e a revisão de segurança.

## Portfólio e mestrado

Quando uma tarefa ensinar algo valorizado no mercado (threat modeling, RLS, OIDC, OpenFGA, supply chain, fuzzing, Rust embarcado), diz-mo numa linha e sugere como pode virar artigo, ponto de portfólio ou tema de trabalho do mestrado.
