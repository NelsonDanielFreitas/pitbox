# 0029 — Acesso do telemóvel ao ambiente local pela tailnet (Tailscale Serve)

- **Estado:** Aceite (Nelson, 2026-10-31)
- **Data:** 2026-10-31
- **Cartão:** F1-14c (novo; pré-condição do F1-15)
- **Relacionado:** [ADR 0004](0004-zitadel-bff.md), [ADR 0013](0013-sessoes-bff-e-tokens.md), [ADR 0017](0017-app-web-bff-mesma-origem.md), [ADR 0019](0019-defesa-palavra-passe-login.md), [ADR 0028](0028-app-instalavel-e-arranque-sem-rede.md); arquitetura §5, §12 e §15; F5-04/F5-05 (produção).

## Contexto

O gate F1-15 usa a v0.1 no telemóvel. Hoje tudo corre no PC (WSL2) e só responde em `127.0.0.1`/`localhost`. Usar o IP da rede de casa (`http://192.168.x.x`) não serve: o Chrome só ativa o service worker (ADR 0028), o cookie `__Host-pitbox_session` (`Secure`) e a instalação da PWA em **HTTPS** ou em `localhost`. A produção (Cloud Run, Cloudflare) só chega no F5 e o objetivo agora é **custo zero**.

Restrições do ambiente atual:

- O Zitadel fixa o issuer e os URLs do login v2 a partir de `ZITADEL_EXTERNALDOMAIN`/`EXTERNALPORT`/`EXTERNALSECURE` e das variáveis `FIRSTINSTANCE`/`DEFAULTINSTANCE` (`deploy/compose/docker-compose.yml`), que só se aplicam no primeiro arranque do volume (ver o spike abaixo para o que isso implica).
- O `zitadel-login` envia `CUSTOM_REQUEST_HEADERS: Host:localhost:8080,X-Forwarded-Proto:http`.
- `scripts/zitadel-provision.py` só aceita falar com `localhost` (o PAT nunca sai da máquina) e regista os redirect URIs a partir de `PITBOX_PUBLIC_URL`.
- `scripts/run-api.sh` fixa `PITBOX_OIDC_ISSUER=http://localhost:<ZITADEL_PORT>`.
- A app e o BFF partilham a origem (ADR 0017); o `vite preview` faz o proxy de `/auth` e `/pitbox.`.

## Decisão

1. **Tailscale Serve, nunca Funnel.** O PC e o telemóvel entram na mesma tailnet (plano Personal, gratuito, uso não comercial). O `tailscale serve` termina o TLS com um certificado válido para `<máquina>.<tailnet>.ts.net` e encaminha para portas locais:
   - `https://<máquina>.<tailnet>.ts.net` (443) → `vite preview` (`127.0.0.1:4173`), que serve a app e faz o proxy de `/auth` e `/pitbox.` para o `pitbox-api` (`127.0.0.1:8081`). Uma só origem, como no ADR 0017.
   - `https://<máquina>.<tailnet>.ts.net:<porta-zitadel>` → `zitadel-proxy` (`127.0.0.1:8080`). A porta concreta confirma-se no spike.
2. **Nada mais fica exposto.** Postgres, Valkey, OpenFGA, Jaeger, Mailpit, SeaweedFS e a porta direta do `pitbox-api` continuam só em `127.0.0.1`. As portas do compose não mudam de interface.
3. **Perfil «telemóvel» opt-in.** O `localhost` continua a ser o perfil por omissão (CI, E2E, `make smoke-*`). O perfil telemóvel ativa-se por uma variável no `.env` (por exemplo `PITBOX_TAILNET_HOST`). Com ele, o Zitadel usa o domínio externo `ts.net` (volume novo: nasce assim; volume existente: `make up` e `make zitadel-provision` acertam-no) (`EXTERNALSECURE=true`, URLs do login v2 em HTTPS), `PITBOX_PUBLIC_URL` e `PITBOX_OIDC_ISSUER` passam a HTTPS e o `make zitadel-provision` regista os redirect URIs do `ts.net`. Um alvo `make serve-phone` / `make serve-phone-off` liga e desliga o `tailscale serve`.
4. **A conta Tailscale é do Nelson e configura-se à mão:** 2FA no fornecedor de identidade, só os dispositivos dele na tailnet, MagicDNS e HTTPS Certificates ativos e um nome de máquina neutro. Nenhuma auth key nem credencial do Tailscale entra no repositório, no `.env` ou nos logs.

## Alternativas

| Alternativa | Motivo para rejeitar agora |
| --- | --- |
| IP da rede de casa em HTTP | Sem HTTPS não há service worker, cookie `Secure` nem instalação; só funciona em casa. |
| HTTPS local com `mkcert` | É preciso instalar uma CA própria no telemóvel (frágil no Android, risco se a chave da CA vazar) e só funciona na rede de casa. |
| Cloudflare Tunnel + Access | Gratuito (Zero Trust Free, até 50 utilizadores), mas expõe o PC à internet atrás do Access e precisa de um domínio (~10 €/ano) para ter um nome estável; os túneis rápidos mudam de URL e partem os redirect URIs. Fica para quando outra pessoa precisar de acesso (F5-05). |
| Staging na cloud (Cloud Run + Postgres gerido) | Tem custo mensal (~30 €/mês só no Postgres, arquitetura §15.2) e antecipa o F5-04/F5-06. |
| TLS no Traefik com `tailscale cert` | Obriga a gerir o certificado (renovação a cada 90 dias, chave privada em disco, montagens no Traefik); o `tailscale serve` faz tudo isso sozinho e a chave não sai do `tailscaled`. |
| Tailscale Funnel | Torna o serviço público na internet; contraria o objetivo de não expor o ambiente local. |

## Consequências

- **Privacidade:** o HTTPS do Tailscale exige MagicDNS e publica o nome da máquina e o da tailnet nos registos públicos de Certificate Transparency. O nome da máquina não pode identificar o Nelson nem o projeto.
- **Disponibilidade:** o servidor é o PC. Desligado, a app continua offline (ADR 0028) e sincroniza depois, dentro das 72 h do passe. No Android só há uma VPN ativa de cada vez.
- **Zero Trust:** a tailnet não é uma zona de confiança. O BFF, o OpenFGA e o RLS continuam a decidir em cada pedido; a tailnet só reduz quem consegue chegar ao serviço.
- **Rate limiting:** atrás do `tailscale serve`, os pedidos chegam ao Traefik e ao `pitbox-api` vindos de `127.0.0.1`. O limite por IP dos POST de login (ADR 0019) passa a ser global, o que é aceitável com um só utilizador. Fica registado no threat model.
- **Mudar de perfil não apaga dados** num volume existente (ver o spike, ponto 5); `make reset` apaga e a instância nova perde as contas. O backup (cartão próprio) continua obrigatório antes do gate; o restauro do dump `pitbox` (TimescaleDB, com `timescaledb_pre_restore()`/`post_restore()`) foi testado num contentor isolado.
- **Respostas do spike (2026-10-31, projeto Compose isolado):**
  1. `tailscale serve --https=8443` é aceite; os ouvintes ficam só na interface `tailscale0` (443 e 8443). Falar com o próprio endereço da tailnet a partir do PC dá *timeout*.
  2. O Zitadel escolhe a instância pelo `Host` e o issuer inclui a porta: o pedido tem de levar `Host: <host>:8443`. Com `localhost` numa instância nova responde 404 «Instance not found». O `zitadel-provision.py` liga a `localhost` mas envia esse `Host`; o PAT continua a não sair da máquina.
  3. O Zitadel funciona com o `Host` externo por HTTP simples, sem `X-Forwarded-Proto`: a configuração do Traefik não muda. O `zitadel-login` recebe `X-Forwarded-Proto: https` e o `Host` externo por `CUSTOM_REQUEST_HEADERS`.
  4. `tailscaled` corre dentro do WSL; o risco é o WSL parar por inatividade (o acesso cai; a app continua offline).
  5. **Contas existentes:** um dump real do Zitadel (7 utilizadores) restaurado numa instância configurada com o domínio ts.net manteve os utilizadores e respondeu com o issuer `https://<host>:8443`. Trocar de perfil não exige `make reset`: o `make up` recria o `zitadel-api` e o `zitadel-login`, e o `make zitadel-provision` corrige o redirect URI e o `baseUri` da feature `loginV2` (que só nasce no primeiro arranque). Confirmado também no volume real.
- **Back-channel logout (#154, TM-AUTH-20):** o Zitadel bloqueia URLs `ts.net` em `backChannelLogoutUri` (`Errors.Project.App.Blocked.BackchannelLogoutURL`), mesmo com `developmentMode`. Mantém-se `http://localhost:8081/auth/backchannel-logout` com `developmentMode=true` nos dois perfis. Em local o back-channel nunca chegou ao `pitbox-api` (o `localhost` do contentor é o próprio contentor); a sessão do BFF corta no refresh seguinte (access token de 10 min). Aceite para a v0.1. Num telemóvel perdido a prova principal é revogar o dispositivo (impede o `Push`).
- **Desvio local ao Zitadel (`PITBOX_OIDC_LOCAL_DIAL`):** o `pitbox-api` corre no WSL e não alcança `https://<host>:8443`. O cliente HTTP do OIDC desvia, por HTTP, **só** o host e a porta exatos do issuer para `127.0.0.1:<porta do Traefik>`, mantendo o `Host`; os restantes destinos usam o cliente normal. Condições: só com `PITBOX_ENV=local` e variável explícita (noutro ambiente o arranque falha); destino fixo `127.0.0.1:<porta>`; só com issuer `https` em `*.ts.net`; aviso no arranque, sem URLs com tokens; testes em tabela.
- **Passkeys:** o Zitadel usa o domínio do pedido como RP ID (`webauthn.go`: `RPID: domainCtx.RequestedDomain()`), por isso **mudar o domínio do Zitadel invalida as passkeys existentes**: entrar por outro método, registar uma passkey nova no ts.net e apagar a antiga. Se a conta só tiver passkey, recupera-se pela consola do Zitadel a partir de outro dispositivo da tailnet (apagar a passkey e enviar a recuperação da palavra-passe) ou voltando ao perfil localhost (`deploy/README.md`). Em produção usa-se um domínio estável (F5).
- **Cookies e portas:** os cookies não isolam portas, por isso o cookie `__Host-` da app também segue para `:8443`. O BFF recusa pedidos `same-site` de outra origem (`Sec-Fetch-Site`); aceite na v0.1 (TM-LOCAL-09).
- **Ameaças:** registadas em `docs/threat-model/ameacas.md` (TM-LOCAL-04 a 10): dispositivo estranho na tailnet, conta Tailscale comprometida, Funnel por engano, limite por IP global, desvio local, cookie entre portas e Certificate Transparency.
