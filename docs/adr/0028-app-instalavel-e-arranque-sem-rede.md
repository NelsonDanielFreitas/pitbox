# 0028 — App instalável e arranque sem rede

- **Estado:** Proposto
- **Data:** 2026-10-31
- **Cartão:** F1-14b (novo; pré-condição do F1-15)
- **Relacionado:** [ADR 0003](0003-app-offline-first-sync-proprio.md), [ADR 0013](0013-sessoes-bff-e-tokens.md), [ADR 0017](0017-app-web-bff-mesma-origem.md), [ADR 0018](0018-base-local-web.md) (altera uma regra), [ADR 0025](0025-formato-de-assinatura-e-cursor-do-sync.md); arquitetura §3 e §5.2; ameaças TM-DEV-01, TM-DEV-02.

## Contexto

O gate F1-15 exige usar a app numa pista sem rede. Hoje a app funciona com rede e continua a funcionar se perder a rede depois de aberta, mas **não abre se for aberta já sem rede**:

1. **Não há service worker.** Sem rede, o browser não tem o HTML, o JS nem o WASM do wa-sqlite. A arquitetura (§3) e a tabela da Fase 1 já previam `vite-plugin-pwa` (Workbox), mas nenhum cartão ficou com ele.
2. **O arranque depende de `/auth/session`.** `_authenticated/route.tsx` chama `fetchSession` no `beforeLoad`; sem rede, o `fetch` rejeita e a rota cai no `RootError`. O `DatabaseGate` só abre a base com o `user_id` dessa resposta, e o ADR 0018 diz que o «`user_id` nunca vem de URL ou storage». O ADR 0018 deixou isto em aberto: «abrir a app sem rede leva ao login até ao passe offline do F1-13».

O passe offline existe desde o F1-13: está na base local de cada utilizador (`app_meta`, chave `sync.pass.<team_id>`), dura 72 h e o motor de sync renova-o em cada ciclo com rede quando faltam menos de 24 h.

## Decisão

### 1. Service worker só para o shell

- `vite-plugin-pwa` 2.x em `generateSW`, com `registerType: "prompt"`.
- **Pré-cache** só dos ficheiros do build: HTML, JS, CSS, fontes, ícones e o WASM do wa-sqlite.
- **Sem `runtimeCaching`.** `/auth/*` e `/pitbox.*` nunca passam pela Cache Storage; `navigateFallbackDenylist` cobre `/auth/` e `/pitbox.`. Nenhuma resposta autenticada nem token em cache.
- **Atualização com aviso:** a versão nova fica em `waiting` e a app mostra «Há uma versão nova — atualizar». Só ativa quando o utilizador toca e nunca com escritas ou envios a meio. Sem `skipWaiting` silencioso, porque um shell novo pode trazer uma migração local `000N`.
- `manifest.webmanifest` (`display: "standalone"`, ícones 192/512 e maskable) em `app/public/`. A CSP já tem `worker-src 'self'` e `manifest-src 'self'`: não muda.
- Depois do primeiro login, `navigator.storage.persist()`; se for recusado, aviso discreto (a OPFS pode ser apagada sob pressão de espaço).

### 2. Arranque sem rede

| Resposta de `/auth/session` | Comportamento |
| --- | --- |
| `200` autenticado | Como hoje. Guarda o «último utilizador». |
| `401` | Login, como hoje (fail closed). A base local **não** é apagada (ADR 0018). |
| `403`, `503`, contrato inválido | Como hoje: ecrã de erro. O servidor respondeu, por isso não é falta de rede. |
| **Erro de rede** (o `fetch` rejeita, sem ser `AbortError`) | **Sessão offline**, se as condições abaixo se cumprirem. |

**Sessão offline:**

- O «último utilizador» é **só o `user_id`** (UUID), numa chave `localStorage` própria, validado com Zod e escrito depois de cada `200`. Nunca tokens, emails nem papéis. Apaga-se no logout explícito e em `401`.
- A app abre a base desse utilizador e procura em `app_meta` um passe offline com `expiresAt` no futuro para **alguma** equipa. Sem passe válido, fecha a base e mostra «Sem rede. Liga-te para continuar», sem dados.
- A interface indica a sessão offline. Os ecrãs online-only (ADR 0020) continuam a recusar sem rede.
- Quando a rede volta, a app repete `/auth/session`: mesmo utilizador, segue; outro utilizador, regra do ADR 0018 (`queryClient.clear()` e outra base); `401`, login sem apagar a fila nem a base.
- O passe local serve **só para decidir se a base abre**. A app não o verifica criptograficamente e o relógio do telemóvel pode estar errado. Quem decide é o servidor, que verifica o passe e a assinatura de cada operação no `Push` (ADR 0025).

### Alteração ao ADR 0018

«`user_id` nunca vem de URL ou storage» passa a «`user_id` nunca vem da URL; do armazenamento local só para a sessão offline do ADR 0028, e só com um passe offline não expirado». O resto mantém-se.

## Alternativas

| Alternativa | Motivo para rejeitar |
| --- | --- |
| Só o service worker, sem arranque offline | O shell carrega mas a rota autenticada rebenta: não cumpre o gate. |
| Guardar a resposta de `/auth/session` em cache | Resposta autenticada na Cache Storage, reutilizada sem limite de tempo. |
| Abrir a base do último utilizador sem exigir passe | Sem limite temporal: um telemóvel perdido mostrava os dados para sempre. |
| `user_id` num ficheiro na OPFS | Equivalente em segurança (mesma origem) e mais complexo de ler antes de abrir o worker. |
| `skipWaiting` + `clientsClaim` | Pode trocar o código a meio de uma escrita ou migração local. |
| Esperar pelo Capacitor (F2-06) | O gate da v0.1 passaria a validar outro stack (SQLite nativo, AppAuth). |

## Consequências

- **TM-DEV-01:** com o telemóvel desbloqueado, os dados locais abrem até 72 h depois do último sync, sem rede e sem login. No browser a base não está cifrada (SQLCipher só no F2-06). Mitigações: bloqueio do sistema operativo, validade do passe e revogação do dispositivo, que impede o `Push` mas não apaga os dados no telemóvel.
- **Nova ameaça (service worker):** shell antigo ou envenenado. Mitigações: só pré-cache com hash, sem `runtimeCaching`, mesma origem, CSP inalterada e atualização com aviso.
- **Testes:**
  1. Reabrir a frio offline mostra os dados.
  2. Sem passe válido não mostra dados.
  3. `401` leva ao login sem apagar a base.
  4. `/auth/*` e `/pitbox.*` fora da Cache Storage.
  5. Uma versão nova não ativa sozinha.
  6. O logout limpa o último utilizador.
  7. Um `user_id` adulterado não abre dados.
- Novas dependências de desenvolvimento: `vite-plugin-pwa`, `workbox-build` e `workbox-window`. São justificadas porque a arquitetura já as prevê. `pnpm audit` limpo.
- Atualizar arquitetura §3, `docs/threat-model/ameacas.md`, `app/README.md`, o plano e o `cartoes.json`.
