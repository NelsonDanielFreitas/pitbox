# 0027 — E2E do sync contra a API real

- **Estado:** Aceite (Nelson, 2026-10-31)
- **Cartão:** F1-14
- **Relacionado:** [ADR 0004](0004-zitadel-bff.md), [ADR 0025](0025-formato-de-assinatura-e-cursor-do-sync.md), [ADR 0026](0026-feedback-do-piloto-no-servidor.md); ameaças TM-SYNC-08, TM-SYNC-13.

## Contexto

Os testes Playwright corriam contra `mock-bff.ts`, que não tem `SyncService`: provavam a app, mas nada do que o cartão F1-13 entregou (assinatura ES256 do WebCrypto verificada em Go, idempotência por `op_id`, RLS, OpenFGA, passe offline, cursor). O cartão F1-14 pede «criar setup offline, voltar a ligar, verificar no servidor».

## Decisão

1. **O E2E do sync corre contra o `pitbox-api` real** (`runAPI`), com PostgreSQL, Valkey e OpenFGA em contentores descartáveis, e uma app de produção servida por `vite preview` com proxy do mesmo modo que a produção (mesma origem).
2. **Só o fornecedor de identidade falta.** O servidor de teste é um **teste Go** com a build tag `e2e` (`backend/internal/bootstrap/e2e_server_test.go`, arrancado por `scripts/e2e-server.sh`): semeia dados sintéticos (equipa, dono, piloto, e uma equipa alheia) e cria as sessões no armazém de sessões, como o login faria. Nada no código de produção sabe dos testes: não há modo de autenticação desligada, nem endpoint que crie sessões, e o ficheiro não entra em nenhum binário.
3. **Porta de controlo só de leitura** (`127.0.0.1`, token aleatório por execução) com consultas fixas, sem SQL vindo de fora, para o teste perguntar «o que há no servidor?». Devolve identificadores e metadados, nunca tokens nem emails.
4. **Job `e2e-sync` obrigatório em todos os PR** (`ci-ok` depende dele). Os testes do mock-bff continuam em `pnpm e2e` (rápidos, sem Docker).
5. O cookie `__Host-` é injetado no contexto do browser (`domain: localhost`, `path: /`, `Secure`, `HttpOnly`); o Chromium não aceita um `__Host-` dado como URL `http`.

## Alternativas

| Alternativa | Motivo para rejeitar |
| --- | --- |
| Estender o `mock-bff` com sync | Verificaria o mock, não o servidor; duplicava a verificação de assinaturas. |
| Stack completa com Zitadel no CI | Fiel mas pesada e frágil (arranque, segredos, mais de 10 min); fica para um job noturno ou staging. O fluxo OIDC já tem o seu smoke (`make smoke-auth`). |
| Endpoint ou modo «login de teste» no `cmd/api` | Uma porta traseira no código de produção para servir testes. |
| Binário `cmd/e2e-api` | Compilável por engano para uma imagem; o teste Go com tag não tem esse risco. |

## Consequências

- O login real (redirecionamento, PKCE, Zitadel) **não** é exercitado aqui; continua coberto por `login.spec.ts` (mock), pelos testes de `platform/auth` e pelo `make smoke-auth`.
- O E2E tem de manter o `runAPI` e a semente alinhados com o resto do código: usa os construtores reais, por isso um módulo novo ligado em `runAPI` fica logo no E2E.
- O job precisa de Docker no runner (já existe) e demora cerca de 5 minutos.
- Os traces, só em falha e com 3 dias de retenção, contêm a sessão sintética de um ambiente que já não existe.
- O E2E encontrou um defeito real do F1-13: o motor pedia os conflitos em todos os ciclos, mas só dono e mecânico os podem listar, por isso o dispositivo de um piloto ficava em «Erro de sincronização». Corrigido neste cartão.
