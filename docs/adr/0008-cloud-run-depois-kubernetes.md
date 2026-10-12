# 0008 — Cloud Run na fase 1 de infraestrutura, Kubernetes na fase 2 com gatilhos

- **Estado:** Aceite (Nelson, 2026-10-11)
- **Data:** 2026-10-11 (decisão); registado em 2026-10-12
- **Cartão:** F0-07 (registo); origem: arquitetura §1 e §11

## Contexto

Operar um cluster Kubernetes para uma equipa de 1–2 pessoas é custo sem benefício no início. O código e as imagens têm de ser os mesmos nas duas fases. "Fase 1/2" aqui refere-se à **infraestrutura** (arquitetura §11), não às Fases F0–F5 do plano de trabalho.

## Decisão

- **Fase 1 de infraestrutura:** Google Cloud Run (`pitbox-api` com mínimo de 1 instância), serviços geridos na UE (Postgres+Timescale, Valkey, Zitadel Cloud, OpenFGA em Cloud Run, Cloud Storage ou R2), Cloudflare à frente, OpenTofu.
- **Fase 2:** Kubernetes gerido (GKE Autopilot ou equivalente), Argo CD, Istio ambient, Kyverno.
- **Gatilhos para passar à fase 2:** mais de 3 processos com escalas diferentes, necessidade de rede privada entre serviços, ou custo do Cloud Run acima de um cluster pequeno.

A tabela de decisões de topo da arquitetura (§1) dizia "Cloud Run ou Fly.io"; passa a dizer Cloud Run.

## Alternativas consideradas

| Alternativa | Porque não |
| --- | --- |
| Fly.io | Fornecedor menos coerente com a restante stack (UE, Secret Manager/KMS, identidade de workload) |
| VM única com Docker Compose | Sem rollout gradual nem rollback automático; é o ambiente local, não produção |
| Kubernetes desde o início | Custo operacional sem necessidade |

## Consequências

- Os fornecedores da §11.1 são exemplos coerentes entre si; o equivalente em AWS ou Azure é válido.
- A alternativa "Cloud SQL + tabelas particionadas" da §11.1 deixa de valer sem novo ADR (ADR 0011: TimescaleDB obrigatória).
- O armazenamento S3 **local** é SeaweedFS, não MinIO ([ADR 0009](0009-seaweedfs-s3-local.md)).
- Em produção, o acesso humano só por break-glass auditado e os deploys só pelo CI com aprovação manual.
