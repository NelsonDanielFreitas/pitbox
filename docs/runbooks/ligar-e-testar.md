# Ligar e testar tudo (guia rápido)

Para quando ligas o PC e queres pôr o PitBox a funcionar, testar e continuar a desenvolver. Tudo no terminal do **WSL**, dentro do repositório:

```bash
cd ~/projects/pitbox
```

## 1. Depois de ligar o PC (sempre)

1. **Docker Desktop** (Windows): abre-o e espera até dizer *Running*. Os contentores voltam sozinhos.
2. **Tailscale**: arranca sozinho no WSL. Confirma com `tailscale status` (o PC e o telemóvel têm de aparecer). Se disser parado: `sudo tailscale up`. No telemóvel, a app Tailscale tem de estar ligada.
3. Confirmar que os serviços estão de pé; se faltar algum, levanta-os:

```bash
make health          # tudo "healthy"/"OK"?
make up              # só se algo faltar (não apaga nada)
make db-migrate      # só se mudaste de branch com migrações novas
```

Só na **primeira vez** ou depois de um `make reset`: `make zitadel-provision` e `make openfga-provision`.

## 2. Pôr a aplicação a correr (3 terminais)

| Terminal | Comando | Para quê |
| --- | --- | --- |
| 1 | `make run-api` | Backend (`pitbox-api` em `127.0.0.1:8081`) |
| 2 | `make app-preview` | App web em `127.0.0.1:4173` |
| 3 | `make serve-phone` | Abre a app e o login ao telemóvel, só pela tailnet |

Para trabalhar na app web com recarga automática, usa em vez do terminal 2: `make app-dev` e abre `http://localhost:5173` (**não** `127.0.0.1`).

Se o Zitadel local está no perfil telemóvel, `make run-api` define também `PITBOX_OIDC_LOCAL_LOGOUT_ISSUER` (o back-channel logout, #154) e, havendo `PITBOX_ZITADEL_RECOVERY_PAT` no `.env` (criado por `make zitadel-provision`), liga a recuperação «Perdi a passkey» (#158). Depois de atualizar o repositório corre `make up` (monta `deploy/compose/zitadel/roles.yaml`), `make db-migrate` e `make zitadel-provision`.

Verificar: `curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8081/healthz` deve dar `200`; no telemóvel abre `https://portatil-wsl.tail9f9359.ts.net`.

## 3. Testar

**Backend e base de dados**

```bash
make test            # Go (com -race) e scripts
make smoke-api       # prova ponta a ponta da API
make smoke-auth      # login, registo e verificação (Zitadel + Mailpit)
make db-test         # migrações, RLS e restrições em contentores descartáveis
make authz-test      # modelo de autorização (OpenFGA)
```

**App web**

```bash
make app-lint
make app-typecheck
make app-test        # Vitest
make app-e2e         # Playwright, não precisa da API
make app-e2e-sync    # Playwright contra a API real (Docker)
```

**App Android** (telemóvel com *Depuração sem fios* ligada, na mesma Wi-Fi)

```bash
~/Android/Sdk/platform-tools/adb pair <IP>:<PORTA-DE-EMPARELHAMENTO>   # só uma vez; pede o código
~/Android/Sdk/platform-tools/adb connect <IP>:<PORTA>                  # a porta do ecrã principal; muda quando religas
make android-debug        # APK de debug: abre a app normal
make android-diagnostics  # APK de debug que abre na página «Diagnóstico nativo»
make android-install      # instala o último APK e abre no telemóvel
make android-logs         # resultado do diagnóstico
```

Para o diagnóstico: `make android-diagnostics`, `make android-install`, `make android-logs`. Esperado: 7 linhas `OK` (3 do Keystore, 4 da base). A página só existe nesse APK. O botão *Deixar base de teste (adb)* serve o teste de [android-teste-base-cifrada.md](android-teste-base-cifrada.md). O login no Android existe desde o F2-06b (ADR 0032): Auth Tab no Chrome (versão recente), passkey e Bearer. Precisa de `make run-api` e `make app-preview` a correr (o proxy da tailnet) e de `PITBOX_OIDC_MOBILE_CLIENT_ID`/`PITBOX_CORS_ORIGINS` no `.env` (`make zitadel-provision`). Se alguma coisa falhar, [sessao-movel-reutilizacao.md](sessao-movel-reutilizacao.md).

**Tudo, como o CI (antes de abrir um PR):** `make ci` (precisa do Docker).

## 4. Desenvolver uma feature e testá-la no telemóvel

Há **dois caminhos**, e não são a mesma coisa:

| | Web no browser do telemóvel | APK Android |
| --- | --- | --- |
| Para quê | Testar ecrãs e features com login e dados reais | Testar o que é nativo (chaves, base cifrada, bloqueio) |
| Precisa dos 3 terminais? | **Sim** | Não |
| Login funciona? | Sim | Sim (Auth Tab, F2-06b) |
| Abre em | `https://portatil-wsl.tail9f9359.ts.net` | A app normal (`make android-debug`) ou o diagnóstico (`make android-diagnostics`) |

### Caminho A: feature nova, testada pelo browser do telemóvel

Antes de começar (uma vez por sessão): secção 1 e os 3 terminais da secção 2.

Ciclo de trabalho:

1. Mexes no código e testas no PC primeiro: `make app-dev` (hot reload em `http://localhost:5173`) ou `make test`.
2. Para ver no telemóvel, **reinicia só o que mudaste** (Ctrl+C no terminal e volta a correr):

| Mudaste | Reinicia | E corre antes |
| --- | --- | --- |
| Código da app (React/TS) | terminal 2: `make app-preview` (volta a fazer a build) | nada |
| Código Go do backend | terminal 1: `make run-api` | nada |
| Um `.proto` | terminal 1 e 2 | `make gen` |
| Migração da base de dados | terminal 1 | `make db-migrate` (e `make db-test`) |

3. No telemóvel, **fecha e volta a abrir** a página (a app guarda a versão anterior em cache): aparece o aviso *Atualizar* e tocas nele. Se não aparecer, recarrega a página duas vezes.
4. O terminal 3 (`make serve-phone`) não se reinicia: só encaminha o tráfego.

### Caminho B: algo nativo, testado no APK

Sem terminais; só o telemóvel ligado por `adb` (secção 3, App Android):

```bash
make android-debug      # constrói (inclui a app web atual); para o diagnóstico: make android-diagnostics
make android-install    # instala e abre
make android-logs       # lê o resultado
```

Repete estes três comandos a cada alteração, seja no TypeScript da app ou no plugin Java. Com o login móvel (F2-06b) o APK mostra os teus dados depois de entrares com a passkey.

### Antes de abrir o PR

```bash
make lint && make test && make app-test && make app-e2e
```

Ou, de uma vez e como o CI, `make ci` (precisa do Docker).

## 5. Mexi em… e agora?

| Mexi em | Corre |
| --- | --- |
| `.proto` | `make gen` e `make breaking` |
| Migração de base de dados | `make db-new name=...`, editar também `schema.sql`, `make db-migrate`, `make db-test` |
| Código Go | `make test` e `make lint` |
| App web | `make app-test` e `make app-lint` |
| App Android ou plugin nativo | `make android-debug`, `make android-install`, `make android-logs` |

## 6. Quando terminas

```bash
# Ctrl+C nos 3 terminais
make serve-phone-off     # fecha o acesso do telemóvel
make down                # opcional: para os contentores (os dados ficam)
```

**Nunca** `make reset` sem `make db-backup` antes: apaga os volumes (Postgres, Zitadel).

## 7. Se algo falha

- `make up` falha: o Docker Desktop está mesmo *Running*? Depois `make health`.
- `make run-api` queixa-se de migrações: `make db-migrate`.
- O telemóvel não abre o site: `tailscale status` nos dois lados e `make serve-phone` outra vez (os terminais 1 e 2 têm de estar a correr).
- `adb devices` vazio ou `offline`: repete `adb connect` com a porta atual (muda quando a depuração sem fios é religada). `adb pair` só se o telemóvel deixou de reconhecer o PC.
- `gradlew` não encontra Java ou o SDK: corre dentro do repositório (o `mise` ativa o Java 21) e confirma `app/android/local.properties` com `sdk.dir=/home/nelson/Android/Sdk`.
- Mudaste de máquina ou de nome Tailscale: atualiza `PITBOX_TAILNET_HOST` em `deploy/compose/.env`.
