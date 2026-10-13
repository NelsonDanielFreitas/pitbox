# deploy

| Pasta | Conteúdo |
| --- | --- |
| `compose/` | `docker-compose.yml` do ambiente local, init do Postgres e configs do proxy do Zitadel (F0-03) |
| `tofu/modules/` | Módulos OpenTofu reutilizáveis (rede, base de dados, Cloud Run, segredos) |
| `tofu/envs/staging`, `tofu/envs/production` | Um diretório por ambiente, cada um com o seu estado remoto (F5-04) |
| `helm/` | Charts para a fase Kubernetes (fase 2, só quando necessário) |

Regras em `.claude/rules/ci-and-infra.md`.

## Ambiente local (`compose/`)

```bash
make init      # 1.ª vez: gera deploy/compose/.env com segredos aleatórios (fora do Git, modo 600)
make up        # docker compose up -d --wait: espera pelos health checks
make health    # estado dos serviços + sonda ao Jaeger
make s3-smoke  # S3: casos negativos (403) e depois bucket, upload e download por URL pré-assinado
make down      # para (mantém os dados)
make reset     # APAGA os volumes e recria (pede confirmação)
```

| Serviço | Endereço (só `127.0.0.1`; portas configuráveis no `.env`) |
| --- | --- |
| PostgreSQL 16 + TimescaleDB | `5432` (bases `pitbox` e `zitadel`) |
| Valkey 8.0 | `6379` (com palavra-passe) |
| Zitadel | `http://localhost:8080` (usar `localhost`, **não** `127.0.0.1`: a instância só responde ao seu domínio) |
| OpenFGA | HTTP `8090`, gRPC `8091` (chave pré-partilhada; sem playground) |
| Jaeger | UI `16686`, OTLP gRPC `4317`, OTLP HTTP `4318`, health `13133/status` |
| SeaweedFS (S3) | `8333` (ver [ADR 0009](../docs/adr/0009-seaweedfs-s3-local.md)) |

As credenciais estão no `.env` (`ZITADEL_ADMIN_USERNAME` / `ZITADEL_ADMIN_PASSWORD`, `S3_ACCESS_KEY` / `S3_SECRET_KEY`, …).

**Login no Zitadel:** em `http://localhost:8080` o campo de utilizador pede o *login name* completo, `<ZITADEL_ADMIN_USERNAME>@zitadel.localhost` (por omissão `zitadel-admin@zitadel.localhost`), e depois a opção **palavra-passe** com `ZITADEL_ADMIN_PASSWORD`. O utilizador já existe: não é preciso criar conta nem registar passkey. Só o nome sem `@zitadel.localhost` dá "utilizador não existe".

### Login local (F1-03)

```bash
make up                  # a 1.ª vez com volumes novos (make reset): o Zitadel cria o utilizador de provisionamento
make db-migrate          # esquema da aplicação
make zitadel-provision   # projeto e app OIDC, chave ES256, tempos dos tokens; escreve PITBOX_OIDC_* no .env
make run-api             # pitbox-api em http://localhost:8081 com o login ligado
make smoke-auth          # prova o login ponta a ponta sem browser (cria e apaga um utilizador de teste)
```

No browser: `http://localhost:8081/auth/login` (Chrome ou Firefox: o cookie `__Host-` exige `Secure`, que o Safari não aceita em `http://localhost`; o HTTPS local fica para o F1-09). O utilizador cria-se no Zitadel (`http://localhost:8080`); o `/auth/session` mostra quem és.

Sem `make zitadel-provision`, o `run-api` arranca sem autenticação: recusa todos os RPC e `/auth/*` responde 503. O PAT do utilizador de provisionamento (IAM_OWNER, 30 dias) vive no volume `zitadel-provisioner`, que só o `zitadel-api` monta; os scripts leem-no do contentor para memória e nunca o imprimem.

### Papéis do Postgres

| Papel | Tipo | Uso |
| --- | --- | --- |
| `postgres` | superutilizador | **só** o init (`compose/postgres/init`); nenhum serviço o usa |
| `pitbox_owner`, `pitbox_app`, `pitbox_readonly` | grupo, `NOLOGIN`, `NOBYPASSRLS` | iguais aos do `schema.sql` |
| `pitbox_migrator` | login, `IN ROLE pitbox_owner` | migrações (Atlas, F0-06) |
| `pitbox_api` | login, `IN ROLE pitbox_app` | API, worker, ingest |
| `openfga` | login | esquema `fga` da base `pitbox` |
| `zitadel` | login | dono da base `zitadel` |

### Cuidados

- **Mudar um segredo no `.env` depois do primeiro arranque não tem efeito**: os scripts de init só correm com o volume vazio e o Zitadel guarda a masterkey cifrada. Nesse caso, `make reset`.
- **Só desenvolvimento local.** Tudo em HTTP, sem TLS; nada disto vai para produção.
- **Desvios ao endurecimento (aceites em local):**
  - `zitadel-api` e `zitadel-login` correm como `root` (igual ao compose oficial), por causa do volume partilhado onde o Zitadel escreve o PAT do `login-client`. Alternativa limpa: um serviço de execução única que faz `chown` do volume.
  - O PAT do `login-client` expira em 2099 (token de longa duração, só neste volume local). Em produção (Zitadel Cloud) não existe.
  - As palavras-passe do Valkey (a do `default` e a do utilizador `pitbox_api`) aparecem na linha de comandos do processo e no `docker inspect` (o entrypoint só larga privilégios com `valkey-server` como 1.º argumento).
  - O Zitadel arranca com a chave de assinatura RS256 e o `make zitadel-provision` ativa a ES256: a chave ECDSA não se configura por variáveis de ambiente (o Viper só aplica variáveis a chaves presentes no `defaults.yaml`; o arranque falha com `Errors.WebKey.Config`).
  - O Jaeger não tem health check do Docker (a imagem não tem shell): usar `make health`.
  - O Postgres e o Valkey ficam sem `cap_drop` porque o entrypoint precisa de `CHOWN`/`SETUID`/`SETGID`.
- Verificação de assinaturas cosign das imagens (por exemplo do OpenFGA): fica para o CI (F0-05 ou posterior).
