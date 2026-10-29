# F1-09b — Plano de implementação: ecrãs de equipa

- **Issue:** [#139 — F1-09b: Ecrãs de equipa: criar, convidar e aceitar](https://github.com/NelsonDanielFreitas/pitbox/issues/139)
- **Estado:** Implementado; a aguardar teste manual e revisão do PR
- **Branch:** `f1-09b-ecras-de-equipa`
- **Base observada:** `f21ca3b` — F1-03e (#143)
- **Cartão:** F1-09b
- **Design:** [Figma Make — PitBox Design System](https://www.figma.com/make/j8YibTN5pp722o063koWgf/PitBox-Design-System), `src/screens/Auth.tsx` e `src/screens/Mais.tsx`; referência local em `docs/design/README.md`.
- **Spike Zitadel v4.19.5:** `prompt=login` numa conta só com password pede novamente login/password, não oferece configurar passkey/TOTP e termina no BFF com `stepup_refused` (403 simples). O self-service `/ui/console/users/me` abre a consola e tem “Senha e Segurança”, com configuração de passkey/TOTP.

## 1. Objetivo e âmbito

Dar a uma conta nova um caminho completo até à garagem:

1. criar uma equipa;
2. convidar outro utilizador por link e escolher o papel;
3. aceitar o convite com outra conta;
4. consultar e gerir membros e convites pendentes;
5. abrir a equipa e chegar à garagem.

Inclui a experiência para utilizadores sem fator forte, necessário para `CreateTeam`, e os estados offline/erro. Não inclui envio real de email (#124), transferência de propriedade, alteração wire aos contratos protobuf, ecrã próprio de segurança da conta, gestão de MFA/passkeys na app, códigos de recuperação, recusa de convite ou preview do convite.

## 2. Investigação do estado atual

### Backend e contratos

- `TeamService` já tem `CreateTeam`, `ListMyTeams`, `GetTeam` e `UpdateTeam`.
- `MemberService` já tem `ListMembers`, `ListInvitations`, `InviteMember`, `AcceptInvitation`, `ChangeMemberRole`, `RemoveMember`, `LeaveTeam` e `RevokeInvitation`.
- Os clientes protobuf/Connect gerados para os dois serviços já existem em `app/src/gen`; não devem ser editados à mão. Não antevejo alteração de `.proto`.
- A lógica de backend está em `backend/internal/identity`: idempotência e limite de 10 equipas, auditoria, outbox, `WithNewTeamTx`/`WithJoinTeamTx`, hash SHA-256 do token, expiração de 7 dias, uso único e erros genéricos de convite.
- Os testes de integração existentes cobrem ciclo de equipa, convites, papéis, último dono, idempotência, concorrência e recusa de utilizadores de outra equipa. A camada de UI deve reutilizar estes contratos; não duplicar as decisões de autorização.
- O mailer é `NoopMailer` até F5. Para F1-09b, a app cria apenas convites por link, sem pedir email nem indicar que foi enviado um email.
- `CreateTeam` já exige um fator forte no interceptor: `amr` com `user` (passkey) ou `otp` (TOTP). A verificação é do servidor e não tem janela de recência. `InviteMember`, lista/revogação de convites e gestão de membros são autorizados no backend pelo papel de dono.

### App e dados locais

- `TeamsRepository` atualmente só expõe `listMyTeams`; `ConnectTeamsRepository` fala com a API e `LocalTeamsRepository` lê a tabela local `team`.
- `DatabaseGate` chama `bootstrapTeams`; a UI lê a lista de equipas da base local.
- A tabela local só guarda os dados necessários da equipa e o papel do utilizador. Não há tabelas locais de membros ou convites.
- `teamBootstrapStatements()` também marca como apagadas equipas que não venham na resposta. Depois de criar/aceitar uma equipa, deve usar-se um refresh **completo** (`bootstrapTeams(true)`), não passar apenas a equipa nova para essa função.
- O BFF falso dos E2E (`app/tests/e2e/mock-bff.ts`) só implementa atualmente `ListMyTeams` e tem uma identidade fixa; será necessário alargá-lo para os fluxos e os dois utilizadores de teste.

## 3. Abordagem recomendada

### 3.1 Camada de dados

- Alargar `TeamsRepository` para criar equipas e acrescentar `MembersRepository` para listar membros/convites e executar as operações de convite/gestão.
- Manter Connect e SQLite dentro de `features/teams/data`/`sync`; componentes e rotas não importam o cliente Connect nem `@/db` diretamente.
- Após `CreateTeam` ou `AcceptInvitation` bem-sucedido: fazer refresh completo das equipas na base local e invalidar `teamsQueryKey`; só depois navegar para a equipa.
- Membros e convites são lidos online através do repositório e mantidos na cache de queries durante a sessão. Não acrescentar tabelas locais nesta fase.
- Gestão de equipas/convites não entra na fila offline: quando não há rede, mostrar a explicação e desativar as ações; continuar a tratar a falha real do RPC, porque `navigator.onLine` é apenas uma indicação.

### 3.2 Criar equipa e fator forte

- O fluxo começa em **Criar equipa** (`/teams/new`) e verifica o `amr` de `/auth/session` antes de mostrar o formulário.
- Se já houver `user` (passkey) ou `otp` (TOTP), mostrar o formulário. Caso contrário, mostrar “Protege a tua conta” e o botão “Ativar segundo fator”. O texto explica que o fator é necessário para criar a equipa; não reutilizar o texto do Figma que fala de convites/custos.
- O spike não encontrou configuração de fator durante `prompt=login`. O botão abre `/auth/account-security`, redirect BFF fixo para `${issuer}/ui/console/users/me`, sem destino fornecido pelo cliente. Depois, “Já ativei” inicia `/auth/login?step_up=true&return_to=/teams/new`.
- Se o callback do step-up ainda vier sem fator forte, o BFF responde 303 para `/teams/new?step_up=weak`, auditando `stepup_refused` e sem alterar sessão/cookie. A app mostra orientação, não um erro genérico.
- Fazer step-up antes do formulário evita persistir o rascunho da equipa em `localStorage`/`sessionStorage` durante o redirect.
- Formulário baseado no Figma: nome 1–80; categoria select (“1/8 TT Nitro”, “1/8 Buggy Nitro”, “1/8 Buggy Elétrico”, “1/8 TT Elétrico”), até 40 caracteres; clube opcional até 80; país `PT` fixo/oculto na v0.1. Categoria vazia envia vazio para o servidor aplicar o default.
- `request_id` UUIDv7; botão bloqueado durante o pedido. Erro de limite (10 equipas) tem mensagem própria. O servidor continua a ser a autoridade para fator, limite e idempotência.

### 3.3 Membros e convites

- Acrescentar “Mais” ao `TeamTabBar`, com “Equipa” em `/teams/$teamId/more/team`. Esta é a área de gestão; guest não vê a entrada.
- Lista de membros: avatar de iniciais, nome, etiqueta “Tu”, `RoleBadge`, ordenada owner → mechanic → pilot → guest. `Member` não inclui email; mostrar “Na equipa desde {joined_at}”. Owner, mechanic e pilot podem listar; guest não chama `ListMembers` e recebe `NOT_FOUND`.
- Só o dono vê convidar, alterar papel, remover membros e convites pendentes. Papéis de convite: mechanic, pilot e guest; nunca owner. Sem ações no próprio dono nem transferência de propriedade.
- Folha “Convidar para a equipa” com `RolePicker`, descrições `ROLE_INFO`, validade de sete dias e aviso “Quem tiver o link pode entrar na equipa”. Só link; email fica para #124.
- O token plaintext não aparece em texto completo no ecrã. “Copiar” e “Partilhar” (Web Share API com fallback para clipboard) são ações explícitas; limpar o token da memória ao fechar a folha. `InviteMember` não é idempotente: sem retry automático; timeout ambíguo exige recarregar convites e, se necessário, revogar/criar outro.
- Convites pendentes só são listados pelo dono; linhas mostram papel, validade e “Cancelar”, sem token. Alterar papel/remover usa o `version` recebido; `ABORTED`/`FAILED_PRECONDITION` recarrega os membros e explica que alguém alterou o estado, sem retry automático.
- Reutilizar `RoleBadge` existente em `shared/ui/Badge.tsx`; criar `Sheet`/`Dialog` acessíveis em `shared/ui` e avaliar a migração de `PickCarSheet`/`RetireDialog` como parte deste cartão.

### 3.4 Aceitar convite

- Link: `/invite#t=<token>` conforme o contrato protobuf; o path `/invite/$token` da descrição original é rejeitado porque envia o token no pedido HTTP. Corrigir apenas o comentário do proto para usar `/invite`; sem mudança wire.
- A rota pública lê `#t=` antes do router processar os guards, valida com Zod (`^[A-Za-z0-9_-]{43}$`) e faz `history.replaceState(null, "", "/invite")` antes de qualquer RPC. Não guardar o token em router state, query cache, storage, logs, traces ou Sentry.
- Se não houver sessão, mostrar “Entra ou cria conta e volta a abrir o link”, com `Entrar` (`return_to=/invite`) sem token. O utilizador reabre o link original depois de autenticar; nenhuma cópia server-side do token.
- Depois de aceitar, mostrar equipa, categoria e papel devolvido pelo servidor; `Abrir a equipa` faz refresh completo local e navega para essa equipa.
- “Tenho um convite” na lista de equipas abre `/invite` com campo para colar o link completo. Extrair o fragmento em memória, limpar o campo e aplicar o mesmo fluxo; aceitar apenas o formato e a origem esperados.
- Antes de aceitar, mostrar cartão genérico e “Aceitar”/“Agora não”. Não há RPC de preview nem de recusa; não revelar equipa ou quem convidou antes de aceitar. Depois, mostrar equipa e `RoleBadge` do papel recebido.
- `NOT_FOUND` de convite inexistente, expirado, revogado, usado, adulterado ou email incompatível mostra sempre a mesma mensagem pública.

### 3.5 Fator forte e self-service do Zitadel

- Spike concluído em Zitadel v4.19.5: password-only com `prompt=login` volta ao BFF como `stepup_refused`; não há enrollment prompt. Self-service `/ui/console/users/me` mostra “Senha e Segurança”, com “Adicionar método” de passkey e “Adicionar Fator”.
- Adicionar `GET /auth/account-security` no BFF. O handler constrói o destino apenas a partir do issuer configurado e do path constante `/ui/console/users/me`; sem query/redirect URL do cliente. Resposta `303`, `no-store`, `Referrer-Policy: no-referrer`.
- O botão abre o self-service e regressa à app; “Já ativei” faz o step-up existente com `return_to=/teams/new`. A sessão só muda no callback BFF depois de validar o mesmo `sub` e fator forte.

### 3.6 Componentes e estilo

- Reutilizar `Button`, `Badge`, `Card`, `Alert`, `FormField`, `TextInput`, `SelectInput`, `PageHeader`, `EmptyState` e `TeamTabBar`.
- Criar `Sheet` e `Dialog` partilhados, acessíveis por teclado e leitor de ecrã, com foco controlado e fecho previsível. `RoleBadge` já existe; reutilizar e ajustar tons apenas se necessário para coincidir com o design. A migração dos dialogs da garagem fica como follow-up se alargar o cartão.
- O Figma mistura “você”; a app usa “tu”. Textos em pt-PT e inglês.
- Alvos de toque ≥44 px, WCAG 2.2 AA, axe sem erros graves. Capturas 390×844 no PR.

### 3.7 Diferenças deliberadas em relação ao Figma

| Figma | F1-09b | Motivo |
| --- | --- | --- |
| Criar equipa antes de pedir o segundo fator | Fator forte → formulário/equipa | O servidor exige fator forte antes de `CreateTeam` (F1-03c). |
| QR TOTP e códigos de recuperação no fluxo | Setup passkey/TOTP no Zitadel; códigos de recuperação fora deste cartão | O IdP gere os fatores; códigos da app são F1-03c. |
| Preview de equipa/quem convidou antes de aceitar | Cartão genérico e aceite por token | Não existe RPC de preview; não ampliar o contrato. |
| “Recusar” convite | “Agora não” só sai do fluxo | Não existe operação de recusa no backend. |
| Convite por email | Só link | Não há mailer real; a app não deve fingir que enviou email. |
| Email dos membros | Data de entrada | `Member` não devolve email por minimização. |
| Convites pendentes vistos por mecânicos/pilotos | Só o dono | `ListInvitations` é owner-only. |
| Transferir propriedade | Não aparece | Fora do âmbito da UI F1-09b. |
| “Você” no Figma | “Tu” | Coerência com a app pt-PT. |

## 4. Segurança e riscos

| Área | Controlo proposto |
| --- | --- |
| Token de convite | Seguir o comentário do proto: `#t=<token>` no fragmento, não `/invite/$token` no path. Ler e limpar com `history.replaceState` antes de qualquer chamada; token apenas em memória até ao RPC/cópia; sem logs, traces, Sentry, analytics, storage ou `return_to`. |
| Referrer | A app envia `Referrer-Policy: same-origin`. `no-referrer` global foi rejeitado: o browser passa a enviar `Origin: null` nos POST (logout) e o BFF recusa-os. O fragmento nunca vai no `Referer`; o BFF mantém `no-referrer` nas suas respostas. |
| Link partilhável | É uma capacidade bearer: informar que quem receber o link o pode aceitar primeiro. Expiração de 7 dias, hash SHA-256 na BD e uso único continuam impostos pelo servidor. |
| Reautenticação | `CreateTeam` é recusado pelo servidor sem `amr=user` ou `otp`; a app apenas antecipa e explica o step-up. Não persistir o formulário para contornar o passo de autenticação. |
| Autorização | Papel controla só a visibilidade dos controlos; `authz.Check`, papel da `membership` e RLS continuam a ser as decisões reais. Não enviar identidade do utilizador em pedidos. |
| PII | `Member` não devolve email; `ListInvitations` é só para dono porque pode devolver emails. A UI F1-09b não cria convites por email. |
| Repetições | `request_id` UUIDv7 em escritas; `InviteMember` não é idempotente, logo sem retry automático e com botão desativado durante o envio. |
| Offline | Gestão de equipa é online-only, sem fila de sync. Não fazer escrita otimista local antes de o servidor confirmar. |
| Dados locais | Atualizar a lista de equipas apenas após sucesso, com refresh completo; não guardar token, convites ou emails na base local. |
| Erros | Convite inválido/expirado/revogado/usado dá a mesma mensagem; limite de 10 equipas e step-up recebem explicações específicas sem detalhes internos. |
| Step-up recusado | BFF responde 303 para `/teams/new?step_up=weak`; a sessão existente não é alterada; `stepup_refused` continua auditado. |

### Spike de step-up (2026-10-28)

- Conta de teste sem fator adicional: `/auth/login?step_up=true` com `prompt=login` voltou ao callback com `amr=pwd`; o Zitadel não mostrou um passo de setup de passkey/TOTP e o BFF respondeu 403 `login not allowed` (`stepup_refused`).
- A rota local `/ui/console/users/me` abriu a consola autenticada. “Senha e Segurança” mostra “Adicionar método” (passkey) e “Adicionar Fator” (TOTP).
- Decisão F1-09b: o BFF redireciona recusas por fator fraco para `/teams/new?step_up=weak`; a app oferece redirect fixo `GET /auth/account-security` para o self-service e, depois da configuração, inicia novamente o step-up. O servidor continua a exigir o fator.

### Riscos/decisões arquiteturais

1. A regra atual de app local-first é excecionada para a gestão de equipas e convites, conforme a Issue #139. ADR 0020 aceite e arquitetura/ADR 0018 atualizados; o alcance offline do modo de prova não muda.
2. O contrato protobuf especifica fragmento (`/invite#t=...`), nunca token no path ou query; a rota lê e limpa o fragmento antes do router/RPC.
3. Sem persistência do token, um utilizador não autenticado deverá reabrir o link depois de entrar. Um fluxo que retome automaticamente o convite após login exigiria um handoff servidor-side; não guardar o token em `sessionStorage` para o conseguir.
4. Um convite link pode ser aceite por qualquer conta autenticada que obtenha o link (sem vínculo a email). Isto é inerente ao modelo de link bearer, limitado por uso único e expiração; a UI deve comunicar essa propriedade ao dono.

## 5. Ficheiros/áreas previstos

- `app/src/features/teams/data/`: ampliar `TeamsRepository`; novo repositório de membros/convites e cliente Connect com testes.
- `app/src/features/teams/domain/`: schemas Zod, limites, papéis e helpers puros para permissões visuais.
- `app/src/features/teams/ui/`: criação de equipa, gestão de membros/convites, aceite de convite, estados offline/erro e confirmações acessíveis.
- `app/src/shared/ui/Sheet.tsx`, `Dialog.tsx`: superfícies acessíveis partilhadas; reutilizar `RoleBadge` existente em `Badge.tsx`.
- `app/src/app/routes/`: `/teams/new`, `/teams/$teamId/more`, `/teams/$teamId/more/team` e `/invite` (fragmento); gerar `routeTree.gen.ts` com `pnpm routes:gen`, sem edição manual.
- `app/src/sync/teams.ts`: refresh completo da lista local após criar/aceitar; sem fila offline para estas ações.
- `app/tests/e2e/mock-bff.ts` e `app/tests/e2e/`: mock de TeamService/MemberService e cenários com dois utilizadores, fator forte e token no fragmento.
- `app/src/i18n/resources.pt-PT.ts` e `resources.en.ts`: texto pt-PT, i18n para inglês.
- `backend/internal/platform/auth/flow.go`/`flow_test.go`: `/auth/account-security` e redirect 303 de step-up fraco, mantendo a sessão intacta.
- `proto/pitbox/identity/v1/member_service.proto`: corrigir o comentário do link para `/invite#t=`; regenerar clientes com `make gen`.
- `backend/internal/identity/app/invitation.go`: corrigir comentário desatualizado de `ListInvitations` (owner-only).
- `docs/adr/0020-gestao-online-equipas-convites.md`, arquitetura, ADR 0018, threat model e `docs/design/README.md`.

Não se prevê alteração ao protobuf, migrations PostgreSQL ou SQL gerado. Se a implementação demonstrar que é necessário mudar o contrato ou outra decisão estrutural, paro para propor a alteração antes de a fazer.

## 6. Testes e critérios de aceitação

- [ ] Conta nova autenticada cria equipa e chega à garagem; a equipa aparece na lista local.
- [ ] Sessão só com password recebe explicação; passkey/TOTP → step-up → formulário → criação autorizada. Pedido direto sem fator forte continua recusado pelo servidor.
- [ ] Spike comprovado: login v2 v4.19.5 não configura fator durante `prompt=login` sem MFA; self-service fornece o setup e o callback fraco regressa à app por 303.
- [x] Segundo utilizador autenticado aceita convite por link e aparece com o papel atribuído.
- [x] Token só fica em `#t=...` antes de ser lido; depois desaparece do URL/histórico antes do RPC; não aparece em logs, queries, storage ou traces.
- [x] Convite expirado, revogado, usado, adulterado ou incompatível mostra erro genérico; papéis e limites continuam a ser aplicados pelo backend.
- [x] Dono vê e consegue convidar, mudar papel, remover membros e revogar convites; piloto/mecânico não vê essas ações e o servidor recusa chamadas forjadas.
- [x] Sem rede, criar/convidar/aceitar/gerir fica desativado com explicação e não cria operação local pendente.
- [x] Depois de cada escrita bem-sucedida, as queries e a lista local refletem o estado do servidor sem apagar outras equipas locais.
- [ ] Spike de Zitadel e redirect para self-service documentados; step-up fraco termina em 303 para `/teams/new?step_up=weak`, sem alterar sessão/cookie.
- [x] “Tenho um convite”: colar link válido aceita; texto inválido mostra erro genérico; token nunca aparece na URL após leitura.
- [x] Listas paginam até ao fim com limite explícito; conflito de `version` recarrega os membros sem retry automático.
- [ ] Capturas 390×844 do fluxo de equipas/convites no PR.
- [ ] axe sem erros graves; `pnpm lint`, `pnpm typecheck`, `pnpm test`, `pnpm e2e`, `make lint` e `make test` verdes.
- [ ] Testes backend de identidade continuam a passar (`go test -race ./internal/identity/...`); testes de autorização existentes continuam verdes.

## 7. Ordem de implementação

1. `docs(adr): add ADR 0020 online-only team management` + referência do Figma em `docs/design/README.md`.
2. `feat(auth): redirect weak step-up back to the app` + `/auth/account-security` (necessário pelo spike).
3. `feat(ui): add Sheet and Dialog` (reutilizar `RoleBadge` existente).
4. `feat(teams): create team with strong-factor guidance`.
5. `feat(teams): members and link invitations`.
6. `feat(teams): accept invitation by link`.
7. `docs: architecture, threat model, proto and service comments`.

## 8. Confirmações

- [x] **Token:** usar `/invite#t=<token>`; corrigir o comentário do proto.
- [x] **Offline-first:** aprovar exceção online-only e ADR 0020.
- [x] **Convite sem sessão:** pedir login/registo e reabrir o link; não guardar token no browser.
- [x] **Fator forte:** spike confirmou ausência de setup automático; usar redirect BFF fixo para self-service e step-up existente.

💼 **Portfólio:** fluxo de convite bearer tratado ponta a ponta, combinado com autorização por papéis, step-up, RLS e uma exceção offline documentada.
