# 0017 — App web e BFF na mesma origem

- **Estado:** Aceite (Nelson, 2026-10-25)
- **Data:** 2026-10-25
- **Cartão:** F1-09 (#37)
- **Relacionado:** [ADR 0004](0004-zitadel-bff.md), [ADR 0013](0013-sessoes-bff-e-tokens.md),
  arquitetura §11.1, §11.3, §12.1 e §13.

## Contexto

A arquitetura previa Cloudflare Pages para a app estática e Cloud Run para a API, em origens
diferentes, com CORS "só para o domínio da app". O BFF atual (ADR 0013) tem uma premissa mais
forte: a app e a API partilham a origem.

- O logout compara `Origin` com `PITBOX_PUBLIC_URL`;
- o `foreignRequest` recusa `Sec-Fetch-Site` diferente de `same-origin`/`none`;
- o cookie de sessão é `__Host-pitbox_session`, Secure, HttpOnly e SameSite=Strict, e fica
  preso ao host da API.

Com origens diferentes, o cookie não é enviado nas chamadas ao BFF, o `Origin` não coincide e
a proteção contra CSRF teria de ser afrouxada. A app não teria uma sessão simples sem CORS com
credenciais.

## Decisão

A **app web e o BFF/RPC são servidos a partir de uma só origem** (por exemplo,
`https://app.pitbox.pt`):

- `/auth/*` e `/pitbox.*` são encaminhados para o Cloud Run;
- o resto é servido como estático (Cloudflare Pages, uma Pages Function/Worker ou o próprio
  `pitbox-api` a servir `dist/`);
- não há CORS para a web;
- o contrato do BFF e os cookies `__Host-` mantêm-se como no ADR 0013.

O mecanismo concreto de produção fica para o cartão de infraestrutura (F5). O F1-09 usa o proxy
do Vite em desenvolvimento e `vite preview` nos E2E, ambos na mesma origem.

## Consequências

- A app usa `baseUrl: window.location.origin` e não configura uma API cross-origin.
- Em desenvolvimento, o browser vê `http://localhost:5173`; o Vite encaminha `/auth` e
  `/pitbox.` para a API, e `PITBOX_PUBLIC_URL` é também `http://localhost:5173`.
- O provisionamento local do Zitadel regista as duas origens locais (`8081` e `5173`), para
  não alternar entre `make run-api` e `make app-dev`.
- O Capacitor (F2-06) corre noutra origem (`https://localhost` ou `capacitor://localhost`),
  usa Bearer/AppAuth e **não** usa o cookie de sessão web. Nesse cartão entra CORS com
  allowlist dessas origens e **sem** `Access-Control-Allow-Credentials`.
- A arquitetura §11.1 e §13 passam a refletir esta decisão.

## Alternativas consideradas

| Alternativa | Porque não |
| --- | --- |
| Pages numa origem e API noutra, com CORS + credenciais | Obriga a aceitar `Origin` da app no BFF, a afrouxar `foreignRequest` e o SameSite; aumenta a superfície de CSRF |
| Subdomínios irmãos (`app.` e `api.`) | O `__Host-` não se partilha entre hosts; continuaria a exigir CORS |
| BFF num subdomínio da API e app a chamar o BFF com CORS | Duplica a origem pública e as regras de cookie; não resolve o `SameSite=Strict` |
| Servir a app pelo próprio `pitbox-api` desde já | É uma opção válida do mecanismo de produção; fica para o cartão de infraestrutura |

## Prova

- `app/vite.config.ts`: proxy `/auth` e `/pitbox.`, `baseUrl` same-origin.
- `app/tests/e2e`: login/lista/logout com BFF stub, cookie `__Host-`, `document.cookie` sem
  sessão, CSP de produção.
- `backend/internal/platform/auth/flow_test.go`: logout com `Origin`, `/auth/session` e
  cookies.
- `docs/adr/0013-sessoes-bff-e-tokens.md`: decisão dos cookies e do BFF.
