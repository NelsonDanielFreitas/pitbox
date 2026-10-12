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
