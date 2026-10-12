# PitBox

Plataforma para equipas de automodelismo 1/8 TT nitro: setups, motores, provas, modo final e telemetria.

- Contexto e decisões: [docs/00-contexto-do-projeto.md](docs/00-contexto-do-projeto.md)
- Especificação: [docs/01-especificacao-funcional.md](docs/01-especificacao-funcional.md)
- Arquitetura: [docs/02-arquitetura-tecnica.md](docs/02-arquitetura-tecnica.md)
- Plano de trabalho: [docs/03-plano-de-trabalho.md](docs/03-plano-de-trabalho.md)

## Pré-requisitos e instalação (WSL2 Ubuntu 24.04)

O código vive em `~/` (nunca em `/mnt/c`). As versões das ferramentas estão fixadas em [`mise.toml`](mise.toml) e instalam-se sem `sudo`, isoladas em `~/.local/share/mise`.

1. Instalar o [mise](https://mise.jdx.dev/getting-started.html) (descarrega o script, lê-o e só depois corre-o):

   ```bash
   curl -fsSL https://mise.run -o mise-install.sh && less mise-install.sh && sh mise-install.sh
   echo 'eval "$($HOME/.local/bin/mise activate bash)"' >> ~/.bashrc && exec bash
   ```

2. Instalar a toolchain e verificar:

   ```bash
   mise trust && mise install
   make tools-check
   ```

   Instala Go, Node + pnpm, Rust (com `clippy` e `rustfmt`), Buf (+ plugins `protoc-gen-go` e `protoc-gen-connect-go`), sqlc, Atlas, golangci-lint, gitleaks, govulncheck, actionlint e zizmor.

3. **Docker** não vem do mise. Instala o Docker Desktop no Windows e ativa *Settings → Resources → WSL integration* para a tua distro Ubuntu.

4. **Firmware (só a partir da Fase 3)**: toolchain ESP com [`espup`](https://github.com/esp-rs/espup) (`cargo install espup && espup install`; cria `~/export-esp.sh`) e `usbipd-win` no Windows para ligar o ESP32 ao WSL. Fica de fora do `mise.toml` porque é global ao utilizador.

## CI (GitHub Actions)

O CI corre os mesmos `make` que corres localmente, com as ferramentas de `mise.toml` / `mise.lock`. Para reproduzir tudo antes de um PR: `make ci`.

| Job | O que verifica |
| --- | --- |
| `contracts` | `buf lint`, `buf format`, `buf breaking` (só em PR), código gerado atualizado (`make gen-check`), `tsc --noEmit` |
| `backend` | `golangci-lint`, `go test -race`, `govulncheck` |
| `secrets` | `gitleaks` sobre o histórico completo |
| `workflows` | `actionlint` e `zizmor` aos próprios workflows |
| `ci-ok` | Agregador: **o único check obrigatório** no ruleset (falha se qualquer job falhar ou for cancelado) |

`vulncheck.yml` corre o `govulncheck` todas as segundas-feiras: vulnerabilidades novas aparecem sem mudar código.

Decisões:

- **Sem filtros `paths:`**: um check obrigatório que não corre por causa de um filtro fica "pendente" e bloqueia o PR. Quando entrarem a app e o firmware, usa-se um job `changes` (`git diff --name-only` contra a base) e `if:` ao nível de cada job: um job saltado conta como `skipped`, que um check obrigatório aceita. O `ci-ok` não muda; só se acrescentam jobs a `needs`.
- **Só 3 ações de terceiros**, fixadas por SHA (`actions/checkout`, `jdx/mise-action`, `actions/cache`); `permissions: {}` no workflow e `contents: read` por job; sem `pull_request_target`; sem `${{ }}` dentro de `run:`.
- **`mise.lock`** fixa checksums das ferramentas e o CI instala com `--locked` (`install_args`). Cobre só `linux-x64` (WSL e runner); depois de mudar o `mise.toml`: `mise lock -p linux-x64`. Sem checksum no lockfile, de propósito: `rust` (o rustup verifica a sua própria cadeia) e `protoc-gen-connect-go` e `govulncheck` (compilados pelo Go a partir do código, protegidos pelo checksum database do Go). O `sqlc` usa o backend `github:` porque o registo `aqua` dele não dá checksum.
- **Segredos:** o gitleaks no CI *deteta*, não previne. Se falhar, o segredo já está no GitHub: rodá-lo. Localmente: `make hooks` (`git config core.hooksPath .githooks`) ativa o `pre-push`, que corre o gitleaks e **recusa o push** se não o conseguir correr. Sem dependências novas: é um script de `.githooks/`.

### Configuração no GitHub (manual)

Plano **GitHub Pro** (os rulesets funcionam em repositórios privados). *Settings → Actions → General* já está feito (permissões só de leitura, SHA completo obrigatório nas ações). Ordem a seguir:

1. **PR verde:** abrir o PR do CI e esperar que os 5 jobs corram; o check `ci-ok` só aparece no ruleset depois de existir.
2. **Merge** desse PR.
3. **Ruleset da `main`** (*Settings → Rules → Rulesets*): exigir PR, exigir o check **`ci-ok`**, bloquear force-push e apagar o branch.
4. **Push direto recusado:** tentar `git push origin main` e confirmar que o GitHub o recusa.
5. **PR de teste verde:** uma alteração trivial; o merge só fica disponível com `ci-ok` verde.
6. **PR com `ghp_` vermelho:** um `ghp_` falso aleatório (não usar a chave de exemplo da AWS: está na allowlist do gitleaks); `ci-ok` fica vermelho e o merge bloqueado; apagar o branch no fim.
7. **Renovate:** instalar a app só neste repositório (não em "All repositories"), sem `automerge`. O `dependencyDashboard` mostra as atualizações retidas pelo `minimumReleaseAge` e porquê (é onde se vê se falta o timestamp da versão).

## Estrutura

| Pasta | Conteúdo |
| --- | --- |
| `proto/` | Contratos Protobuf (fonte de verdade da API) |
| `backend/` | API, worker e ingest em Go |
| `app/` | App React + Capacitor (web, Android, iOS) |
| `firmware/` | Módulo de telemetria e recetor em Rust (ESP32-S3) |
| `analysis/` | Análise de dados em Python |
| `deploy/` | Docker Compose, OpenTofu |
| `docs/` | Documentação, ADRs, threat model, runbooks |
