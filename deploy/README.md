# deploy

| Pasta | Conteúdo |
| --- | --- |
| `compose/` | `docker-compose.yml` do ambiente local + configs de Zitadel, OpenFGA, Jaeger (F0-03) |
| `tofu/modules/` | Módulos OpenTofu reutilizáveis (rede, base de dados, Cloud Run, segredos) |
| `tofu/envs/staging`, `tofu/envs/production` | Um diretório por ambiente, cada um com o seu estado remoto (F5-04) |
| `helm/` | Charts para a fase Kubernetes (fase 2, só quando necessário) |

Regras em `.claude/rules/ci-and-infra.md`.
