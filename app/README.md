# app

App única para web, Android e iOS: React + Vite + TypeScript + Capacitor. Criada no cartão
F1-09; as pastas `android/` e `ios/` são geradas pelo Capacitor (F2-06).

| Pasta | Conteúdo |
| --- | --- |
| `src/app/` | Arranque, rotas (TanStack Router), providers e layout |
| `src/features/<f>/` | Uma pasta por funcionalidade: `domain/` (regras puras), `data/` (repositórios), `ui/` (ecrãs e componentes) |
| `src/shared/` | Componentes de UI, hooks e utilitários comuns |
| `src/db/` | SQLite local em Worker (`OPFSCoopSyncVFS`), migrações, cliente tipado e ciclo de vida por utilizador |
| `src/sync/` | Fila de operações, assinatura por dispositivo, Push/Pull (F1-13) |
| `src/auth/` | Sessão BFF na web, AppAuth no nativo |
| `src/i18n/` | Textos pt-PT e en |
| `src/gen/` | Cliente gerado pelo Buf (não editar) |
| `public/` | Ícones e manifest PWA |
| `tests/e2e/` | Playwright, incluindo o BFF stub determinístico |

## Desenvolvimento

```bash
make up
make db-migrate                 # atualiza o schema local de um volume já existente
make zitadel-provision          # regista http://localhost:8081 e :5173 em dev
make openfga-provision          # store/modelo e PITBOX_OPENFGA_* no .env
PITBOX_PUBLIC_URL=http://localhost:5173 make run-api
make app-dev                    # http://localhost:5173
```

O browser tem de usar **`localhost`**, não `127.0.0.1`, para o `Origin` coincidir com
`PITBOX_PUBLIC_URL`. O Vite escuta em `127.0.0.1` e encaminha `/auth` e `/pitbox.` para a API
local; em desenvolvimento, se abrires `127.0.0.1:5173`, a app redirecciona para
`localhost:5173` (o cookie `__Host-pitbox_login` é por host e o callback do Zitadel usa
`localhost`). Em produção, a app e o BFF partilham a mesma origem (ADR 0017).

Um login tem de ser concluído em **30 minutos** (`LoginCookieMaxAge`), o mesmo prazo do
código do email. Mesmo assim, não reutilizes separadores antigos: o pedido de autorização do
Zitadel e o state do BFF têm de ser os do mesmo fluxo.

A recuperação de palavra-passe termina com uma password nova; se o regresso ao callback do
BFF não tiver o state do login (por exemplo, link aberto noutro browser ou pedido expirado),
isso não desfaz a alteração. Volta a `http://localhost:5173/login` e entra com a password nova.

O registo é feito no login do Zitadel. Os emails locais de verificação e recuperação ficam no
Mailpit (`http://127.0.0.1:8025`, credenciais `MAILPIT_UI_*` no `.env`). Se o email ainda não
estiver verificado, o BFF envia para `/confirm-email`; a página não tem sessão nem dados
pessoais e permite voltar ao login.

O cookie `__Host-pitbox_session` é `Secure`; Chrome, Edge e Firefox aceitam-no em
`http://localhost`. O Safari não é suportado em HTTP local; o Capacitor usa `https://localhost`
no F2-06.

## Testes e verificação

```bash
cd app
pnpm lint
pnpm typecheck
pnpm test
pnpm build
pnpm e2e          # precisa do Chromium: pnpm exec playwright install --with-deps chromium
```

`pnpm e2e` usa `vite build`/`vite preview` e um BFF stub em `tests/e2e/mock-bff.ts`; não precisa
de Docker nem Zitadel. O fluxo real com Zitadel é manual, com os comandos acima.

O E2E verifica que `document.cookie` não contém a sessão, que `localStorage`/`sessionStorage`/
IndexedDB não contêm tokens, que o CSP de produção está ativo e que o ecrã de equipas passa
axe sem violações graves/críticas.

## Segurança

- Nenhum token no JavaScript: a sessão é um cookie `HttpOnly` gerido pelo BFF.
- Os ecrãs nunca importam `@connectrpc/*` nem `src/gen/**/*_service_pb`; só `data/` fala com a
  API. O ESLint verifica esta fronteira.
- `localStorage`/`sessionStorage` só são permitidos em `src/i18n/` (preferência de idioma) e,
  para o «último utilizador» do ADR 0028 (só o `user_id`), em `src/auth/last-user.ts`;
  `navigator.storage` só em `src/db/`.
- `dangerouslySetInnerHTML`, `eval` e `new Function` estão proibidos.
- CSP de produção: `script-src 'self' 'wasm-unsafe-eval'`, `worker-src 'self'`, sem inline
  scripts nem `unsafe-eval`. Em dev, o Vite usa um nonce fixo para o React Refresh.
- A base local usa `wa-sqlite` num Worker, com `OPFSCoopSyncVFS`; é uma base por utilizador
  em `pitbox/<sha256(user_id)>/db.sqlite3`. Os dados são apagados no logout. A matriz de
  browsers da especificação §14 (Chrome, Safari e Firefox) tem de ser validada; sem OPFS a
  app falha com mensagem explícita, sem fallback para memória.

## Garagem (F1-11)

- Os ecrãs de garagem vivem em `src/features/garage/ui` e leem/escrevem só no repositório
  local (`data/local-garage-repository.ts`). Não fazem pedidos à API.
- A navegação usa uma TabBar da equipa (Início/Garagem) e um `Segmented`
  Carros/Motores/Pneus/Peças.
- Stock em centi (`min_stock_centi`, `qty_delta_centi`), alinhado com o proto. O F1-11 só cria
  o movimento `purchase` inicial; o resto do inventário é F1-11b.
- `version` é a versão do servidor; `local_rev` é a concorrência entre abas.
- Piloto lê; só Dono/Mecânico vê ações de escrita. O servidor continua a ser a barreira real.
- Sem rede, a garagem funciona dentro de uma sessão já autenticada. Abrir a app a frio sem rede
  funciona com a sessão offline do F1-14b (secção seguinte).

## Instalar no telemóvel e abrir sem rede (F1-14b, ADR 0028)

O service worker só existe no build: em `pnpm dev` não há service worker. Para o testar:

```bash
pnpm build && pnpm preview     # http://localhost:4173 (com a API/BFF local, ver «Desenvolvimento»)
```

1. Abre `http://localhost:4173`, entra e deixa a app sincronizar uma vez (é isso que guarda o
   passe offline de 72 h na base local).
2. Instala: no Chrome, menu → «Instalar app» (no Android, «Adicionar ao ecrã inicial»).
   Em DevTools → Application → Manifest/Service Workers confirma «activated and is running».
3. DevTools → Network → **Offline**, fecha a app e volta a abri-la: mostra os dados locais com
   o aviso «Sessão offline». Sem passe válido (expirado ou nunca sincronizado) mostra «Sem
   rede. Liga-te para continuar.» e nenhum dado.
4. Quando a rede volta a app pede `/auth/session` outra vez: mesmo utilizador segue; outro
   utilizador começa limpo; 401 vai para o login sem apagar a base nem a fila.

Notas:

- O service worker faz pré-cache só dos ficheiros do build (incluindo o WASM do wa-sqlite).
  `/auth/*` e `/pitbox.*` nunca ficam em cache. Uma versão nova mostra «Há uma versão nova —
  atualizar» e só ativa quando tocas (e não durante um envio).
- O «último utilizador» é só o `user_id` numa chave `localStorage`; apaga-se no logout e em 401.
  O passe local só decide se a base abre: o servidor verifica o passe e as assinaturas no `Push`.
- Com o telemóvel desbloqueado os dados locais abrem até 72 h depois do último sync (TM-DEV-01);
  a cifra nativa chega no F2-06.
- Instalabilidade: o Lighthouse 12 já não tem a categoria PWA. Verifica no DevTools →
  Application e com `pnpm e2e` (`tests/e2e/offline-start.spec.ts`).
