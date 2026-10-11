# PitBox

Plataforma para equipas de automodelismo 1/8 TT nitro: setups, motores, provas, modo final e telemetria.

- Contexto e decisões: [docs/00-contexto-do-projeto.md](docs/00-contexto-do-projeto.md)
- Especificação: [docs/01-especificacao-funcional.md](docs/01-especificacao-funcional.md)
- Arquitetura: [docs/02-arquitetura-tecnica.md](docs/02-arquitetura-tecnica.md)
- Plano de trabalho: [docs/03-plano-de-trabalho.md](docs/03-plano-de-trabalho.md)

| Pasta | Conteúdo |
| --- | --- |
| `proto/` | Contratos Protobuf (fonte de verdade da API) |
| `backend/` | API, worker e ingest em Go |
| `app/` | App React + Capacitor (web, Android, iOS) |
| `firmware/` | Módulo de telemetria e recetor em Rust (ESP32-S3) |
| `analysis/` | Análise de dados em Python |
| `deploy/` | Docker Compose, OpenTofu |
| `docs/` | Documentação, ADRs, threat model, runbooks |
