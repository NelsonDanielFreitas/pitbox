# 0004 — Zitadel como IdP; BFF com cookie para a web

- **Estado:** Aceite (Nelson, 2026-10-11)
- **Data:** 2026-10-11 (decisão); registado em 2026-10-12
- **Cartão:** F0-07 (registo); origem: arquitetura §1, §5.1

## Contexto

O backend não deve ver nem guardar palavras-passe. É preciso OIDC, passkeys, MFA e alojamento na UE com custo previsível.

## Decisão

Zitadel como IdP (OIDC + PKCE, passkeys, MFA obrigatório para donos), na UE. Web: padrão BFF no `pitbox-api` (Authorization Code + PKCE no servidor, tokens guardados no Valkey, ao browser só um cookie `__Host-` HttpOnly, Secure, SameSite=Strict). Android/iOS: AppAuth com o browser do sistema, tokens no Keychain/Keystore. Access token JWT ES256 de 10 min com audiência `pitbox-api`; refresh token rotativo com deteção de reutilização. As equipas e papéis **não** vão no token: verificam-se no OpenFGA.

## Alternativas consideradas

| Alternativa | Porque não |
| --- | --- |
| Keycloak | Mais operação própria para o mesmo resultado |
| Auth0 | Custo e alojamento fora da UE por omissão |
| Tokens no JavaScript (SPA pura) | Exposição a XSS; o BFF mantém os tokens fora do browser |

## Consequências

- O **Valkey passa a guardar segredos** (tokens de sessão): em produção exige TLS e autenticação, e entra no threat model.
- O espelho do utilizador na base (`identity.user_account.idp_subject`) guarda só a claim `sub`, email e nome; as credenciais nunca estão na base do PitBox.
- A ligação Zitadel ↔ `user_account` só existe a partir do F1-03b (criação no primeiro login).
- No compose local, o Zitadel tem decisões de conveniência documentadas no F0-03 (PAT de arranque com validade longa, contentores como root): são só para desenvolvimento.

## Emenda (2026-10-13, F1-03) — ver [ADR 0013](0013-sessoes-bff-e-tokens.md)

Verificada contra o Zitadel `v4.19.4` e o `go-oidc` v3.21.0. O resto do ADR mantém-se.

- **ES256 não é o padrão do Zitadel.** O JWKS por omissão só tem RS256. O ES256 obtém-se configurando as *web keys* (ECDSA P-256) no primeiro arranque; o algoritmo aceite fica em `PITBOX_OIDC_ALLOWED_ALGS` (por omissão `ES256`). O tipo de access token por omissão é opaco e o tempo de vida 12 h: a app é registada com access tokens opacos (o BFF não os lê; ver ADR 0013) e os tempos são fixados na instância (access 10 min, ID token 5 min, refresh 168 h de inatividade e 720 h absolutas).
- **`auth_time`, `amr` e `sid` só existem no ID token.** O principal e o *step-up* leem-nos da sessão do BFF, preenchida com o ID token validado no login (arquitetura §5.1).
- **Sem revogação da família de refresh tokens.** O Zitadel rejeita o refresh token antigo mas, segundo a revisão do código, não revoga a família. A spec §3.4 ("reutilização revoga toda a família") **fica por cumprir até ao F2-06**; no BFF o refresh token nunca sai do servidor.
- **Tolerâncias do `go-oidc`:** `exp` sem tolerância e `nbf` com 5 min fixos (a §5.1 dizia 30 s).
- **Sem Bearer no F1-03:** o caminho `Authorization: Bearer` passa para o F2-06.
- **Valkey:** cliente `valkey-go` (não `go-redis`).
