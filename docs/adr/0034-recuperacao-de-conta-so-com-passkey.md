# 0034 — Recuperação de conta só com passkey

- **Estado:** Aceite (confirmado pelo Nelson em 2026-11-09); implementado (F2-06b, 2026-11-10)
- **Data:** 2026-11-09
- **Cartão:** F2-06b (#166), issue #158
- **Emenda:** [ADR 0035](0035-codigos-de-recuperacao-como-segundo-fator.md): uma conta com códigos de recuperação exige email + código.
- **Relacionado:** [ADR 0019](0019-defesa-palavra-passe-login.md) (defesa de palavra-passe e login, anti-enumeração parcial), [ADR 0032](0032-login-movel-e-sessao-ligada-ao-dispositivo.md); ameaças TM-AUTH-24 e TM-AUTH-20.

## Contexto

Uma conta que só tem passkey fica sem saída se a perder (telemóvel novo, mudança do domínio do Zitadel, ADR 0029). O spike de 2026-11-09 contra o Zitadel 4.19.5 mostrou:

- O login v2 só mostra «Use password» na página da passkey; não há entrada «perdi a passkey» para contas só com passkey.
- A API v2 permite, para um utilizador sem palavra-passe, `PasswordReset` e `CreatePasskeyRegistrationLink` (ambos com `returnCode`, que devolve o código em vez de o enviar). Ambas exigem credencial de gestão do Zitadel.
- O ADR 0019 mantém `ignoreUnknownUsernames=true`: o ecrã de login não revela se a conta existe.

## Decisão

1. **Endpoint próprio anti-enumeração:** `POST /auth/recover` `{email}` responde **sempre 202** com o mesmo corpo e o mesmo tempo de resposta (trabalho feito em segundo plano, resposta antes dele), exista a conta ou não. Se existir e tiver passkey, o `pitbox-api` pede ao Zitadel um link de registo de passkey **enviado por email pelo Zitadel** (`sendLink`), nunca devolvido ao cliente. Com `sendLink` o código não passa pelo `pitbox-api`, e a validade (15 min, ADR 0019) é a do Zitadel.
2. **Limites:** por IP e por email (hash), ambos em Valkey; teto global; falha de Valkey recusa em modo seguro (a resposta continua 202, o pedido é descartado e registado).
3. **Credencial de serviço mínima:** um utilizador máquina do Zitadel só para esta função, com o papel mais estreito que permita as chamadas necessárias. **O papel exato determina-se por teste** na implementação (a documentação consultada não o fixa) e fica registado aqui antes do merge. O segredo entra só pelo gestor de segredos (`.env` 0600 em local), nunca em logs; PAT de curta duração e rodável. Se o papel mais estreito possível for demasiado largo (por exemplo gestão total de utilizadores), a decisão volta a ti antes de avançar.
4. **Segundo caminho:** `/auth/session` passa a devolver `recovery_ready`. Contas com passkey sem palavra-passe nem códigos de recuperação veem um aviso persistente até gerarem os códigos.
5. **Auditoria:** cada pedido fica em `audit.audit_log` (hash do email, resultado interno, request ID); o cliente nunca vê o resultado.
6. **Interface:** entrada «Perdi a passkey» no ecrã de login (web e nativo), que mostra sempre a mesma mensagem: «Se existir uma conta, enviámos um email».

## Alternativas

- **Só avisos e códigos de recuperação.** Sem credencial nova, mas quem perdeu tudo não tem saída automática. Recusada por ti.
- **Reposição de palavra-passe pelo Zitadel.** Não consegui provar que o login v2 a oferece a contas só com passkey; exigiria o mesmo tipo de credencial.
- **Devolver o código ao cliente (`returnCode`).** Tornaria o `pitbox-api` um oráculo de enumeração e de tomada de conta. Recusada.

## Consequências

- O `pitbox-api` passa a ter uma credencial para o Zitadel. Linha nova no threat model (roubo da credencial de serviço, abuso do endpoint para spam de email, enumeração por tempo de resposta) e runbook de rotação.
- O email de recuperação é o único fator de recuperação: quem controla o email da conta controla o registo de uma passkey nova. Aceite e descrito no threat model; mitigado por limites, auditoria e aviso por email ao titular.
- Prova: teste ponta a ponta no `auth-smoke.py` (conta só com passkey → pedido → email no Mailpit → link de registo) e testes de igualdade de resposta e de tempo (conta existente e inexistente).

## Resultado da implementação (2026-11-10)

**Papel da conta de serviço (spike no Zitadel 4.19.5, por tentativa).** Nenhum papel embutido é estreito:

| Papel | Link de registo de passkey | Efeito colateral medido |
| --- | --- | --- |
| `ORG_USER_MANAGER` | 403 (não consegue) | — |
| `IAM_LOGIN_CLIENT` | sim | também bloqueia utilizadores e lê as definições OIDC |
| `IAM_USER_MANAGER` | sim | também bloqueia utilizadores |

Por decisão tua (2026-11-10) criou-se um **papel próprio**, `IAM_PITBOX_ACCOUNT_RECOVERY`, só com `user.read` e `user.passkey.write`. Medido: envia o link e lê utilizadores e métodos de autenticação; **não** bloqueia, não repõe palavra-passe, não apaga e não lê definições (403 em todos, provado no `auth-smoke`, passo 4c).

- **Mecanismo:** o Zitadel só aceita a lista inteira de papéis (uma lista por ficheiro ou ambiente substitui a lista por omissão). `scripts/zitadel-roles.py` copia a lista por omissão da versão fixada no compose e acrescenta o papel; o resultado, `deploy/compose/zitadel/roles.yaml`, monta-se no `zitadel-api` com `--config`. **Ao subir a versão do Zitadel** corre-se o script e revê-se o diff. O prefixo `IAM_` é obrigatório (o Zitadel recusa outros como papel de instância).
- **Utilizador máquina** `pitbox-recovery` e PAT de 90 dias criados e renovados por `make zitadel-provision` (renova quando faltam menos de 14 dias; `--rotate-recovery-pat` força), guardado em `PITBOX_ZITADEL_RECOVERY_PAT` no `.env` (0600); o provisionamento corrige papéis a mais.
- **Risco residual (aceite):** quem roubar o PAT consegue pedir o código de registo de passkey de **qualquer** conta (`returnCode`) e tomá-la. É inerente a `user.passkey.write`; mitiga-se com o PAT só no processo da API, validade curta, rotação e o runbook `rotacao-credencial-recuperacao.md`.

**Desvios e pormenores face à decisão:**

1. O limite por email (3/hora), por IP (10/hora) e global (120/hora) usa contadores de janela fixa no Valkey (`pitbox:rl:rec:*`, sem ACL novo): o limitador por segundo não exprime «por hora». Além disso há o limite por IP do limitador público (429), que depende só do IP, nunca do email.
2. O email entra na auditoria como **pseudónimo** (HMAC-SHA256 com chave derivada do anel, 128 bits) ou, se a conta existir, como `sub:<id>`; nunca o endereço. Motivos: `sent`, `no_account`, `no_passkey`, `upstream_error`, `throttled`, `dropped_busy`, `disabled`.
3. A conta procura-se **no Zitadel** por email (só contas humanas ativas com email verificado e resultado único), sem tabela nem coluna novas no PitBox.
4. A resposta é 202 antes de qualquer acesso à conta (trabalho em segundo plano, no máximo 8 em simultâneo; acima disso o pedido é descartado e auditado). Só um corpo inválido (400) e o limite por IP (429) diferem, e não dependem da conta.
5. `recovery_ready` em `/auth/session` é `false` só para quem tem passkey, não tem palavra-passe e não tem códigos por usar; calcula-se com `identity.count_unused_recovery_codes` e a consulta ao Zitadel, com cache de 60 s por utilizador (limpa ao gerar códigos). Se algo falhar o campo **sai** da resposta (o login nunca depende disto).
6. **Não implementado:** o aviso por email ao titular como mensagem à parte. O próprio email do Zitadel com o link é o aviso.
7. O aviso na app gera os códigos na **web e no Android**: o endpoint `/auth/recovery-codes` aceita agora o Bearer (ADR 0032, emenda) e a página «Conta e segurança» (`/account`) mostra quantos códigos restam, gera um conjunto novo e lista as sessões.
8. Prova: `auth-smoke.py` passo 4c (conta com passkey de software, email no Mailpit com o link `passkey/set`, conta desconhecida e conta só com palavra-passe sem email, limite de 3 emails, auditoria sem emails) e testes de igualdade de resposta, de ordem (resposta antes do trabalho) e de limites em `recover_test.go`.

**Revogar as passkeys antigas na recuperação — não implementado, com razão.** O registo da passkey nova acontece no Zitadel (link `passkey/set`); o PitBox não vê esse evento e por isso não pode terminar as outras sessões nem apagar passkeys no momento certo sem uma Action/webhook do Zitadel. Mitigações já feitas: a página «Conta e segurança» tem «terminar todas as outras sessões» e o email do Zitadel avisa o titular. **Proposta para um cartão próprio:** ligar um webhook/evento `user.human.passkey.added` (Zitadel Actions v2) ao PitBox para terminar as sessões e avisar o titular; apagar passkeys antigas só por escolha explícita (o `excludeCredentials` do Zitadel impede registar outra no mesmo aparelho enquanto a antiga lá estiver; foi o que aconteceu no teste no telemóvel, ver o fim deste ADR).

**Prova no telemóvel (SM-S911B, 2026-11-10).** Pedido por email → link → passkey nova registada com a impressão digital: funcionou depois de apagar as passkeys antigas da conta de teste. Com a passkey antiga ainda no aparelho o Chrome recusa (`InvalidStateError`, «already registered»): é o comportamento do WebAuthn, e é o caso em que a pessoa ainda consegue entrar com ela.
