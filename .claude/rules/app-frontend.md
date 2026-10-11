---
paths:
  - "app/**/*"
---

# Regras da app (React + TypeScript + Capacitor)

## TypeScript
- `strict: true`, `noUncheckedIndexedAccess: true`. Proibido `any` e `as` para silenciar erros; usar `unknown` + validação.
- Validação com Zod em todas as fronteiras: formulários, respostas da API, leitura da base local, dados do BLE.
- Funções puras para regras (estratégia de box, cálculos de setup) em `src/features/<f>/domain/`, com testes; `fast-check` para propriedades.

## Organização
- `src/features/<funcionalidade>/{domain,data,ui}`; `src/shared/` para componentes e utilitários comuns; `src/sync/` para a fila e o protocolo; `src/db/` para a base local; `src/gen/` gerado pelo Buf (não editar).
- A interface lê sempre da base local (repositórios em `data/`). Só o motor de sync fala com a API.
- Estado de servidor com TanStack Query; estado de fluxo complexo (modo final) com XState; sem estado global desnecessário.

## Offline e sync
- IDs gerados no cliente com UUIDv7. Cada escrita cria uma operação na fila com `op_id`, assinada com a chave do dispositivo (ECDSA P-256, não exportável).
- Nada no caminho crítico de um toque do modo final pode depender da rede.
- Testar com Playwright em modo avião.

## Segurança
- Tokens: na web só via BFF com cookie HttpOnly; no Capacitor no Keychain/Keystore. Nunca em `localStorage`, `sessionStorage` ou IndexedDB em claro.
- Base local cifrada no nativo (SQLCipher); apagar dados locais no logout depois de sincronizados.
- CSP estrita, sem `eval` nem scripts inline; sem `dangerouslySetInnerHTML`.
- Deep links e intents validados; nunca confiar em parâmetros de URL para autorização.
- Login móvel com AppAuth (browser do sistema + PKCE), nunca WebView embutida.

## Interface
- WCAG 2.2 AA. Modo final: alvos de toque ≥ 64 px, contraste ≥ 7:1, ecrã sempre ligado, legível ao sol, utilizável com luvas.
- Texto da interface em português de Portugal, com i18n preparado para inglês; unidades sempre visíveis (cSt, mm, °, °C).

## Testes e qualidade
- Vitest + Testing Library para componentes; Playwright para E2E.
- `pnpm lint`, `pnpm typecheck` e `pnpm test` limpos antes de dar uma tarefa por concluída.
