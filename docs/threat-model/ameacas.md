# Ameaças (STRIDE por componente)

Legenda e método em [README.md](README.md). Colunas: **S**poofing, **T**ampering, **R**epudiation, **I**nformation disclosure, **D**enial of service, **E**levation of privilege. Prioridade: A, M, B.

Um identificador (`TM-...`) é estável: testes e cartões futuros referem-se a ele.


## Borda e API (DF-01, DF-02)

Elementos: Cloudflare, `pitbox-api`, BFF.

| ID | STRIDE | Ameaça | Mitigação | Estado | Prova ou cartão | Prio |
| --- | --- | --- | --- | --- | --- | --- |
| TM-API-01 | D | Corpos gigantes, bombas de compressão, corpos enviados devagar e prazos sem fim | 4 MiB por mensagem nos dois sentidos (também depois de descomprimir); deadline de 30 s nos unários e 55 min nos streams; corpo cortado ao fim do teto | implementado | `backend/internal/bootstrap/rpc_test.go` (`TestMessageSizeLimit`, `TestASlowBodyIsCutAtTheCeiling`, `TestDeadlines`) | A |
| TM-API-01b | D | Flood de pedidos | Rate limiting por utilizador e equipa na API (F1-04b); por IP e WAF na borda (F5-05) | implementado | `backend/internal/platform/ratelimit/*_test.go`; `backend/internal/platform/interceptors/ratelimit_test.go`; ADR 0014 | A |
| TM-API-02 | T | Entrada malformada ou fora de limites chega ao domínio | Regras protovalidate em todo o pedido, com o validador compilado no arranque e sem modo lazy (um tipo não listado é recusado, não ignorado); o serviço nem corre se a validação falha; o valor inválido não volta na resposta | implementado | `backend/internal/bootstrap/rpc_test.go` (`TestValidationStopsInvalidRequestsAndEchoesNothing`, `TestRequestMessagesCoverEveryService`); `backend/internal/platform/interceptors/chain_test.go` | A |
| TM-API-03 | T | Pedido de escrita repetido (retry, duplo clique) duplica dados | `request_id` idempotente, 7 dias, chave `(user_id, request_id)`: outro utilizador pode usar o mesmo `request_id` sem colidir nem ver a linha alheia | implementado | `backend/db/tests/rls_and_constraints_test.sql` (idempotência); uso no servidor: F1-01 | M |
| TM-API-04 | I | Mensagens de erro ou respostas revelam detalhes internos ou campos a mais | Só um `*connect.Error` escrito de propósito chega ao cliente; qualquer outro erro (e qualquer panic) vira `Internal` com a mensagem fixa; a causa só vai para o log, e só nas falhas do servidor; `internal/archtest` só admite mensagens fixas em `connect.NewError` | implementado | `backend/internal/bootstrap/rpc_test.go` (`TestInternalErrorsNeverReachTheClient`, `TestAPanicIsContainedAndTheServerKeepsServing`); `backend/internal/archtest/archtest_test.go` | M |
| TM-API-05 | I | Reflection e documentação expõem a superfície da API | Reflection só com `PITBOX_ENV=local`; não montada em preview, staging nem produção | implementado | `backend/internal/bootstrap/rpc_test.go` (`TestReflectionIsLocalOnly`) | B |
| TM-API-08 | I | `traceparent` do cliente força a amostragem ou escolhe os nossos `trace_id` | `otelhttp` e `otelconnect` tratam a origem como não confiável (o do cliente fica como link); RPCs com um só span | implementado | `backend/internal/bootstrap/rpc_test.go` (`TestRPCsGetOneServerSpanAndDoNotTrustTheCaller`); `backend/internal/platform/httpserver/server_test.go` (`TestTracing`) | M |
| TM-API-09 | R | Escritas sem rasto (quem, de onde, que trace) | `AuditMeta` (trace id, IP do peer, user agent sanitizado) em todas as escritas; um RPC sem `idempotency_level = NO_SIDE_EFFECTS` conta como escrita; cabeçalhos de IP só com a origem protegida (F5-05) | implementado | `backend/internal/platform/interceptors/audit_test.go`; `backend/internal/bootstrap/rpc_test.go` (`TestAuditMetadataForWrites`, `TestReadsAreNotAudited`). `audit.RecordTeam` escreve a linha em `audit.audit_log` na transação do caso de uso, com a equipa e o autor do contexto da transação (F1-04); `backend/internal/platform/audit/team_test.go` | M |
| TM-API-06 | T | Mudança incompatível de contrato quebra clientes ou abre um campo sem validação | `buf breaking` contra `main` no CI; código gerado verificado (`make gen-check`) | implementado | `.github/workflows/ci.yml` (job `contracts`) | M |
| TM-API-07 | D | Origem acedida diretamente, contornando o WAF | A origem só aceita tráfego da Cloudflare (autenticação de origem / mTLS) | planeado | F5-05 | M |

## Identidade, sessões e tokens (DF-01, DF-02, DF-04, DF-06)

Elementos: Zitadel, BFF, Valkey, tokens.

| ID | STRIDE | Ameaça | Mitigação | Estado | Prova ou cartão | Prio |
| --- | --- | --- | --- | --- | --- | --- |
| TM-AUTH-01 | S | Tomada de conta (phishing, palavra-passe fraca ou comprometida) | Passkeys, MFA obrigatório para donos, verificação de palavras-passe comprometidas, política de bloqueio (tudo no Zitadel) | planeado | F1-03 (login) e F1-03c (passkeys/MFA, proposto no contexto §7) | A |
| TM-AUTH-02 | I | Roubo de tokens por XSS no browser | BFF: nenhum token no JavaScript; cookie `__Host-` HttpOnly, Secure, SameSite=Strict; o Valkey guarda só o hash do cookie; o callback responde com `Referrer-Policy: no-referrer` e `no-store`; `/auth/session` devolve só identificadores | implementado | `backend/internal/platform/auth/flow_test.go` (`TestSuccessfulLogin`, `TestSessionInfo`); `scripts/auth-smoke.py` (atributos do cookie e valor do cookie ausente do Valkey) | A |
| TM-AUTH-03 | S | ID token forjado, de outro emissor ou de outra audiência aceite no login | `go-oidc`: assinatura, `iss`, `aud`, `exp`, `nbf`, algoritmo fixo em `PITBOX_OIDC_ALLOWED_ALGS` (rejeita `none` e HS*) e `nonce`; at_hash se existir; o `sub` do `userinfo` tem de ser o do ID token (OIDC Core 5.3.2). O ID token (que vai no `id_token_hint` do logout) não leva email nem nome. O Bearer e o JWKS com TTL ficam no F2-06 | implementado (login); Bearer: F2-06 | `backend/internal/platform/auth/flow_test.go` (`TestUserInfoValidation`, `TestTheProfileComesFromUserInfoAndTheIDTokenCarriesNone`; `TestIDTokenValidation`: 12 casos, incluindo alg none, HS256 com o segredo do cliente, outra chave, payload alterado, kid desconhecido, nonce, aud, iss, exp, nbf) | A |
| TM-AUTH-04 | S | Reutilização de um refresh token roubado | O refresh token nunca sai do servidor (cifrado no Valkey); o Zitadel rota-o mas **não revoga a família** (risco aceite até ao F2-06); um lock por sessão evita deslogar por refresh concorrente | parcial: o token não sai do servidor e a rotação é provada; a revogação da família fica para o F2-06 | `backend/internal/platform/auth/authenticate_test.go` (`TestFiftyConcurrentRequestsMakeOneRefresh`); `scripts/auth-smoke.py` (refresh real com token rodado) | M |
| TM-AUTH-05 | E | Operação sensível feita com uma sessão antiga | Step-up: `auth_time` ≤ 5 min e `amr` com fator forte, senão `UNAUTHENTICATED` | planeado | F1-03c | M |
| TM-AUTH-06 | I | O Valkey guarda tokens de sessão (segredos) e é acedido sem TLS ou autenticação | TLS e autenticação em produção; segredos com TTL; refresh tokens cifrados (AES-256-GCM, chave derivada por função, `kid`, ligados à sessão por aad); utilizador ACL limitado a `pitbox:sess:*`, `pitbox:lock:*` e `pitbox:rl:*` sem `@dangerous`; no compose local só `127.0.0.1` e palavra-passe | implementado (cifra e ACL); TLS em produção: F5-04 | `backend/internal/platform/auth/session_test.go` (`TestApplicationUserIsConfinedByTheACL`, `TestSealedTokensAreBoundToTheirSession`, `TestSessionKeyRotation`); `backend/internal/platform/auth/keys_test.go` (+ fuzz); `deploy/compose/docker-compose.yml` | M |
| TM-AUTH-07 | S | Credenciais do IdP na base do PitBox | O PitBox só guarda `idp_subject`, email e nome; credenciais nunca | implementado | `backend/db/schema/schema.sql` (`identity.user_account`); ADR 0004 | M |
| TM-AUTH-08 | T | Callback forjado (CSRF de login, *login fixation*): o atacante liga a vítima à sessão dele | `state` e `nonce` no cookie de pré-login cifrado (Lax, 10 min); `state` comparado em tempo constante antes de qualquer chamada ao Zitadel; sem cookie válido → 400; ID da sessão novo no callback; o código só vale uma vez e o PKCE liga-o ao cookie | implementado | `backend/internal/platform/auth/flow_test.go` (`TestCallbackWithoutAValidLoginStateNeverCallsTheProvider`, `TestAnAuthorizationCodeWorksOnce`, `TestPKCEVerifierMustMatch`, `TestLoginReplacesTheSessionTheBrowserHolds`) | A |
| TM-AUTH-09 | T | `return_to` leva o utilizador a outro site (*open redirect*) | Só caminhos absolutos sem host; rejeita `//`, `\\`, esquemas, controlo, segmentos vazios e `.`/`..` (em bruto e decodificados) e `/auth/`; validado no login e outra vez na página de destino | implementado | `backend/internal/platform/auth/returnto_test.go` (`FuzzSafeReturnTo`, 30 s sem falhas); `TestReturnToIsRevalidatedAtTheLanding` | M |
| TM-AUTH-10 | S | Primeiro login liga o utilizador a uma conta existente por email | `identity.resolve_login` procura só por `sub`; email de outro `sub` → erro e auditoria; só com `email_verified = true`; conta apagada não entra | implementado | `backend/db/tests/login_test.sql`; `backend/internal/identity/store/login_test.go` (corridas de 20 e 12 logins em paralelo); `backend/internal/platform/auth/flow_test.go` (`TestRefusedAccountsLookTheSameAndAreAudited`) | A |
| TM-AUTH-11 | E | `resolve_login` (`SECURITY DEFINER`) abusada para ler ou criar utilizadores | `search_path` fixo, nomes qualificados, `EXECUTE` só para `pitbox_app`, devolve só o `uuid`; `pitbox_app` sem contexto continua sem ver linhas nem inserir diretamente; a lista de funções `SECURITY DEFINER` é fixada por teste | implementado | `backend/db/tests/structure_test.sql` (secção 9); `backend/db/tests/login_test.sql`; testes de mutação (sem `REVOKE`, sem `search_path`, procura por email) | A |
| TM-AUTH-12 | T | Logout forçado por outro site (CSRF) ou sessão que sobrevive ao logout | `POST` com `Origin` verificado; revoga o refresh token no Zitadel antes de apagar a sessão (e apaga-a mesmo que a revogação falhe); `end_session` com `id_token_hint` | implementado | `backend/internal/platform/auth/flow_test.go` (`TestLogout`, `TestLogoutStillEndsTheSessionWhenRevocationFails`); `scripts/auth-smoke.py` | M |
| TM-AUTH-13 | D | `/auth/*` esgotado por pedidos em massa (cada login faz chamadas ao Zitadel) | Pré-login sem estado no servidor; callback sem cookie ou com `state` errado recusado **sem chamar o Zitadel e sem escrever na base de dados** (só log e contador `pitbox.auth.refused_logins{reason}`; a auditoria começa depois de o `state` bater certo); rate limiting por IP na borda (o BFF não limita) | parcial: sem chamadas ao Zitadel sem cookie válido; limite por IP: F5-05 | `backend/internal/platform/auth/flow_test.go` (`TestCallbackWithoutAValidLoginStateNeverCallsTheProvider`, `TestCallbacksWithoutAValidStateAreCountedNotAudited`: 100 callbacks sem cookie, 0 linhas de auditoria) | M |
| TM-AUTH-14 | E | Utilizador desativado, bloqueado ou apagado no IdP continua com sessão no PitBox | A janela de revogação é o access token de 10 min: no refresh seguinte o Zitadel recusa e a sessão acaba (`invalid_grant` ou `invalid_request` com `Errors.User.*`); recusas ambíguas ou operacionais (cliente recusado, pedido nosso mal formado, falha de rede) **não** terminam sessões | implementado | `backend/internal/platform/auth/authenticate_test.go` (`TestWhichRefusalsEndTheSession`); `scripts/auth-smoke.py` (desativa o utilizador no Zitadel real e vê a sessão acabar). Defeito apanhado pelo smoke e corrigido: a recusa do Zitadel não é `invalid_grant` | A |
| TM-AUTH-15 | S | Pedido com cookie vindo de outra origem ou de um subdomínio comprometido (CSRF; SameSite=Strict não cobre *same-site*) | O interceptor e `/auth/session` recusam `Origin` que não é a nossa e `Sec-Fetch-Site` diferente de `same-origin`/`none`, antes de olhar para a sessão; dois cookies de sessão (ambíguo) = sem sessão | implementado | `backend/internal/platform/auth/authenticate_test.go` (`TestInterceptorRefusesCrossOriginRequestsBeforeLookingAtTheSession`, `TestInterceptorIsDefaultDeny`); `backend/internal/bootstrap/auth_test.go` | A |
| TM-AUTH-16 | R | Logins, falhas e logouts sem rasto, ou um login que acontece sem auditoria | `audit.audit_log` (equipa NULL) com ator, motivo fixo, trace, IP do peer e user agent; **um login que não se consegue auditar não acontece**; nunca tokens, códigos, state, emails nem *claims* | implementado | `backend/internal/platform/auth/flow_test.go` (`TestALoginThatCannotBeAuditedDoesNotHappen`, `TestSuccessfulLogin`); `backend/internal/platform/audit/audit_test.go`; `scripts/auth-smoke.py` | M |
| TM-AUTH-17 | D | Falha do Zitadel, do Valkey ou da base de dados deslogava todos os utilizadores ou deixava passar sem autenticar | Falha de armazém ou de IdP → `UNAVAILABLE` (nunca `UNAUTHENTICATED`) e a sessão é mantida; graça de 2 min com o access token expirado; sem configuração de autenticação o serviço recusa tudo (nunca serve sem autenticar); descoberta do IdP preguiçosa com *backoff* | implementado | `backend/internal/platform/auth/authenticate_test.go` (`TestProviderOutageGivesAGracePeriodThenUnavailable`, `TestAStoreOutageIsUnavailableNotUnauthenticated`, `TestDenyAllRefusesEverythingAndNeverCallsTheHandler`); `backend/internal/bootstrap/auth_test.go` | M |

## Autorização (DF-05, DF-07)

Elementos: OpenFGA, RLS, casos de uso.

| ID | STRIDE | Ameaça | Mitigação | Estado | Prova ou cartão | Prio |
| --- | --- | --- | --- | --- | --- | --- |
| TM-AUTHZ-01 | I | IDOR/BOLA: utilizador acede a um recurso de outra equipa por ID | `Check` do OpenFGA por chamada + RLS; `NOT_FOUND` para recursos de outras equipas; teste de outra equipa em cada endpoint | planeado | Barreira RLS já testada (TM-DB-01); `Check` e testes por endpoint: F1-05 | A |
| TM-AUTHZ-02 | E | Piloto edita estratégia; convidado vê motores (papel a mais) | Papéis no modelo OpenFGA com asserções, incluindo casos negativos, corridas no CI | planeado | F1-05 (`backend/authz/model.fga.yaml`) | A |
| TM-AUTHZ-03 | E | Caso de uso esquece o `Check` ou a transação com contexto | Convenção `authz.Check` → `db.WithTeamTx`; RLS falha fechada sem contexto (sem linhas visíveis) e um contexto malformado dá erro; a pertença à equipa é verificada na própria base, por isso um caso de uso que se esqueça do `Check` ainda não entrega dados de uma equipa a quem não é membro | implementado (`Check` do OpenFGA: F1-05) | `backend/db/tests/rls_and_constraints_test.sql` ("sem contexto, sem dados"); `backend/internal/platform/db/tx_test.go` (`TestTeamIsolation`) | A |
| TM-AUTHZ-04 | T | Ligar um registo a um recurso de outra equipa por FK | FKs compostas `(team_id, x_id)` | implementado | `backend/db/tests/rls_and_constraints_test.sql` ("referência a carro de outra equipa") | A |
| TM-AUTHZ-05 | E | Papel dono/migração usado pela aplicação, ou `BYPASSRLS` | Aplicação como `pitbox_api` (grupo `pitbox_app`), sem `BYPASSRLS`; migrações como `pitbox_migrator`; dono `pitbox_owner`; **cada ligação do pool verifica o papel** (`current_user` membro de `pitbox_app`, não superutilizador, sem `BYPASSRLS`, não membro de `pitbox_owner`) e falha se estiver errado | implementado | `backend/db/tests/structure_test.sql` (secções 4–6); `backend/internal/platform/db/tx_test.go` (`TestThePoolRefusesRolesThatBypassRLS`: migrador e superutilizador recusados) | A |
| TM-AUTHZ-06 | E | Um SQL que corra como `pitbox_api` reescreve o contexto (`set_config('app.team_id', …)`) e muda de equipa (o contexto é uma variável de sessão) | SQL só com parâmetros e `sqlc`; só `platform/db` escreve o contexto, e um teste de arquitetura recusa `set_config`, `SET LOCAL`, `SET ROLE` e `app.team_id`/`app.user_id` em *strings* Go (avaliando constantes inteiras e desfazendo aspas, `||` e espaços) e nos `.sql` do `sqlc` (ficheiro inteiro, comentários incluídos) fora dele; `Begin`/`BeginTx` e `pgxpool` também só lá; a ligação tem de começar sem contexto | implementado; **limites conhecidos**: injeção de SQL continua a ser crítica e o teste não vê texto construído em tempo de execução (`fmt.Sprintf`) | `backend/internal/archtest/archtest_test.go` (`TestRLSContextAndTransactionRules`, `TestSQLFilesCannotWriteTheRLSContext`, `TestConstantsAreFollowed`: os contornos aceites em revisão, como `"set_" + "config("`, o nome entre aspas, `'app.' || 'team_id'`, o `(` na linha seguinte e um `'--'` dentro de uma *string*, falham); `backend/internal/platform/db/tx_test.go` (`TestThePoolRefusesConnectionsThatStartWithAContext`; `TestKnownLimit…` documenta o limite); ADR 0005, emenda | A |
| TM-AUTHZ-07 | I | O contexto de um pedido fica na ligação do pool e é herdado pelo pedido seguinte | `set_config(…, true)`: o valor dura só até ao fim da transação; a função não recebe o `pgx.Tx` (sem `Commit` nem transação aninhada) e o `DBTX` morre quando a transação acaba | implementado | `backend/internal/platform/db/tx_test.go` (`TestTheContextDoesNotLeakToTheNextUseOfTheConnection`: pool de 1 ligação depois de *commit*, *rollback*, *panic*, cancelamento e recusa; `TestConcurrentTeamsNeverSeeEachOther`; `TestTheFunctionCannotCommitOrKeepTheTransaction`) | A |
| TM-AUTHZ-08 | E | Um membro removido da equipa mantém o acesso até o worker apagar a relação no OpenFGA (cópia assíncrona da `membership`) | `WithTeamTx` confirma a pertença em `identity.membership` (fonte de verdade) em cada transação, com a equipa e o utilizador não apagados | implementado | `backend/internal/platform/db/tx_test.go` (`TestTeamIsolation`: *a removed member loses access at once, a demoted one the role*: apaga-se a linha da `membership` e a transação seguinte já recebe `ErrNotMember`, sem esperar por nenhum worker) | A |
| TM-AUTHZ-09 | I | Sondar ids de equipas: a resposta distingue "equipa de outro" de "não existe" ou "apagada" | Um só erro, `ErrNotMember` (embrulha `apperr.ErrNotFound`), com o mesmo texto; o interceptor de erros responde `NOT_FOUND` com mensagem fixa, num só sítio | implementado | `backend/internal/platform/db/tx_test.go` (o texto do erro é igual nos seis casos); `backend/internal/platform/interceptors/errors_test.go`; `backend/internal/bootstrap/rpc_test.go` (`TestANotMemberErrorIsNotFoundForTheClient`) | A |
| TM-AUTHZ-11 | D | Um cliente provoca erros `INTERNAL` (500, registados como ERROR) à vontade, por exemplo com um `team_id` nulo que o protovalidate aceita | `ParseTeamID` devolve `ErrInvalidTeamID`, que embrulha `apperr.ErrInvalidArgument`; o interceptor de erros responde `INVALID_ARGUMENT` com mensagem fixa e regista-o como erro do cliente (Info) | implementado | `backend/internal/platform/interceptors/errors_test.go`; `backend/internal/bootstrap/rpc_test.go` (`TestANilTeamIDIsInvalidArgumentNotInternal`); `backend/internal/platform/db/tx_test.go` (`TestZeroAndMalformedIdsNeverReachTheDatabase`) | M |
| TM-AUTHZ-10 | R | Uma entrada de auditoria de equipa escrita na equipa errada ou em nome de outro | `audit.RecordTeam` lê a equipa e o autor de `app.current_team_id()` / `app.current_user_id()` no próprio SQL (a função não tem esses campos) e não escreve sem contexto; vai na mesma transação e a tabela é só de acrescento | implementado | `backend/internal/platform/audit/team_test.go` | M |
| TM-AUTHZ-12 | E | O worker aplica eventos de membership fora de ordem (por exemplo, uma remoção depois de uma adição) e devolve acesso removido | Eventos de estado (`membership.changed`) idempotentes; claim ordenado por chave (`team_id`, `aggregate`, `aggregate_id`) com `NOT EXISTS`, lease e backoff; `SetTeamRole` atómico | implementado | `backend/internal/platform/outbox/worker_test.go` (`TestWorkerKeepsOrderPerKey`, `TestWorkerReplaysAreIdempotent`, `TestShutdownMarksOnlyWhatItApplied`); ADR 0015; reconciliação periódica: #122 | A |
| TM-AUTHZ-13 | E | O worker liga-se como `pitbox_api` e pode ler ou escrever todas as tabelas, quando só precisa da outbox | Papel `pitbox_worker` com `GRANT` só sobre `platform.outbox` (e leitura estritamente necessária), a criar no deploy | planeado | F1-16 / F5; registado no PR #113 | M |
| TM-AUTHZ-14 | S | Um convite reencaminhado é aceite por outra conta (ou um convite antigo é reutilizado) | Convite de uso único com token de 256 bits guardado só como SHA-256; validade de 7 dias; um convite por email só é aceite pela conta com esse email verificado (comparação `lower()`); todas as recusas são o mesmo `NOT_FOUND` | implementado | `backend/internal/identity/app/integration_test.go` (`TestInvitationFailuresAreIndistinguishable`, `TestAcceptIsIdempotentAndDoesNotChangeAnExistingRole`); `backend/db/tests/rls_and_constraints_test.sql` (F1-06) | A |
| TM-AUTHZ-15 | E | Quem entra só com palavra-passe cria uma equipa e torna-se dono sem MFA | `CreateTeam` exige um fator forte na sessão (`StepUp` com `StrongFactor`, sem a janela de 5 min); sem MFA responde `UNAUTHENTICATED` | implementado | `backend/internal/platform/interceptors/stepup_test.go` (`TestStrongFactorNeedsMFANotARecentLogin`); `backend/internal/bootstrap/api.go` | A |
| TM-AUTHZ-16 | E | Um não-membro obtém um contexto de RLS de equipa (criar equipa ou aceitar convite) e lê/escreve dados da equipa | Só `platform/db` escreve o contexto; `WithNewTeamTx` usa o id que ele próprio gera e a pertença é criada na mesma transação; `WithJoinTeamTx` só define `app.team_id` depois do join e continua a exigir a pertença; o `archtest` só permite estas variantes em `internal/identity/app` | implementado | `backend/internal/platform/db/tx_test.go` (`TestNewTeamTx`, `TestJoinTeamTx`); `backend/internal/archtest/archtest_test.go` | A |

## Base de dados e migrações (DF-07)

Elementos: PostgreSQL + TimescaleDB, migrações Atlas.

| ID | STRIDE | Ameaça | Mitigação | Estado | Prova ou cartão | Prio |
| --- | --- | --- | --- | --- | --- | --- |
| TM-DB-01 | I | Tabela nova de equipa sem RLS, ou RLS sem política | Teste automático: toda a tabela da aplicação tem RLS e pelo menos uma política (exceções por lista explícita) | implementado | `backend/db/tests/structure_test.sql` (1–3); CI job `database` | A |
| TM-DB-02 | I | Tabela nova legível por `pitbox_readonly` (suporte/análise) com dados sensíveis | `REVOKE` explícito; lista exata de tabelas ilegíveis comparada em teste | implementado | `backend/db/tests/structure_test.sql` (7) | A |
| TM-DB-03 | T | Adulterar ou editar uma migração já aplicada | `atlas.sum` + `atlas migrate validate` no CI | implementado | `make db-validate`; `scripts/db-test.sh` (passo 0) | A |
| TM-DB-04 | T | Migração destrutiva sem revisão | Lint por script: `DROP`, `TRUNCATE`, `RENAME`, `ALTER ... TYPE`, `SET NOT NULL`, `CREATE INDEX` sem `CONCURRENTLY`; exceção visível `-- lint:allow` | implementado | `scripts/db-lint.py`; ADR 0010. Aproximação por expressões regulares | M |
| TM-DB-05 | T | `backend/db/schema/schema.sql` e migrações divergem (a base de produção difere da testada) | Teste de desvio: `pg_dump` das duas bases, papéis, extensões e jobs; falha com dump vazio | implementado | `scripts/db-test.sh` (passo 5) | M |
| TM-DB-06 | R | Alterar ou apagar auditoria, versões de setup e movimentos de stock | Triggers `forbid_mutation` + `REVOKE UPDATE, DELETE` | implementado | `backend/db/tests/rls_and_constraints_test.sql`; `backend/db/tests/structure_test.sql` (6) | M |
| TM-DB-07 | E | `pitbox_owner` tem LOGIN (exigido pelo TimescaleDB; ADR 0011) e alguém lhe define palavra-passe | Sem palavra-passe; teste falha se existir; ligações por TCP caem em `scram-sha-256` | implementado | `backend/db/tests/default_privileges_test.sql`; ADR 0011 | M |
| TM-DB-08 | I | Agregações contínuas de telemetria (vistas materializadas) não têm RLS | Acesso por vista `security_barrier` ou função que filtra por `app.current_team_id()`, com teste de outra equipa | planeado | F4-09 (ADR 0011) | M |
| TM-DB-09 | I | Dados em repouso e em trânsito até à base | TLS até à base de dados; cifra em repouso; cifra por envelope em campos sensíveis; backups cifrados e imutáveis | planeado | F5-04, F5-08 | M |
| TM-DB-10 | D | Telemetria sem compressão cresce além do previsto | Retenção de 24 meses; gatilho de revisão aos ~20 GB; ficheiros brutos reprocessáveis | aceite | ADR 0011 (Nelson, 2026-10-12); estimativa por confirmar com dados reais | B |
| TM-DB-11 | I | Backup ou restauro expõe dados ou não funciona | Backups cifrados na UE com restauro testado; validade de 35 dias | planeado | F5-08 | M |

## Sincronização offline (DF-03)

Elementos: `SyncService`, passe offline, fila local.

| ID | STRIDE | Ameaça | Mitigação | Estado | Prova ou cartão | Prio |
| --- | --- | --- | --- | --- | --- | --- |
| TM-SYNC-01 | S | Operação enviada em nome de outro dispositivo ou utilizador | Assinatura ECDSA P-256 por operação (chave do dispositivo no armazenamento seguro); passe offline assinado pelo servidor | planeado | F1-13 | A |
| TM-SYNC-02 | T | Repetir ou reordenar operações antigas | `op_id` idempotente, processamento por ordem, `base_version` | planeado | F1-13 | A |
| TM-SYNC-03 | E | Dispositivo revogado ou papel alterado continua a escrever | Revalidação da autorização atual no `Push`; operações de dispositivo revogado ficam para revisão do dono | planeado | F1-13 | A |
| TM-SYNC-04 | D | `Push` enorme ou em ciclo esgota a API | Limites de tamanho e de operações por pedido; páginas de 500 no `Pull` | planeado | F1-13 | M |
| TM-SYNC-05 | T | Conflito resolvido em silêncio perde dados do mecânico | Conflito de campo gera registo `conflict` e escolha do utilizador; registos de acrescento nunca em conflito | planeado | F1-13 | M |
| TM-SYNC-06 | I | Pull devolve dados de outra equipa | RLS + autorização no `Pull`; cursor por equipa | planeado | F1-13 (barreira RLS já testada, TM-DB-01) | A |

## Dispositivo do utilizador perdido ou roubado (DF-02, DF-03)

Elementos: app, SQLite local, chave do dispositivo.

| ID | STRIDE | Ameaça | Mitigação | Estado | Prova ou cartão | Prio |
| --- | --- | --- | --- | --- | --- | --- |
| TM-DEV-01 | I | Ler a base local de um telemóvel ou browser roubado | SQLCipher no telemóvel, chave no armazenamento seguro; bloqueio da app; no browser, dados mínimos | planeado | F1-10, F2-06 | A |
| TM-DEV-02 | E | Usar o passe offline de 72 h de um dispositivo roubado | Revogação do dispositivo, validade de 72 h, assinaturas rejeitadas após revogação | planeado | F1-13 | A |
| TM-DEV-03 | I | Armazenamento de tokens no dispositivo | Keychain/Keystore (AppAuth); nenhum token em localStorage | planeado | F2-06 | M |

## Links de partilha (público)

Elementos: endpoint público de links de setup (arquitetura §6.3).

| ID | STRIDE | Ameaça | Mitigação | Estado | Prova ou cartão | Prio |
| --- | --- | --- | --- | --- | --- | --- |
| TM-SHARE-01 | I | Enumeração ou adivinhação de tokens | Token aleatório de 256 bits; só o hash SHA-256 guardado (`CHECK length = 32`) | implementado | `backend/db/schema/schema.sql` (`setup.share_link`) | M |
| TM-SHARE-02 | I | Distinguir token inexistente de expirado | Resposta idêntica; rate limiting próprio; endpoint separado | planeado | F1-08 | M |
| TM-SHARE-03 | I | Link dá mais do que o prometido | Ligação a um único setup, só leitura, com validade e revogável | planeado | F1-08 | M |

## Live timing (DF-08)

Elementos: `pitbox-worker`, conectores, sites de terceiros.

| ID | STRIDE | Ameaça | Mitigação | Estado | Prova ou cartão | Prio |
| --- | --- | --- | --- | --- | --- | --- |
| TM-TIM-01 | T | HTML/JSON hostil explora o parser (memória, tempo, profundidade) | Parser com limites de tamanho e profundidade; sem execução de scripts; fuzzing com páginas reais | planeado | F3-02, F3-03 | A |
| TM-TIM-02 | D | Pedidos em excesso levam ao bloqueio do IP ou violam os termos | 1 pedido a cada 5 s por prova, cache, `robots.txt`, User-Agent identificado; conectores só depois de confirmar os termos | planeado | F3-01, F3-04 | M |
| TM-TIM-03 | S | Resultados falsificados na fonte ou no caminho | HTTPS; origem do dado marcada (`events.data_source`); respostas brutas guardadas 30 dias | planeado | F3-04 | B |
| TM-TIM-04 | I | Worker com acesso à base de dados é o único a falar com a Internet aberta | Processo isolado, identidade de workload, rede só de saída necessária | planeado | F5-04 | M |

## Telemetria: ingestão e objetos (DF-09, DF-10, DF-14)

Elementos: `pitbox-ingest`, armazenamento de objetos.

| ID | STRIDE | Ameaça | Mitigação | Estado | Prova ou cartão | Prio |
| --- | --- | --- | --- | --- | --- | --- |
| TM-ING-01 | D | Ficheiro de telemetria gigante ou malformado derruba a ingestão | `pitbox-ingest` isolado do `pitbox-api`; validação de formato e CRC; limites de tamanho; processamento assíncrono | planeado | F4-08 | A |
| TM-ING-02 | T | Amostras de um módulo gravadas na equipa errada | `device_id` → `team_id` do certificado; `app.team_id` por mensagem; RLS em `telemetry.sample` | implementado | `backend/db/tests/structure_test.sql` (RLS em `telemetry.sample`; hypertable sem compressão); ingestão: F4-08 | A |
| TM-ING-03 | I | URL pré-assinado reutilizado ou demasiado permissivo | URL de curta duração, limitado ao objeto e ao tamanho | planeado | F4-08 | M |
| TM-ING-04 | S | Módulo revogado ainda envia | Certificado X.509 de 90 dias, rotação aos 60, lista de revogação verificada pelo ingest | planeado | F4-11 | M |

## Módulo no carro: firmware, emparelhamento e OTA (DF-13, DF-17)

Elementos: ESP32-S3, BLE, microSD.

| ID | STRIDE | Ameaça | Mitigação | Estado | Prova ou cartão | Prio |
| --- | --- | --- | --- | --- | --- | --- |
| TM-FW-01 | T | Firmware adulterado ou de um atacante instalado no módulo | Secure boot v2, flash cifrada, firmware assinado (Ed25519) verificado antes de instalar; anti-rollback | planeado | F4-11, F5-03; migração para ML-DSA: ADR 0007 | A |
| TM-FW-02 | S | Emparelhamento com um módulo ou uma app falsos | Código de uso único (5 min) pedido ao `DeviceService`; par de chaves gerado no módulo; CSR → certificado do servidor | planeado | F4-07, F4-11 | A |
| TM-FW-03 | E | Chave de assinatura de firmware comprometida | Chave em KMS, assinatura no CI; chave de verificação atualizável por OTA assinada pela chave antiga | planeado | F5-03; ADR 0007 | A |
| TM-FW-04 | T | Falha do módulo afeta o controlo do carro | O módulo só lê sensores, alimentação isolada com fusível e díodo, nunca partilha a linha de sinal dos servos; watchdog | planeado | F4-10 (regra de arquitetura nº 8) | A |
| TM-FW-05 | I | Cartão microSD retirado expõe dados | Dados de desempenho de baixa sensibilidade; sem credenciais no cartão | aceite | Nelson, 2026-10-12 (a confirmar no F4-11) | B |
| TM-FW-06 | D | Rollback para uma versão com falhas conhecidas | Contador anti-rollback; OTA só muda de partição se o arranque da nova versão for bem-sucedido | planeado | F5-03 | M |

## Rádio LoRa (DF-11)

Elementos: módulo → recetor, 868 MHz.

| ID | STRIDE | Ameaça | Mitigação | Estado | Prova ou cartão | Prio |
| --- | --- | --- | --- | --- | --- | --- |
| TM-LORA-01 | S | Pacotes forjados ou repetidos aceites pelo recetor | ChaCha20-Poly1305, número de sequência no nonce, rejeição de repetições; chave de sessão do emparelhamento | planeado | F5-02 | A |
| TM-LORA-02 | I | Escuta de telemetria | Cifra AEAD; só um resumo de ~24 B | planeado | F5-02 | B |
| TM-LORA-03 | D | Interferência (jamming) ou perda de pacotes | Fora de âmbito: o módulo grava sempre no cartão; o rádio é um extra | aceite | README (fora de âmbito) | B |
| TM-LORA-04 | D | Duty cycle excedido (violação regulamentar e perda de pacotes) | Medir o tempo de antena com o pacote final; opções no ADR 0007 (250 kHz, 0,5 Hz, compactar); nunca truncar a tag | planeado | F5-02 (acrescentar teste de tempo de antena); ADR 0007 | M |
| TM-LORA-05 | I | Chave de sessão derivada no emparelhamento quebrada no futuro (quântico) | Rever a derivação (ECDH → ML-KEM ou híbrido); a cifra simétrica de 256 bits mantém-se | planeado | ADR 0007 (migração); F4-07 | B |

## Administração e suporte (DF-16)

Elementos: papéis administrativos, `support_grant`, break-glass.

| ID | STRIDE | Ameaça | Mitigação | Estado | Prova ou cartão | Prio |
| --- | --- | --- | --- | --- | --- | --- |
| TM-ADM-01 | E | Administrador acede a dados de uma equipa sem conhecimento do dono | `support_grant` temporário (máx. 24 h, constraint na base de dados), ligado a um pedido, registado na auditoria e visível ao dono | planeado | Constraint na base de dados: `backend/db/schema/schema.sql` (`identity.support_grant`); fluxo: F1-06 e F5-11 | A |
| TM-ADM-02 | S | Conta administrativa comprometida | Papéis administrativos só no IdP, com passkey obrigatória | planeado | F5-04 | A |
| TM-ADM-03 | R | "Não fui eu" no acesso humano a produção | Break-glass auditado; acesso humano só nesse caso | planeado | F5-07, F5-11 | M |
| TM-ADM-04 | R | Negar uma alteração de setup | Auditoria com autor, hora e antes/depois (`audit.audit_log`, só de acrescento); operações offline assinadas | planeado | Tabela só de acrescento: `backend/db/tests/structure_test.sql` (6); escrita no servidor: F1-02 | M |

## CI, supply chain e entrega

Elementos: GitHub Actions, dependências, imagens, firmware.

| ID | STRIDE | Ameaça | Mitigação | Estado | Prova ou cartão | Prio |
| --- | --- | --- | --- | --- | --- | --- |
| TM-CI-01 | T | Ação de terceiros comprometida (tag movida) | Ações fixadas por SHA; só 3 ações de terceiros; `zizmor` e `actionlint` aos workflows | implementado | `.github/workflows/ci.yml`; `make workflows` | A |
| TM-CI-02 | E | Workflow com permissões a mais ou injeção por `${{ }}` | `permissions: {}` por omissão, `contents: read` por job; sem `pull_request_target`; sem `${{ }}` em `run:` | implementado | `.github/workflows/ci.yml` | A |
| TM-CI-03 | I | Segredo commitado | gitleaks no CI (histórico completo) e `pre-push`; uma fuga detetada obriga a rodar o segredo | implementado | `.gitleaks.toml`, `.githooks/pre-push`, job `secrets` | A |
| TM-CI-04 | T | Versão de dependência acabada de publicar e comprometida | Renovate com `minimumReleaseAge` de 3 dias, sem automerge | implementado | `.github/renovate.json` | M |
| TM-CI-05 | T | Ferramenta da toolchain adulterada | Versões fixadas em `mise.toml` e checksums em `mise.lock`, instalação `--locked` | implementado | `mise.toml`, `mise.lock`, `.github/workflows/ci.yml` | M |
| TM-CI-06 | T | Dependência com vulnerabilidade conhecida | `govulncheck` em cada PR e semanal; `pnpm audit`/osv-scanner e `cargo audit` quando houver app e firmware | implementado | `.github/workflows/ci.yml` (job `backend`), `.github/workflows/vulncheck.yml` (Go); restantes: F1-09, F4-03 | M |
| TM-CI-07 | T | Imagem de produção não é a que o CI construiu | Imagens assinadas (cosign keyless), SBOM, proveniência SLSA; verificação no deploy; Kyverno na fase 2 | planeado | F5-06 | A |
| TM-CI-08 | E | Chaves estáticas de cloud no repositório | OIDC do GitHub para a cloud, sem chaves; identidade de workload | planeado | F5-04, F5-06 | A |
| TM-CI-09 | T | `main` alterada sem revisão ou sem CI verde | Ruleset: PR + check `ci-ok`; `ci-ok` falha com qualquer job falhado ou cancelado | implementado | `.github/workflows/ci.yml` (job `ci-ok`); ruleset configurado no GitHub (manual, README) | A |

## Ambiente local (compose)

Elementos: `deploy/compose` (só desenvolvimento).

| ID | STRIDE | Ameaça | Mitigação | Estado | Prova ou cartão | Prio |
| --- | --- | --- | --- | --- | --- | --- |
| TM-LOCAL-01 | I | Serviços locais acessíveis a partir da rede | Portas publicadas só em `127.0.0.1`; segredos aleatórios no `.env` (fora do Git) | implementado | `deploy/compose/docker-compose.yml`; `make init` | M |
| TM-LOCAL-02 | T | Imagem de base alterada | Tag + digest fixados; Renovate propõe atualizações | implementado | `deploy/compose/docker-compose.yml` | M |
| TM-LOCAL-03 | E | Contentores do Zitadel como root e PAT de arranque com validade longa (2099) | Só para desenvolvimento, nunca em produção; motivo documentado no compose | aceite | `deploy/compose/docker-compose.yml` (Nelson, 2026-10-11, F0-03) | B |

## Privacidade (RGPD)

O STRIDE não cobre bem a privacidade. Linhas ligadas à especificação (§14) e ao cartão F5-10; para aprofundar (por exemplo, no mestrado), o método é o LINDDUN.

| ID | Tema | Risco | Controlo | Estado | Prova ou cartão | Prio |
| --- | --- | --- | --- | --- | --- | --- |
| TM-PRIV-01 | Minimização | Guardar mais dados pessoais do que o necessário | Só nome, email, fotografia opcional, dispositivos, IP de sessão e nomes de pilotos publicados; credenciais fora da base | implementado | `backend/db/schema/schema.sql` (`identity.user_account`) | M |
| TM-PRIV-02 | Direitos do titular | Exportar, corrigir e apagar sem passar por um humano | Exportação JSON, apagamento efetivo em 30 dias, backups expiram em 35 dias | planeado | F5-10 | M |
| TM-PRIV-03 | Retenção | Dados guardados além do necessário | Registos de segurança 12 meses; telemetria bruta 24 meses (configurável por equipa) | planeado | F5-10; retenção da telemetria: ADR 0011 | M |
| TM-PRIV-04 | Menores | Pilotos com menos de 16 anos | Só entram pela conta de um responsável | planeado | F1-06 / F5-10 | M |
| TM-PRIV-05 | Localização e telemetria | Dados de desempenho e de local usados para além da equipa | Isolamento por equipa (RLS), dados na UE, subcontratantes listados | planeado | F5-10 | M |
| TM-PRIV-06 | Violação de dados | Atraso a notificar | Procedimento documentado, 72 h para a CNPD e titulares afetados | planeado | F5-10, runbook em `docs/runbooks/` | M |
| TM-PRIV-07 | Registos e traces | Dados pessoais em logs, traces e erros | Nada de segredos ou dados pessoais em logs/traces/erros (checklist de segurança) | planeado | F1-02, F5-07 | M |

## Revisão

- Revisto no fim de cada fase e antes de cada versão do roadmap (plano, cartões contínuos).
- Regra: uma linha só passa a `implementado` quando a coluna "Prova" aponta para um ficheiro que existe no `main`. Uma verificação automática disto (cada `TM-*` `implementado` aponta para um caminho existente) está **proposta** e ainda não existe.
- Antes de a v1.0 sair: teste de intrusão externo (F5-12) e revisão ASVS nível 2 (F5-11).
