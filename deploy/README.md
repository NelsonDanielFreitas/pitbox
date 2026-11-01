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
| Mailpit (F1-03e) | UI/API `http://127.0.0.1:8025`, SMTP interno `mailpit:1025` (auth separada para SMTP e UI) |
| Jaeger | UI `16686`, OTLP gRPC `4317`, OTLP HTTP `4318`, health `13133/status` |
| SeaweedFS (S3) | `8333` (ver [ADR 0009](../docs/adr/0009-seaweedfs-s3-local.md)) |

As credenciais estão no `.env` (`ZITADEL_ADMIN_USERNAME` / `ZITADEL_ADMIN_PASSWORD`, `S3_ACCESS_KEY` / `S3_SECRET_KEY`, …). O Mailpit tem credenciais separadas para SMTP (`MAILPIT_SMTP_*`, usadas pelo Zitadel) e para a UI/API (`MAILPIT_UI_*`); a UI está autenticada e só escuta em `127.0.0.1`.

**Login no Zitadel:** em `http://localhost:8080` o campo de utilizador pede o *login name* completo, `<ZITADEL_ADMIN_USERNAME>@zitadel.localhost` (por omissão `zitadel-admin@zitadel.localhost`), e depois a opção **palavra-passe** com `ZITADEL_ADMIN_PASSWORD`. O utilizador já existe: não é preciso criar conta nem registar passkey. Só o nome sem `@zitadel.localhost` dá "utilizador não existe".

### Login local (F1-03)

```bash
make up                  # a 1.ª vez com volumes novos (make reset): o Zitadel cria o utilizador de provisionamento
make db-migrate          # esquema da aplicação
make zitadel-provision   # projeto e app OIDC, chave ES256, tempos dos tokens; escreve PITBOX_OIDC_* no .env
make run-api             # pitbox-api em http://localhost:8081 com o login ligado
make smoke-auth          # prova login, registo/verificação e recuperação no Mailpit (cria e apaga utilizadores de teste)
```

No browser: `http://localhost:8081/auth/login` (Chrome ou Firefox: o cookie `__Host-` exige `Secure`, que o Safari não aceita em `http://localhost`; o HTTPS local fica para o F1-09). Usa sempre `localhost`, não `127.0.0.1`: o cookie de pré-login é por host e o callback do Zitadel aponta para `localhost`; a app de desenvolvimento redirecciona `127.0.0.1:5173` para `localhost:5173`. O utilizador cria-se no Zitadel (`http://localhost:8080`); o `/auth/session` mostra quem és. O Mailpit (F1-03e) captura os emails de verificação e recuperação; abre `http://127.0.0.1:8025` com as credenciais `MAILPIT_UI_*` do `.env`. O login v2 corre com `EMAIL_VERIFICATION=true`: o registo pede o código enviado pelo Zitadel antes de concluir o login. A página `/confirm-email` da app fica como defesa em profundidade para um login que chegue ao BFF ainda por verificar.

Sem `make zitadel-provision`, o `run-api` arranca sem autenticação: recusa todos os RPC e `/auth/*` responde 503. O PAT do utilizador de provisionamento (IAM_OWNER, 30 dias) vive no volume `zitadel-provisioner`, que só o `zitadel-api` monta; os scripts leem-no do contentor para memória e nunca o imprimem.

### Telemóvel pela tailnet (F1-14c, [ADR 0029](../docs/adr/0029-acesso-do-telemovel-pela-tailnet.md))

Para abrir a app no telemóvel por HTTPS, sem pagar nada e sem expor nada à internet. O PC e o telemóvel entram na mesma tailnet (Tailscale, plano Personal) e o `tailscale serve` termina o TLS com um certificado válido para `<máquina>.<tailnet>.ts.net`. É um *reverse proxy* com terminação de TLS: o `tailscale serve` recebe a ligação HTTPS do telemóvel, desencripta-a e entrega o pedido em HTTP simples a um serviço que só escuta em `127.0.0.1`.

| Endereço (só na tailnet) | Destino local |
| --- | --- |
| `https://<host>` (443) | `vite preview` em `127.0.0.1:4173` (serve a app e faz o proxy de `/auth` e `/pitbox.` para o `pitbox-api`) |
| `https://<host>:8443` | `zitadel-proxy` (Traefik) em `127.0.0.1:8080` |

**Uma vez, à mão (a conta Tailscale é tua; nada disto vai para o repositório nem para o `.env`):**

1. Cria a conta com o fornecedor de identidade com 2FA; só os teus dispositivos na tailnet.
2. No painel de administração: ativa **MagicDNS** e **HTTPS Certificates**. Escolhe um nome de máquina neutro.
3. Instala o Tailscale no PC (WSL) e no telemóvel e entra nos dois. No telemóvel só pode haver uma VPN ativa (no Android).
4. No PC: `sudo tailscale set --operator=$USER` (para o `tailscale serve` correr sem `sudo`).
5. Em `deploy/compose/.env`: `PITBOX_TAILNET_HOST=<máquina>.<tailnet>.ts.net` (só o nome, em minúsculas; sem `https://`, porta nem caminho: um valor inválido pára o `make` e o `vite`).

**Cada sessão:**

```bash
make up                 # com PITBOX_TAILNET_HOST o Zitadel passa a usar o domínio ts.net
make zitadel-provision  # regista os redirect URIs do ts.net SEM tirar os de localhost; corrige o URI do login v2
make run-api            # issuer https://<host>:8443; liga o desvio local ao Zitadel (ver abaixo)
make app-preview        # build de produção + vite preview em 127.0.0.1:4173
make serve-phone        # tailscale serve (nunca funnel): 443 e 8443; mostra o estado
# no telemóvel, com a app Tailscale ligada: https://<host>
make serve-phone-off    # corta o acesso (tailscale serve reset) e mostra o estado vazio
```

Sem `PITBOX_TAILNET_HOST` tudo funciona como antes (`localhost`): CI, E2E e `make smoke-*` não mudam. Trocar de perfil num volume existente **não apaga contas**: o `make up` recria só o `zitadel-api` e o `zitadel-login` com o domínio novo e o `make zitadel-provision` acerta o resto (testado com um dump real: 7 contas mantidas). `make reset` cria uma instância nova e perde as contas; o plano B é restaurar dos dumps (ver abaixo).

**Regras e cuidados**

- **Nunca `tailscale funnel`.** O Funnel torna o serviço público. O alvo `make serve-phone` só usa `tailscale serve`, e confirma que `PITBOX_TAILNET_HOST` é o nome desta máquina na tailnet. Para ver o que está exposto: `tailscale serve status` (deve estar vazio depois de `make serve-phone-off`).
- **Nada mais é exposto.** Confirma com `ss -tlnH`: só `443` e `8443` em `tailscale0`; Postgres, Valkey, OpenFGA, Jaeger, Mailpit, SeaweedFS, o `pitbox-api` (`8081`) e o `vite preview` (`4173`) ficam em `127.0.0.1`.
- **Privacidade:** o HTTPS do Tailscale publica o nome da máquina e o da tailnet nos registos públicos de Certificate Transparency. Usa um nome que não identifique pessoas nem o projeto.
- **Desvio local ao Zitadel:** o PC não alcança o próprio endereço da tailnet (a ligação dá timeout). Com o perfil telemóvel o `make run-api` define `PITBOX_OIDC_LOCAL_DIAL=127.0.0.1:<porta>`: só o host e a porta exatos do issuer são desviados, por HTTP, para esse endereço fixo, mantendo o `Host`. O `pitbox-api` recusa a variável fora de `PITBOX_ENV=local`, com um IP que não seja `127.0.0.1` ou com um issuer que não seja `*.ts.net`, e deixa um aviso no arranque.
- **Terminar sessão no Zitadel não corta a sessão do BFF** (back-channel logout não funciona em local; #154, TM-AUTH-20). Num telemóvel perdido, revoga o dispositivo.
- **Disponibilidade:** o servidor é o PC. Se o WSL parar por inatividade, o `tailscaled` pára e o acesso cai (a app continua offline, ADR 0028).
- **Rate limiting:** atrás do `tailscale serve`, todos os pedidos chegam de `127.0.0.1`; o limite por IP do login é global (aceitável com um só utilizador).
- **Cookies não isolam portas:** o cookie `__Host-` da app também segue para `:8443`. O BFF recusa pedidos `same-site` de outra origem (`Sec-Fetch-Site`).

**Passkeys: mudar o domínio do Zitadel invalida as passkeys existentes.** O Zitadel usa como RP ID da passkey o domínio com que o pedido chega (`RPID` = `RequestedDomain()` e origem = `Origin()`, em `internal/webauthn/webauthn.go` do Zitadel), e o browser só usa uma passkey no RP ID em que foi criada. Uma passkey criada em `localhost` deixa de servir em `<host>.ts.net` (o erro típico é «não foi possível obter»/*NotAllowedError*). Isto vem do domínio, não de um defeito: voltar ao perfil localhost faz as antigas voltarem a funcionar, porque o RP ID é só o nome (sem porta). Para migrar: entra por outro método (palavra-passe), regista uma passkey nova no domínio ts.net (e MFA se for dono) e apaga a antiga. Se a conta só tiver passkey: (a) abre a consola do Zitadel `https://<host>:8443/ui/console` a partir de outro dispositivo da tailnet (o PC não alcança o próprio endereço), entra com o administrador (`ZITADEL_ADMIN_USERNAME`, por palavra-passe), abre o utilizador e, em autenticação, apaga a passkey e envia um email de recuperação da palavra-passe (o Mailpit local mostra-o); ou (b) remove `PITBOX_TAILNET_HOST` do `.env`, `make up` e `make zitadel-provision`, e entra em `localhost` com a passkey antiga. As passkeys de produção (F5) devem usar um domínio estável para nunca passar por isto.

### Backup e restauro (F1-14d)

```bash
make db-backup                       # pitbox + zitadel -> ~/pitbox-backups (pasta 700, ficheiros 600, fora do volume Docker)
make db-backup DB=zitadel            # só uma base
make db-restore                      # VERIFICA o mais recente das duas bases num contentor descartável (não toca em nada)
make db-restore FILE=~/pitbox-backups/pitbox-AAAAMMDD-HHMMSS.dump DB=pitbox
make db-restore-live DB=pitbox CONFIRM=yes   # RESTAURA no compose: APAGA a base e recria-a (pára o Zitadel se for a base zitadel)
make db-backup-test                  # prova automática em contentores descartáveis (corre no CI)
```

- **O que entra:** a base `pitbox` (dados, RLS, hypertable de telemetria e o esquema `fga` do OpenFGA, ou seja, as relações de autorização) e a base `zitadel` (contas, passkeys, sessões). Cada dump é `pg_dump -Fc`, verificado com `pg_restore --list`, com um `.sha256` ao lado. O restauro recusa um dump cuja soma não confere.
- **O que NÃO entra, e tens de guardar à parte:** o `deploy/compose/.env`. O dump do Zitadel só é utilizável com a `ZITADEL_MASTERKEY` (cifra os segredos lá dentro), e a app precisa de `PITBOX_AUTH_KEYS` e `PITBOX_SYNC_PASS_KEYS`. Guarda-o num sítio seguro (gestor de palavras-passe), não ao lado dos dumps. O SeaweedFS (S3) também não entra: ainda não guarda nada da v0.1.
- **Segurança dos dumps:** têm emails e hashes. A pasta é 700 e os ficheiros 600; o script recusa uma pasta dentro do repositório; `*.dump*` está no `.gitignore`. Para guardar cópias noutro sítio, cifra-os: com `PITBOX_BACKUP_AGE_RECIPIENT=<chave pública age>` o backup sai como `.dump.age` (precisa do binário [`age`](https://github.com/FiloSottile/age), `mise exec age@1.3.1 -- ...`), e o restauro usa `PITBOX_BACKUP_AGE_IDENTITY=<ficheiro da chave privada>`. A chave privada nunca fica junto dos dumps. Sem `age`, os dumps ficam em claro, protegidos só pelas permissões e pela cifra de disco do teu PC.
- **Rotação:** `PITBOX_BACKUP_KEEP` (por omissão 14) dumps por base; só apaga ficheiros com o nome `<base>-AAAAMMDD-HHMMSS.dump[.age]`. `PITBOX_BACKUP_DIR` muda a pasta.
- **Restauro (porquê o `pre_restore`):** a base `pitbox` usa TimescaleDB. Um `pg_restore` simples falha nas hypertables. O script cria a extensão, corre `timescaledb_pre_restore()`, faz `pg_restore --exit-on-error` (donos e privilégios incluídos, os papéis vêm do bootstrap do compose) e fecha com `timescaledb_post_restore()`.
- **Recuperar de um `make reset`:** `make reset`, `make up`, depois `make db-restore-live DB=pitbox CONFIRM=yes` e `make db-restore-live DB=zitadel CONFIRM=yes` (com o mesmo `.env` de antes!), `make up`, `make zitadel-provision`.
- **Limites:** é um instantâneo manual do ambiente local, sem PITR nem cópia noutra conta. Isso é o F5-08.

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
- **Só desenvolvimento local.** Tudo em HTTP, sem TLS (o TLS do perfil telemóvel é do `tailscale serve`); nada disto vai para produção.
- **Desvios ao endurecimento (aceites em local):**
  - `zitadel-api` e `zitadel-login` correm como `root` (igual ao compose oficial), por causa do volume partilhado onde o Zitadel escreve o PAT do `login-client`. Alternativa limpa: um serviço de execução única que faz `chown` do volume.
  - O PAT do `login-client` expira em 2099 (token de longa duração, só neste volume local). Em produção (Zitadel Cloud) não existe.
  - As palavras-passe do Valkey (a do `default` e a do utilizador `pitbox_api`) aparecem na linha de comandos do processo e no `docker inspect` (o entrypoint só larga privilégios com `valkey-server` como 1.º argumento).
  - O Zitadel arranca com a chave de assinatura RS256 e o `make zitadel-provision` ativa a ES256: a chave ECDSA não se configura por variáveis de ambiente (o Viper só aplica variáveis a chaves presentes no `defaults.yaml`; o arranque falha com `Errors.WebKey.Config`).
  - O Jaeger não tem health check do Docker (a imagem não tem shell): usar `make health`.
  - O Postgres e o Valkey ficam sem `cap_drop` porque o entrypoint precisa de `CHOWN`/`SETUID`/`SETGID`.
- Verificação de assinaturas cosign das imagens (por exemplo do OpenFGA): fica para o CI (F0-05 ou posterior).
