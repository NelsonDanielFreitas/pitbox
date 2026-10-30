# cmd/api

Servidor Connect/gRPC + BFF web (`pitbox-api`).

Só contém `main.go`: trata dos sinais (SIGINT/SIGTERM) e chama `internal/bootstrap.RunAPI`.

## Correr localmente

```bash
make up            # Jaeger (e o resto) em deploy/compose
make run-api       # 127.0.0.1:8081, traces para o Jaeger (http://127.0.0.1:16686)
curl -i 127.0.0.1:8081/healthz
make smoke-api     # prova ponta a ponta: /healthz, trace no Jaeger e paragem limpa
```

## Configuração (variáveis de ambiente)

| Variável | Omissão | Notas |
| --- | --- | --- |
| `PITBOX_ENV` | `local` | `local`, `preview`, `staging` ou `production`; outro valor é erro |
| `PITBOX_HTTP_ADDR` | `127.0.0.1:8081` | Em contentor/Cloud Run usar `:8080` (definido no deploy, F5) |
| `PITBOX_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `PITBOX_SHUTDOWN_DRAIN` | `0s` | Serve "não pronto" antes de fechar (alguns segundos em Kubernetes) |
| `PITBOX_SHUTDOWN_TIMEOUT` | `6s` | `drain + timeout + 2 s` de flush de telemetria ≤ 10 s (prazo do Cloud Run) |
| `PITBOX_RPC_MAX_TIMEOUT` | `30s` | Deadline por omissão e teto dos RPCs unários (1 s a 30 s) |
| `PITBOX_RPC_STREAM_MAX_DURATION` | `55m` | Teto dos streams; abaixo do timeout de pedido da plataforma (Cloud Run: até 60 min) |
| `PITBOX_CLIENT_IP_MODE` | `peer` | `peer` ou `header:<Nome>`; o modo `header` exige `PITBOX_ORIGIN_CLOUDFLARE_ONLY=true` e um ambiente que não seja `local` |
| `PITBOX_ORIGIN_CLOUDFLARE_ONLY` | `false` | Declara que a origem só aceita tráfego da Cloudflare (F5-05) |
| `PITBOX_PUBLIC_URL` | `http://localhost:8081` | Origem que o browser vê (sem caminho). O redirect do OIDC é `<URL>/auth/callback`. Fora de `local` tem de ser `https` |
| `PITBOX_OIDC_ISSUER` | `http://localhost:8080` | Issuer do Zitadel, comparado exatamente com o `iss` do ID token. Fora de `local`, `https` |
| `PITBOX_OIDC_CLIENT_ID`, `PITBOX_OIDC_CLIENT_SECRET` | (vazio) | Escritos no `.env` por `make zitadel-provision`. **Segredo** |
| `PITBOX_OIDC_ALLOWED_ALGS` | `ES256` | Algoritmos aceites no ID token, separados por vírgulas (`ES256`, `ES384`, `ES512`, `RS256`, `EdDSA`); `none` e HS* nunca |
| `PITBOX_VALKEY_ADDR` | `127.0.0.1:6379` | Armazém das sessões |
| `PITBOX_VALKEY_USERNAME` | `pitbox_api` | Utilizador ACL limitado a `pitbox:sess:*`, `pitbox:lock:*` e `pitbox:rl:*`; nunca o `default` |
| `PITBOX_VALKEY_PASSWORD` | (vazio) | **Segredo** |
| `PITBOX_VALKEY_TLS` | `false` | Obrigatório fora de `local` |
| `PITBOX_DATABASE_URL` | (vazio) | `postgres://pitbox_api:<palavra-passe>@host:5432/pitbox`; o papel da aplicação (nunca o dono nem o migrador). Fora de `local`, só `sslmode=verify-full` (`require` cifra mas aceita qualquer certificado). **Segredo** |
| `PITBOX_AUTH_KEYS` | (vazio) | Chaves mestras para cifrar cookies e refresh tokens: `kid:base64[,kid:base64]`, pelo menos 32 bytes cada (`openssl rand -base64 32`). A primeira cifra; as outras só decifram (rotação). **Segredo** |
| `PITBOX_SYNC_PASS_KEYS` | (vazio) | Chaves Ed25519 que assinam os passes offline (sync, ADR 0025): `kid:base64[,kid:base64]`, exatamente 32 bytes cada (`openssl rand -base64 32`). A primeira assina; todas verificam (rotação). Obrigatória fora de `local`; em `local` sem ela o `SyncService` não é registado. **Segredo** |
| `PITBOX_SESSION_IDLE_TIMEOUT` | `168h` | Inatividade máxima de uma sessão web (mínimo 1 min) |
| `PITBOX_SESSION_ABSOLUTE_TIMEOUT` | `720h` | Duração máxima; não pode passar o limite do refresh token no Zitadel (720 h) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | (vazio) | Sem endpoint, os traces ficam desligados. `http://` só com `PITBOX_ENV=local` |
| `OTEL_SERVICE_NAME`, `OTEL_TRACES_SAMPLER`, ... | | Variáveis padrão do OpenTelemetry, lidas pelo SDK |

Fora de `local` a autenticação é obrigatória (`PITBOX_OIDC_CLIENT_ID`, `PITBOX_OIDC_CLIENT_SECRET`, `PITBOX_AUTH_KEYS`, `PITBOX_VALKEY_PASSWORD` e `PITBOX_DATABASE_URL`); em `local` pode faltar toda, mas não só parte. Sem ela o serviço recusa tudo (nunca serve sem autenticar). A configuração inválida faz o processo sair com erro no arranque (fail closed); os erros sobre segredos nunca incluem o valor. No log só aparecem os campos de uma lista permitida (`config.Config.LogValue`).

## Endpoints

- `GET /healthz`: liveness (200 `ok`), sem detalhes internos.
- `GET /readyz`: readiness; passa a 503 quando a paragem começa.

Nenhum dos dois cria traces.

Login (BFF, ADR 0013; só funcionam com a autenticação configurada, senão respondem 503):

- `GET /auth/login?return_to=/caminho`: inicia o login (Authorization Code + PKCE) e redireciona para o Zitadel.
- `GET /auth/callback`: conclui o login, cria a sessão e responde 200 com um *meta refresh* para o `return_to`.
- `POST /auth/logout`: exige `Origin`; revoga o refresh token, apaga a sessão e redireciona (303) para o `end_session` do Zitadel.
- `GET /auth/session`: `{authenticated, user_id, amr, auth_time}` (só identificadores) ou 401; 503 se o armazém falhar.
- `GET /auth/signed-out`: página de destino depois do logout.

O servidor Connect (F1-02) monta os serviços com a cadeia de `internal/platform/interceptors`; ainda não há nenhum registado (os módulos trazem o seu a partir do F1-06). Com `PITBOX_ENV=local` monta-se também a reflection gRPC (`grpcurl`/`grpcui`), nunca noutros ambientes.
