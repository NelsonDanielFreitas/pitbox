# 0035 — Códigos de recuperação como segundo fator da recuperação

- **Estado:** Aceite (Nelson, 2026-11-10: «exigir email + código»); implementado (F2-06b, 2026-11-10)
- **Data:** 2026-11-10
- **Cartão:** F2-06b (#166), issue #158
- **Relacionado:** [ADR 0034](0034-recuperacao-de-conta-so-com-passkey.md) (estende), [ADR 0019](0019-defesa-palavra-passe-login.md); ameaças TM-AUTH-18 e TM-AUTH-24; spec §3.5.

## Contexto

Os códigos de recuperação (8 por conjunto, 128 bits, HMAC) existiam, mas não serviam para nada na app: `POST /auth/recovery-codes/consume` exige uma sessão aberta e só sobe o step-up. Quem perde a passkey recuperava só com o email (ADR 0034). Para uma conta com passkey, que é resistente a phishing, isso é um rebaixamento: um email comprometido bastava.

NIST SP 800-63B-4 §4.2 (recuperação de conta, AAL2): exige **dois códigos de recuperação obtidos por métodos diferentes** (guardado + emitido) ou um código mais um autenticador de fator único ligado à conta. O nosso conjunto é o código **guardado**; o link por email é o **emitido** (24 h no máximo; o do Zitadel dura 15 min). Os códigos guardados têm ≥ 64 bits e só existem em hash (já cumprido); a verificação está sujeita a *throttling* (§3.2.2); toda a recuperação gera uma notificação ao titular. OWASP: não bloquear contas por falhas de recuperação (negação de serviço) e resposta uniforme (anti-enumeração).

## Decisão

1. **Os códigos nunca abrem sessão nem substituem a passkey.** Servem de segundo fator da recuperação: **email + código guardado**. Só depois de os dois baterem certo o Zitadel envia o link de registo da passkey nova (`sendLink`, como no ADR 0034).
2. `POST /auth/recover {email, code?}` continua a responder **202 igual** antes de qualquer acesso à conta (corpo, cabeçalhos, tempo). Um `code` que nem tem o formato de um código dá 400 (depende só do que a pessoa escreveu, nunca da conta).
3. Em segundo plano, para uma conta com passkey, o `pitbox-api` pergunta à base (`identity.attempt_recovery_by_subject`, numa só chamada atómica):
   - `no_codes` ou `unknown_user`: a conta não tem códigos por usar, logo não há segundo fator a exigir; recupera só com o email, como no ADR 0034 (o aviso `recovery_ready=false` continua a pedir que gerem códigos);
   - `invalid`: tem códigos e nenhum serve (ou não veio nenhum): não envia nada (`code_required` ou `invalid_code` na auditoria);
   - `consumed`: o código serve e fica gasto (uso único; o `UPDATE` bloqueia a linha, por isso dois pedidos com o mesmo código não passam os dois).
4. Depois de enviar o link com um código: **gastar o resto do conjunto** (pode ter sido visto) e **terminar todas as sessões** da conta (`DeleteAllBySubject`). A conta fica com 0 códigos, `recovery_ready=false`, e a app mostra o aviso até se gerar um conjunto novo (NIST: emitir novo código após o uso).
5. **Limites sem negação de serviço.** Por IP (10/h) e globais (120/h) contam todos os pedidos. Por email há duas contagens separadas: **links enviados** (3/h, só conta quando se envia) e **falhas de código** (10/h, **por email e por IP de quem pede**). Quem só sabe o email enche a sua própria contagem e nunca a do dono, que pede de outro endereço. O `consume` com sessão passa a ter limite por utilizador (20/h). Em falha do Valkey o pedido é descartado (falha fechada), como antes.
6. Falha fechada: sem a base de códigos ou sem a chave para os verificar, nada é enviado (`codes_unavailable`).

## Alternativas

- **O código como login.** Recusada: um código roubado passaria a ser a conta toda, num fator único e sujeito a phishing, e a sessão não teria AAL forte.
- **Código → `returnCode` → `passkey/set` sem email.** Recusada: o `pitbox-api` passaria a tratar o código do Zitadel, ficaria com um fator único e a credencial de serviço mais exposta.
- **Só email (ADR 0034).** Recusada por ti: um email comprometido chega.
- **Só email, com espera de 24 h.** Recusada: exige estado e tarefas, e continua a ser um fator.

## Consequências

- Quem perder a passkey **e** os códigos numa conta com códigos fica sem recuperação automática. Mitigação: o aviso persistente, a contagem em «Conta e segurança» e o aviso de poucos códigos.
- **Janela aceite:** uma conta com 0 códigos por usar (nunca os gerou, ou acabou de recuperar e ainda não gerou outros) recupera só com o email. É o estado do ADR 0034; fecha-se gerando códigos.
- A notificação é o email do Zitadel com o link. O aviso de **conclusão** (passkey registada) continua a depender da Action `user.human.passkey.added` do Zitadel (cartão à parte, proposto no ADR 0034).
- Se o envio do link falha depois de o código ser gasto, esse código perde-se (o resto do conjunto só é gasto depois de o envio correr bem).
- Prova: testes de handler com um armazém falso (código em falta, errado, de outra conta, certo, repetido; igualdade das respostas; limites por quem pede; nada do código em logs nem na auditoria), testes SQL (`rls_and_constraints_test.sql`), testes de mutação (retirar a exigência do código, os limites por email, o limite do `consume`, o gasto do conjunto ou o fim das sessões faz falhar um teste) e prova no telemóvel.
