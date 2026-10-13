# 0013 — Sessões BFF e tratamento de tokens

- **Estado:** Proposto (aguarda confirmação de Nelson)
- **Data:** 2026-10-13
- **Cartão:** F1-03 (a: login e logout; b: utilizador no primeiro login)
- **Relacionado:** [ADR 0004](0004-zitadel-bff.md) (Zitadel e BFF), emendado em conjunto com este.

## Contexto

O ADR 0004 fixou o padrão BFF e deixou por decidir o que o `pitbox-api` guarda, durante quanto tempo e como valida. Verificado a 13/10/2026 contra o Zitadel local (`v4.19.4`), o `go-oidc` v3.21.0 e a documentação do Zitadel:

- O JWKS por omissão só tem chaves **RS256** (ES256 exige configurar as *web keys*).
- O tipo de access token por omissão é **opaco**; os tempos por omissão são 12 h (access e ID token) e 30 dias de inatividade no refresh token.
- `auth_time`, `amr` e `sid` só existem no **ID token**, não no access token.
- O `RemoteKeySet` do `go-oidc` não tem TTL: só volta ao JWKS quando a verificação falha. O `nbf` tem uma tolerância fixa de 5 minutos e o `exp` não tem nenhuma.
- O Zitadel rejeita um refresh token já rodado. Segundo a revisão do código da `v4.19.4`, `CheckRefreshToken` só rejeita o antigo: **não revoga a família**.

## Decisão

### Âmbito

O `pitbox-api` é um cliente OIDC confidencial (Authorization Code + PKCE S256, `client_secret_basic`). No browser só existe um cookie de sessão. O caminho `Authorization: Bearer` **não existe neste cartão**: passa para o F2-06 (telemóvel). No BFF o único JWT que o servidor valida é o **ID token**, recebido do endpoint de tokens numa ligação TLS iniciada por nós; não há JWT de origem não confiável, logo nenhuma amplificação de pedidos ao JWKS por pedidos forjados.

### Tokens e validação

- Algoritmo **ES256** (web keys ECDSA P-256 configuradas no primeiro arranque, ver o runbook); lista aceite em `PITBOX_OIDC_ALLOWED_ALGS`, por omissão `ES256` (agilidade criptográfica).
- Tempos da instância: access token 10 min, ID token 5 min, refresh token 168 h de inatividade e 720 h absolutas. As quatro são definidas em conjunto, senão o arranque do Zitadel falha.
- **O access token é opaco** (`OIDC_TOKEN_TYPE_BEARER`): o BFF nunca o lê, só o apresenta ao `userinfo`. Um JWT convidaria a confundi-lo com o ID token (mesma chave, mesma `aud`). Quando houver Bearer (F2-06), decide-se se o access token passa a JWT (validação local) ou se fica opaco (introspeção).
- **O ID token não leva dados pessoais.** Vai no `id_token_hint` do redirect de logout (histórico do browser, logs de proxy), por isso a app é registada sem `idTokenUserinfoAssertion`. O email, o `email_verified` e o nome obtêm-se no callback pelo endpoint `userinfo` (`go-oidc` `Provider.UserInfo`), do lado do servidor, com o access token recebido na troca do código. O `sub` do `userinfo` tem de ser igual ao do ID token (OIDC Core 5.3.2); se não for, o login é recusado. Uma falha do `userinfo` é 503 (o IdP está com problemas), não 403.
- ID token validado com `oidc.IDTokenVerifier` (`SupportedSigningAlgs` da configuração, `ClientID`, issuer) e `nonce` verificado à mão. Tolerâncias reais do `go-oidc`: `exp` sem tolerância, `nbf` com 5 min fixos.
- Descoberta do provider **preguiçosa** com novas tentativas: o arranque do `pitbox-api` não depende do Zitadel; `/auth/login` responde 503 até haver provider; `/healthz` não é afetado.
- Utilizador novo só entra com `email_verified = true`.

### Cookies

| Cookie | Atributos | Conteúdo |
| --- | --- | --- |
| `__Host-pitbox_login` | Secure, HttpOnly, **SameSite=Lax**, Path=/, Max-Age=600 | `{state, nonce, code_verifier, return_to, exp}` cifrado com AES-256-GCM (sem Valkey no pré-login) |
| `__Host-pitbox_session` | Secure, HttpOnly, **SameSite=Strict**, Path=/ | 32 bytes aleatórios; no Valkey só o SHA-256 |

O cookie de pré-login é Lax porque o regresso do Zitadel é uma navegação entre sites (em desenvolvimento `localhost:8080` e `localhost:8081` são o mesmo *site*, por isso o erro só se veria em produção). **Não existe nenhum modo de desenvolvimento sem `Secure` nem `__Host-`**: o Safari não aceita cookies `Secure` em `http://localhost` (WebKit 232088, aberto); HTTPS local (mkcert + Traefik) fica como subtarefa do F1-09.

### Callback

1. Sem cookie de pré-login válido: **400**, sem qualquer chamada ao endpoint de tokens. O `state` compara-se com `subtle.ConstantTimeCompare` antes de falar com o Zitadel; o cookie é apagado sempre.
2. A resposta de sucesso é uma página **200** com `<meta http-equiv="refresh">` (a CSP proíbe script inline), `Cache-Control: no-store` e `Referrer-Policy: no-referrer`. Um 302 depois de uma navegação entre sites perderia o cookie Strict no primeiro carregamento.
3. O ID da sessão é gerado no callback (nunca reutiliza um anterior: sem *session fixation*).

`return_to`: `url.Parse`, caminho absoluto sem host; rejeita `//`, `\`, esquemas e caracteres de controlo (teste de fuzz).

### Chaves

Uma chave mestra de configuração com `kid`, e **uma chave por função** derivada com HKDF-SHA256 (pré-login, refresh tokens). Rodar a chave mestra = novo `kid`; o antigo continua válido só para decifrar até as sessões expirarem.

### Sessão (Valkey)

Chave `pitbox:sess:<sha256 do cookie>`. Guarda apenas **identificadores**: `user_id`, `sub`, `sid` do Zitadel, `amr`, `auth_time`, datas, geração e o refresh token **cifrado**. Sem email, sem nome, sem access token (o BFF não precisa dele depois da troca) e sem ID token além do necessário para o `id_token_hint` do logout (cifrado com a mesma função de refresh).

- Duração: **7 dias de inatividade, 30 absolutos**, alinhadas com o Zitadel (refresh 168 h / 720 h).
- A API liga-se ao Valkey com um utilizador ACL limitado a `pitbox:sess:*`, `pitbox:lock:*` e `pitbox:rl:*`, **sem `@dangerous`** (o rate limiting do F1-04b usa `pitbox:rl:*`).
- Índices por `sub` e por `sid` para o logout remoto e o *back-channel logout* (F1-03c).

### Refresh fora do caminho crítico

O access token tem 10 min; é também a janela máxima de revogação (um utilizador desativado perde a sessão no refresh seguinte).

1. Com `access_exp` ultrapassado: `SET pitbox:lock:refresh:<sid> <token> NX PX 10000`.
2. Quem ganha faz o refresh (prazo de 5 s), grava com Lua *compare-and-set* por geração e liberta o lock com Lua (só apaga se o valor for o seu). Sem `singleflight` nem `valkeylock`.
3. Quem perde o lock, **dentro dos 2 min de graça** (contados desde a expiração do access token), segue com a sessão atual sem esperar.
4. Quem perde o lock **fora da graça** (um utilizador que esteve parado mais de 12 min e cuja app faz vários pedidos ao abrir) **espera pelo vencedor**: relê a sessão a cada ~100 ms até o `access_exp` avançar, no máximo ~6 s (o prazo do vencedor mais 1 s). Se esgotar, devolve `UNAVAILABLE`, nunca `UNAUTHENTICATED`: não obter resposta a tempo não diz nada sobre o utilizador. Se o vencedor terminou a sessão, quem espera vê-a apagada e recebe `UNAUTHENTICATED`.
5. `invalid_grant` (e, no Zitadel, `invalid_request` com `Errors.User.*`) → sessão apagada e registo na auditoria. Erro de rede ou 5xx → graça e, passada, `UNAVAILABLE`.

A graça cobre a **lentidão ou falha do IdP**, não a corrida entre pedidos: a corrida resolve-se esperando. (Defeito encontrado na revisão do PR: com o lock perdido fora da graça, 4 de 5 pedidos simultâneos eram recusados.)

Sem este lock, dois pedidos em paralelo enviariam o mesmo refresh token rodado e o segundo deslogaria o utilizador sem motivo.

### Logout

`POST` com `Origin` verificado (nunca GET). Ordem: (1) revoga o refresh token no servidor (`/oauth/v2/revoke`, como cliente confidencial); (2) apaga a sessão e o cookie; (3) **303** para `end_session` com `id_token_hint` (o Zitadel aceita-o expirado) e `post_logout_redirect_uri` registado.

### Primeiro login (F1-03b)

A RLS de `identity.user_account` bloqueia o `SELECT` por `idp_subject` e o `INSERT` antes de existir `app.user_id`. Uma função `identity.resolve_login(p_sub, p_email, p_display_name) RETURNS uuid`, `SECURITY DEFINER`, dona `pitbox_owner`, `search_path = pg_catalog, pg_temp`, nomes qualificados, `REVOKE ALL ... FROM PUBLIC`, `EXECUTE` só para `pitbox_app`:

- procura **só por `sub`**; nunca liga contas por email;
- conta apagada: o login falha; `sub` vazio: rejeitado;
- email de outro `sub`: erro próprio; o utilizador vê uma mensagem genérica e fica entrada na auditoria;
- email ou nome mudaram no IdP: atualiza.

### Auditoria e logs

`audit.audit_log` com `team_id` NULL: login, falha, logout e sessão revogada. Em logs, traces e auditoria nunca entram tokens, `code`, `state`, email nem *claims*.

### Provisionamento

Script Python idempotente por estado desejado (`scripts/zitadel-provision.py`). O PAT de `IAM_OWNER` tem validade de 30 dias, fica num ficheiro 0600 fora do repositório (e coberto pelo `.gitignore`), e nunca aparece no output. O `client_id` e o segredo vão para o `.env` local. Desliga-se o OTP por SMS e por email na política de login: no Zitadel ambos dão `amr` `mfa`, o que estragaria o *step-up* do F1-03c.

### Para o F2-06 (Bearer, telemóvel)

Se o access token for JWT, o Zitadel assina-o com `typ` `JWT`, a mesma chave e a mesma `aud` do ID token (segundo a revisão do código; hoje a app usa access tokens opacos). Quando o Bearer existir: exigir o *claim* `client_id` numa lista de permitidos, **rejeitar tokens com `nonce`, `at_hash` ou `auth_time`** (são ID tokens) e testar um ID token apresentado como Bearer. Aí é preciso também decidir o JWKS: o `RemoteKeySet` sem TTL e com pedido por assinatura inválida deixa de ser aceitável (ver o contexto).

## Alternativas consideradas

| Alternativa | Porque não |
| --- | --- |
| `KeySet` próprio ou `jwx` já no F1-03 | Sem Bearer não há JWT de origem não confiável; custo sem benefício agora |
| Pré-login no Valkey | O cookie cifrado dispensa estado e um ataque ao `/auth/callback` sem cookie nem chega ao Valkey |
| Cookie de sessão `Lax` | Perde a defesa sem custo real: a página 200 resolve o primeiro carregamento |
| `singleflight` no refresh | Só serializa dentro de um processo; com várias instâncias (Cloud Run) o lock tem de estar no Valkey |
| `valkeylock` | Biblioteca de locks distribuídos mais complexa do que o caso pede (um lock por sessão com TTL) |
| Sessão com email e nome | Dados pessoais desnecessários num armazém de segredos; vêm da base quando precisam |

## Consequências

- O Valkey guarda segredos (TM-AUTH-06): em produção exige TLS e autenticação; ACL restrita desde já.
- O Zitadel não revoga a família de refresh tokens: a spec §3.4 ("reutilização revoga toda a família") **fica por cumprir até ao F2-06**. No BFF o risco é menor (o refresh token nunca sai do servidor).
- A arquitetura §5.1 (principal e *step-up*) passa a ler `auth_time` e `amr` da sessão e não do token.
- Novas variáveis: `PITBOX_OIDC_*`, Valkey, base de dados, chave mestra e durações da sessão.
- **O fim de uma sessão por utilizador desativado depende da resposta do Zitadel.** Verificado na `v4.19.4` com `scripts/auth-smoke.py`: para um utilizador desativado o refresh falha com `invalid_request` e a descrição `Errors.User.NotActive` (não `invalid_grant`). A sessão acaba com `invalid_grant` e com `invalid_request` + `Errors.User.*`; qualquer outra recusa (cliente recusado, pedido mal formado, falha de rede) mantém a sessão (e `UNAVAILABLE` depois da graça). Uma versão do Zitadel que mude isto faz falhar o smoke.
- `GET /auth/session` (identificadores apenas) existe para a app saber se está autenticada sem ler tokens; renova o access token se preciso.
- **Trocar a chave de assinatura numa instância com clientes:** entre `CreateWebKey` e `ActivateWebKey` tem de passar mais do que o *max-age* do JWKS (5 min por omissão, `OIDC.JWKSCacheControlMaxAge`; a documentação do `ActivateWebKey` desaconselha ativar uma chave criada dentro dessa janela), senão clientes com o JWKS em cache rejeitam os tokens novos. O script ativa-a logo porque a instância local não tem clientes.
- **Valkey sem cluster:** os scripts Lua (criar, atualizar e apagar sessão) tocam em várias chaves (sessão, índice por utilizador, índice por `sid`), por isso o Valkey é *standalone* ou primário com réplica, não *cluster*. Se um dia for preciso, as chaves de uma sessão passam a partilhar uma *hash tag* (`{<ref>}`).
- A chave ES256 não se configura por variáveis de ambiente do Zitadel (o Viper só aplica variáveis a chaves presentes no `defaults.yaml`; verificado: o arranque falha com `Errors.WebKey.Config`): `make zitadel-provision` cria e ativa-a.
- `/auth/*` fica sem rate limiting no servidor: risco aceite e ligado ao F5-05 (Cloudflare, por IP).
- Em desenvolvimento só o Chrome e o Firefox funcionam com `__Host-` sobre `http://localhost`; o Safari espera pelo HTTPS local do F1-09.
- Revisitar se o Zitadel passar a revogar a família de refresh tokens, ou antes do F2-06.
