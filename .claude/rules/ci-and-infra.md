---
paths:
  - ".github/**/*"
  - "deploy/**/*"
  - "Makefile"
  - "**/Dockerfile"
---

# Regras de CI/CD e infraestrutura

## GitHub Actions
- Ações de terceiros fixadas por SHA completo (com a versão em comentário); Renovate atualiza-as.
- `permissions:` mínimas por workflow e por job (por omissão `contents: read`).
- Sem segredos de longa duração: acesso à cloud por OIDC (workload identity federation).
- Workflows com filtros de caminho (`paths:`) para só correr o que mudou (backend, app, firmware, proto).
- Nunca usar `pull_request_target` com checkout do código do PR.

## Imagens e entrega
- Imagens Go com `ko` (distroless, sem shell, utilizador não root); outras imagens multi-stage com base distroless/Chainguard.
- SBOM com Syft, scanning com Trivy (falha em vulnerabilidades críticas corrigíveis), assinatura cosign keyless, proveniência SLSA.
- Deploy de produção só a partir do CI, com aprovação manual e rollout gradual.

## Infraestrutura (OpenTofu)
- Tudo como código em `deploy/tofu`; estado remoto cifrado; ambientes separados (staging, production) em projetos/contas diferentes.
- Princípio do menor privilégio em contas de serviço; sem chaves JSON exportadas.
- Recursos na UE. Backups com bloqueio de objetos noutra conta.

## Docker Compose local
- `deploy/compose/docker-compose.yml`: Postgres 16 + TimescaleDB, Valkey, Zitadel, OpenFGA, MinIO, Jaeger, com health checks.
- Portas expostas só em `127.0.0.1`. Credenciais de desenvolvimento em `.env` (no `.gitignore`), com `.env.example` versionado.
