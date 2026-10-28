# 0019 — Defesa contra ataques à palavra-passe no login

- **Estado:** Proposto (F1-03e)
- **Data:** 2026-10-27
- **Cartão:** F1-03e (#141)
- **Relacionado:** [ADR 0013](0013-sessoes-bff-e-tokens.md) (sessões BFF), [ADR 0014](0014-rate-limiting-em-camadas.md) (rate limiting na API); arquitectura §5; threat model TM-AUTH-01, TM-AUTH-13.

## Contexto

A especificação §3.2 pede:

- mensagens genéricas que não revelem se uma conta existe;
- 5 tentativas falhadas por conta em 15 min, com atraso progressivo e CAPTCHA;
- **nunca** bloqueio permanente, para evitar negação de serviço à vítima.

O Zitadel v4.19.5 não tem atraso progressivo nem CAPTCHA de origem. O bloqueio nativo
(`maxPasswordAttempts`) mantém a conta bloqueada até um administrador a desbloquear; é
exactamente o comportamento que a especificação proíbe. A verificação de palavras-passe
comprometidas (HIBP) e o CAPTCHA não estão disponíveis na v0.1.

A superfície de login é a UI v2 do Zitadel. A password é submetida por *server actions*
(POST) no próprio caminho `/ui/v2/login/...`; o contentor `zitadel-login` fala com o
`zitadel-api` directamente. Um limite em `/api` não vê essas tentativas.

## Decisão

1. **Sem bloqueio permanente no IdP.**
   - `maxPasswordAttempts = 0` na lockout policy do Zitadel.
   - `maxOtpAttempts` é preservado; o objectivo é não bloquear a conta por password, não
     enfraquecer TOTP.
   - As tentativas continuam a ser limitadas por IP no edge; a conta mantém-se utilizável.

2. **Rate limit por IP nos POST do login.**
   - Router Traefik próprio:
     `PathPrefix('/ui/v2/login') && Method('POST')`.
   - Middleware `rateLimit` com valor inicial `average: 10`, `burst: 10`, `period: 1m`.
   - GET, RSC e `/_next/` não são limitados.
   - Não se aplica a `/api`; o login v2 fala com o Zitadel directamente.
   - O valor será medido com um registo completo antes de ser fixado.
   - O objectivo é 11 POST rápidos do mesmo IP → 429, sem afectar um login normal.

3. **Sem CAPTCHA/HIBP na v0.1.**
   - O CAPTCHA e a verificação HIBP exigem o edge/IdP da F5; ficam registados na issue #142.
   - Risco aceite: com 2 utilizadores, passkeys, MFA obrigatório para donos e password mínima
     de 12 caracteres, o risco residual é inferior ao custo de construir um serviço de
     verificação próprio agora.
   - A decisão será revisitada antes do lançamento público e no F5.

4. **Palavra-passe compatível com NIST SP 800-63B-4.**
   - Mínimo de 12 caracteres.
   - Sem regras de composição obrigatórias.
   - Sem expiração periódica.
   - HIBP fica para a F5, com o risco aceite e a data registados.

5. **Anti-enumeração parcial, não absoluta.**
   - `ignoreUnknownUsernames = true`.
   - Mensagens genéricas no BFF.
   - A página de email por verificar só aparece depois de uma autenticação bem-sucedida no
     IdP.
   - Risco residual: com passkeys, o ecrã pode revelar que a conta existe; o registo também
     pode revelar um email já usado. O rate limit e a verificação de email reduzem o abuso,
     mas não o eliminam.

6. **Registo e verificação.**
   - `allowRegister = true`.
   - Email verificado é requisito para criar `user_account`.
   - `allowExternalIdp = false` e `disableLoginWithPhone = true` reduzem a superfície.
   - Passkeys permitidas (`passwordlessType = PASSWORDLESS_TYPE_ALLOWED`), propostas após o
     registo; são o método recomendado.

7. **Validade dos códigos.**
   - Verificação de email: 30 min.
   - Recuperação de palavra-passe: 15 min.
   - Configurada nos secret generators do Zitadel e reflectida no texto do email.

## Limite do rate limit por IP

- Em Docker/WSL, o `userland-proxy` pode fazer chegar todos os pedidos ao Traefik com o IP do
  gateway. O teste local prova o mecanismo, não o isolamento entre clientes.
- Em produção atrás da Cloudflare (F5-05), o Traefik tem de configurar
  `forwardedHeaders.trustedIPs` com os intervalos da Cloudflare e um `ipStrategy` adequado.
  Sem isso, o limite é global e pode ser contornado ou bloquear todos.
- A Cloudflare aplicará também WAF/rate limiting e Turnstile no F5.

## Alternativas consideradas

| Alternativa | Porque não |
| --- | --- |
| Manter o lockout nativo do Zitadel | Bloqueio permanente até intervenção administrativa; contradiz a spec e permite DoS à vítima |
| Limite por conta no BFF | O BFF não vê a password; a autenticação é no Zitadel. Exigiria um proxy de credenciais, aumentando a superfície |
| CAPTCHA agora | Exige integração com o edge/Cloudflare e mais um fornecedor; F5 |
| HIBP agora | O Zitadel não tem a funcionalidade; exige um serviço/webhook próprio e um tratamento de falhas; F5 |
| Limitar todos os GET/RSC da UI | Conta pedidos normais e bloqueia logins legítimos |
| Limitar `/api` | O login v2 fala com o API internamente; não cobre as tentativas de password |

## Consequências

- Uma conta já não fica bloqueada por 10 tentativas falhadas.
- A força bruta fica limitada por IP, não por conta; um atacante distribuído continua possível
  até ao CAPTCHA/F5.
- O rate limit pode afectar utilizadores atrás do mesmo IP/NAT; o valor é conservador e será
  medido.
- Os códigos de verificação/recuperação passam a ter validade explícita e textos coerentes.
- O access log local deixa de registar caminho/query, para não guardar códigos de uso único.
- A decisão sobre HIBP/CAPTCHA fica ligada à issue #142 e ao F5-05.
- O limite no edge não substitui a autorização nem a validação do servidor.

## Prova

- `scripts/test_zitadel_provision.py`: políticas completas, lockout, secret generators e
  idempotência.
- `scripts/auth-smoke.py`: registo, verificação por Mailpit, recuperação, 10 falhas sem
  bloqueio, 11 POST → 429 e ausência de códigos nos logs.
- `backend/internal/platform/auth/flow_test.go`: email não verificado, redirect e revogação
  de tokens.
- `app/tests/e2e`: `/confirm-email`, Mailpit e axe.
- `make smoke-auth` documentado no PR com o resultado manual.
