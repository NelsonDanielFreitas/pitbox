# Runbook — rotação da credencial de recuperação de conta

Credencial: `PITBOX_ZITADEL_RECOVERY_PAT`, o token do utilizador máquina `pitbox-recovery` do Zitadel (papel `IAM_PITBOX_ACCOUNT_RECOVERY`: `user.read` e `user.passkey.write`; ADR 0034, TM-AUTH-24). Quem a tiver consegue pedir o código de registo de passkey de qualquer conta: tratar como segredo de produção.

## Rotação normal (a cada 90 dias, ou quando faltarem menos de 14)

1. Local: `make zitadel-provision` renova sozinho (escreve o PAT novo no `.env`, cria-o antes de apagar os antigos). Para forçar: `python3 -I scripts/zitadel-provision.py --rotate-recovery-pat`.
2. Reinicia o `pitbox-api` (lê o PAT no arranque). Até lá continua a usar o antigo, que já foi apagado: a recuperação falha em silêncio (`recovery lookup failed` no log, auditoria `upstream_error`).
3. Produção (F5): o PAT vive no gestor de segredos; o mesmo passo faz-se com o `provisioner` contra o Zitadel de produção e publica-se uma versão nova do segredo.

## Suspeita de fuga

1. Apaga já todos os PAT do utilizador `pitbox-recovery` (consola do Zitadel → Utilizadores → `pitbox-recovery` → Tokens) ou desativa o utilizador: a recuperação pára, o login não é afetado.
2. Verifica na auditoria os pedidos `auth.recovery_requested` e, no Zitadel, as passkeys registadas recentemente (eventos `user.human.passkey.added`); revoga as que não reconheces e termina as sessões dessas contas.
3. Gera um PAT novo (`--rotate-recovery-pat`) e reinicia a API.

## Sinais a vigiar

- Muitos `throttled` ou `no_account` seguidos na auditoria: alguém a sondar o endpoint.
- `upstream_error` constante: o PAT expirou ou o papel foi alterado (`make zitadel-provision` repõe o papel certo).

## Ao subir a versão do Zitadel

Corre `python3 -I scripts/zitadel-roles.py`, revê o diff de `deploy/compose/zitadel/roles.yaml` (a lista de papéis por omissão pode ter mudado) e só depois `make up`.
