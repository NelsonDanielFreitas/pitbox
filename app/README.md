# app

App única para web, Android e iOS: React + Vite + TypeScript + Capacitor. Criada no cartão F1-09; as pastas `android/` e `ios/` são geradas pelo Capacitor (F2-06).

| Pasta | Conteúdo |
| --- | --- |
| `src/app/` | Arranque, rotas (TanStack Router), providers, layout |
| `src/features/<f>/` | Uma pasta por funcionalidade: `domain/` (regras puras), `data/` (repositórios na base local), `ui/` (ecrãs e componentes) |
| `src/shared/` | Componentes de UI, hooks e utilitários comuns |
| `src/db/` | SQLite local: ligação, migrações locais |
| `src/sync/` | Fila de operações, assinatura por dispositivo, Push/Pull |
| `src/auth/` | Sessão (BFF na web, AppAuth no nativo) |
| `src/i18n/` | Textos pt-PT e en |
| `src/gen/` | Cliente gerado pelo Buf (não editar) |
| `public/` | Ícones, manifest PWA |
| `tests/e2e/` | Testes Playwright (incluindo modo avião) |

Regras em `.claude/rules/app-frontend.md`.
