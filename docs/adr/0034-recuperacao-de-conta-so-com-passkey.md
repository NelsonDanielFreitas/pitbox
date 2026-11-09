# 0034 — Recuperação de conta só com passkey

- **Estado:** Aceite (confirmado pelo Nelson em 2026-11-09); implementação por fazer
- **Data:** 2026-11-09
- **Cartão:** F2-06b (#166), issue #158
- **Relacionado:** [ADR 0019](0019-defesa-palavra-passe-login.md) (defesa de palavra-passe e login, anti-enumeração parcial), [ADR 0032](0032-login-movel-e-sessao-ligada-ao-dispositivo.md); ameaças TM-AUTH-* (linha nova a criar).

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
