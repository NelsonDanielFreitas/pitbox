# 0009 — SeaweedFS como armazenamento S3 no ambiente local

- **Estado:** Aceite
- **Data:** 2026-10-11
- **Cartão:** F0-03

## Contexto

A arquitetura previa MinIO como armazenamento S3 local. Já não é viável:

- O repositório `minio/minio` está arquivado ("THIS REPOSITORY IS NO LONGER MAINTAINED"); a última release é `RELEASE.2025-10-15T17-29-55Z`.
- A imagem `minio/minio` já não existe no Docker Hub (404).
- O sucessor oficial, o AIStor Free, tem licença proprietária.
- As imagens de terceiros que ainda circulam não têm manutenção, o que é um risco de componentes vulneráveis ou desatualizados (OWASP Top 10:2025, A03).

Em produção o armazenamento é gerido (arquitetura §11.1: Cloud Storage ou Cloudflare R2). Localmente só precisamos da **API S3**: PUT/GET e URLs pré-assinados.

## Decisão

Usar **SeaweedFS** (Apache-2.0, escrito em Go, imagem oficial `chrislusf/seaweedfs`) no Docker Compose local, com o comando `mini` (master, volume, filer e gateway S3 num só contentor).

A aplicação fala **S3 genérico** com `aws-sdk-go-v2`: endpoint e credenciais vêm da configuração; não se usa nenhuma API específica do SeaweedFS. Assim o mesmo código corre contra o SeaweedFS (local) e contra o armazenamento gerido (produção).

## Alternativas consideradas

| Alternativa | Porque não |
| --- | --- |
| AIStor Free | Licença proprietária |
| Garage | AGPL; precisa de bootstrap de layout. Segunda escolha |
| RustFS | Ainda imaturo |
| Imagens MinIO de terceiros | Sem manutenção nem cadeia de confiança |
| Sem S3 no compose | Só adia a decisão; falta o armazenamento para o F3 |

## Consequências

- Object lock e replicação não são testáveis em local; ficam para o staging.
- Atualizar a arquitetura §11.3 e o plano (tabela de tecnologias da Fase 0 e Fase 3) para SeaweedFS; o `CLAUDE.md` e `.claude/rules/ci-and-infra.md` também.
- A imagem é fixada por tag e digest; o Renovate propõe as atualizações.
- Reavaliar se o Garage ou o RustFS amadurecerem, ou se o SeaweedFS deixar de ter manutenção.
- O número 0009 segue os ADR 0001–0008 já reservados para o F0-07 (arquitetura §16.2).
