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

   Instala Go, Node + pnpm, Rust (com `clippy` e `rustfmt`), Buf, sqlc, Atlas e golangci-lint.

3. **Docker** não vem do mise. Instala o Docker Desktop no Windows e ativa *Settings → Resources → WSL integration* para a tua distro Ubuntu.

4. **Firmware (só a partir da Fase 3)**: toolchain ESP com [`espup`](https://github.com/esp-rs/espup) (`cargo install espup && espup install`; cria `~/export-esp.sh`) e `usbipd-win` no Windows para ligar o ESP32 ao WSL. Fica de fora do `mise.toml` porque é global ao utilizador.

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
