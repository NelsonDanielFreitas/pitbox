# PitBox — Plano de Trabalho

Oct 11, 2026 · @Nelson Freitas

## Como usar este quadro

Começa pelo cartão F0-01 e segue a ordem dos IDs: cada fase acaba com um cartão **Gate**, que só se fecha depois de usares o que construíste numa prova ou num treino real.

- **Um cartão de cada vez.** No máximo 2 cartões "Em curso"; só começas um novo quando fechares outro
- **Pronto quando** é o critério para mudar o estado para "Feito": se não está cumprido, o cartão não está feito
- **Bloqueado** = depende de algo fora do teu controlo (peça encomendada, resposta de um fornecedor); escreve o motivo num comentário no cartão
- Cada cartão é um branch e um pull request com o ID no nome (ex.: `f1-07-setup-service`), mesmo trabalhando sozinho: treina o processo das empresas e deixa histórico
- Para passar isto para o Trello ou GitHub Projects: uma lista por estado, uma etiqueta por fase, e o "Pronto quando" como checklist do cartão

Referem-se a: PitBox — Especificação Funcional (o quê) e PitBox — Arquitetura Técnica (como). Os ficheiros `schema.sql` e testes da base de dados entram no cartão F0-06.

## Fase 0 — Preparação

Objetivo: ter um repositório onde `make up` levanta todo o ambiente local e o CI corre em cada pull request, antes de escrever qualquer funcionalidade.

**Tecnologias desta fase**

| Área | Tecnologias e ferramentas | Para quê |
| --- | --- | --- |
| Ambiente | WSL2 com Ubuntu 24.04, VS Code + extensão WSL, mise | Desenvolvimento em Linux; versões de Go, Node, Rust e Python fixadas por projeto |
| Linguagens | Go 1.27 (versão estável atual), Node 24 + pnpm, Rust stable (rustup), Python 3.12 + uv | Backend, app, firmware e análise |
| Contentores | Docker Engine (ou Docker Desktop com integração WSL), Docker Compose | Ambiente local reproduzível |
| Serviços locais | PostgreSQL 16 (imagem `timescale/timescaledb`), Valkey 8, Zitadel, OpenFGA, SeaweedFS (S3), Jaeger | Os mesmos serviços da produção, em contentores |
| Contratos | Buf CLI, protoc-gen-go, protoc-gen-connect-go, protoc-gen-es | Gerar código Go e TypeScript a partir dos `.proto` |
| Base de dados | Atlas, sqlc, psql | Migrações versionadas e queries SQL tipadas |
| Qualidade | golangci-lint, gitleaks, lefthook (git hooks) | Lint e deteção de segredos antes do commit |
| CI e gestão | GitHub, GitHub Actions, GitHub Projects | Repositório, pipeline e quadro de cartões |
| Documentação | Markdown (ADRs), Mermaid | Decisões e diagramas no repositório |

| ID | Cartão | O que fazer | Pronto quando | Estado |
| --- | --- | --- | --- | --- |
| F0-01 | Criar o monorepo | Repositório no GitHub com a estrutura de pastas da arquitetura (§16.1), `README`, licença, `.editorconfig`, `.gitignore` | Estrutura no `main`; branch protegida com PR obrigatório | Por fazer |
| F0-02 | Instalar ferramentas | Go 1.27, Node 24 + pnpm, Rust (rustup + espup), Docker, Buf, sqlc, Atlas, golangci-lint; documentar versões num ficheiro `.tool-versions` (mise ou asdf) | Um colega consegue instalar tudo só com o README | Por fazer |
| F0-03 | Ambiente local com Docker Compose | Serviços: Postgres 16 (imagem TimescaleDB), Valkey, Zitadel, OpenFGA, SeaweedFS (S3), Jaeger; `Makefile` com `init`, `up`, `down`, `reset` | `make up` levanta tudo e os health checks passam | Por fazer |
| F0-04 | Primeiros contratos Protobuf | `buf.yaml`, `buf.gen.yaml`, pacote `pitbox.identity.v1` com um `TeamService` mínimo; geração para Go e TypeScript | `buf lint` e `buf generate` sem erros | Por fazer |
| F0-05 | CI inicial | GitHub Actions: `buf lint` + `buf breaking`, `golangci-lint`, `go test -race`, gitleaks; ações fixadas por SHA, permissões mínimas | PR de teste fica verde; PR com um segredo falso fica vermelho | Por fazer |
| F0-06 | Base de dados no repositório | `schema.sql` em `backend/db/schema`, Atlas Community com migrações escritas à mão (ADR 0010), testes SQL a correr no CI contra Postgres em contentor | A `0001_initial` é validada e aplicada pelo Atlas; o teste de desvio entre migrações e `schema.sql` passa; os testes de RLS passam no CI | Por fazer |
| F0-07 | ADRs e threat model inicial | `docs/adr` com as 8 decisões da arquitetura (§16.2); `docs/threat-model` com a tabela STRIDE | Ficheiros no `main` | Por fazer |
| F0-08 | Quadro do projeto | GitHub Projects com etiquetas por fase; os cartões criam-se como issues quando se começa cada um (dados em `docs/cartoes.json`), por isso o quadro só tem cartões por fazer ou em curso | Quadro criado e todos os cartões por fazer da Fase 0 e 1 (F0-08 e F1-01 a F1-15) lá; os cartões já feitos ficam só no plano | Por fazer |

## Fase 1 — v0.1 Fundações

Objetivo: o mecânico faz login, regista carros, motores e pneus, e grava setups na app sem rede, que sincronizam depois. Primeiro o backend, depois a app, depois o sync.

**Tecnologias desta fase**

| Área | Tecnologias e bibliotecas | Para quê |
| --- | --- | --- |
| API (Go) | `connectrpc.com/connect`, `buf.build/go/protovalidate`, `connectrpc.com/grpcreflect` (só em desenvolvimento) | Serviços Connect/gRPC e validação das mensagens |
| Base de dados (Go) | `jackc/pgx/v5`, sqlc, Atlas | Ligação ao Postgres, queries geradas, migrações |
| Autenticação (Go) | `coreos/go-oidc/v3`, `golang.org/x/oauth2`, `valkey-io/valkey-go` | Fluxo OIDC + PKCE no BFF, validação do ID token, sessões no Valkey (`lestrrat-go/jwx` só se o F2-06 o exigir) |
| Autorização | OpenFGA, `openfga/go-sdk`, CLI `fga` (testes de asserções) | Permissões por recurso |
| Plataforma (Go) | `log/slog`, `go.opentelemetry.io/otel`, `caarlos0/env`, outbox + poller próprio (ADR 0015; `riverqueue/river` no F2-04) | Logs, traces, configuração, fila de tarefas e outbox |
| Testes (Go) | `testing`, `stretchr/testify`, `testcontainers-go`, `google/go-cmp` | Testes unitários e de integração com Postgres real |
| App | React 19, Vite, TypeScript (strict), TanStack Router, TanStack Query | Estrutura da app e acesso a dados |
| Interface | Tailwind CSS, Radix UI, React Hook Form, Zod | Componentes acessíveis e formulários validados |
| API na app | `@connectrpc/connect-web`, `@bufbuild/protobuf` | Cliente gerado a partir dos `.proto` |
| Base local | `wa-sqlite` (OPFS), `vite-plugin-pwa` (Workbox) | SQLite no browser e funcionamento offline |
| Sync | `uuidv7` (npm), `google/uuid` (Go), Web Crypto (ECDSA P-256) | IDs gerados offline e assinatura das operações por dispositivo |
| Testes (app) | Vitest, Testing Library, Playwright | Testes de componentes e E2E em modo avião |

| ID | Cartão | O que fazer | Pronto quando | Estado |
| --- | --- | --- | --- | --- |
| F1-01 | Esqueleto do `pitbox-api` | `cmd/api`, configuração por variáveis de ambiente, logs slog em JSON, OpenTelemetry, health check, arranque e paragem graciosos | `curl /healthz` responde; traces visíveis num Jaeger local | Por fazer |
| F1-01b | Migrar para Connect v2 | Gerador e biblioteca `connectrpc.com/connect/v2` (ADR 0012); `make gen-check` verifica que os geradores coincidem com `go.mod` | `make gen`, `make lint`, `make test` e `make vuln` verdes só com `connectrpc.com/connect/v2` no `go.mod` | Por fazer |
| F1-02 | Interceptors Connect | Cadeia de interceptors da §4.4: OpenTelemetry, recuperação de panics, erros e log por RPC, deadline (30 s unários, 55 min streams), validação protovalidate com validador compilado no arranque e contexto de auditoria; limites de tamanho (4 MiB, também após descompressão) no transporte; reflection só em `local` | Testes de cada interceptor; um erro interno (`fmt.Errorf("pg: ...")`) chega ao cliente só como código, sem a mensagem; um corpo acima de 4 MiB e uma bomba de compressão são recusados (`CodeResourceExhausted`); um corpo enviado devagar é cortado no teto; o valor inválido não volta na resposta; teste de arquitetura (`internal/archtest`) que só admite mensagens fixas em `connect.NewError` e os limites em `httpserver/rpc.go` (ADR 0012) | Por fazer |
| F1-03 | Login com Zitadel + BFF | (a) Projeto e aplicação OIDC no Zitadel local com ES256 e tokens curtos (script idempotente); `/auth/login`, `/auth/callback` e `/auth/logout` no BFF (Authorization Code + PKCE, cookies `__Host-`, sessão no Valkey com refresh token cifrado); interceptor de autenticação no lugar 5 da cadeia; (b) criação do `user_account` no primeiro login (`identity.resolve_login`, só com email verificado) (ADR 0013) | Login e logout no browser; ID token adulterado, de outro emissor, com `nonce` errado ou algoritmo fora da lista é rejeitado num teste; 50 pedidos simultâneos com o access token caducado fazem um só refresh; segundo login não duplica o utilizador | Por fazer |
| F1-03c | Passkeys, MFA e sessões | Passkeys, MFA obrigatório para donos, códigos de recuperação, recuperação de conta, lista e revogação de sessões (*back-channel logout*), *step-up* com `auth_time` e `amr` lidos da sessão (ADR 0013) | Operação sensível com sessão antiga devolve `UNAUTHENTICATED` com pedido de reautenticação; sessão terminada no Zitadel deixa de funcionar | Por fazer |
| F1-03d | Endurecer os códigos de recuperação | 16 bytes (128 bits) em base32 canónico; HMAC-SHA256 com chave HKDF (`recovery-code`); `key_id` na tabela para rotação; função SQL com lista de candidatos; normalização estrita; falhas auditadas; runbook de rotação | Código gerado tem 128 bits; o mesmo código com chaves diferentes dá hashes diferentes; um código continua válido depois de rodar a chave; um código usado ou mal formado devolve a mesma resposta; `go test -race ./...`, `make lint`, `make db-test` e `TM-AUTH-18` verdes | Por fazer |
| F1-03e | Registo e conta no Zitadel | Mailpit local, SMTP e políticas idempotentes, login v2 com verificação de email obrigatória, rate limit dos POST de login, validade dos códigos, página de email por verificar, textos pt-PT e smoke de registo/recuperação | Conta nova no browser recebe email no Mailpit, verifica e entra; 10 falhas não bloqueiam; 11 POST → 429; `make smoke-auth`, `make test-scripts`, `make lint` e CI verdes | Em curso |
| F1-04 | Contexto de equipa e RLS | `db.WithTeamTx`, `WithTeamReadTx` e `WithUserTx` (não é um middleware: a equipa só se conhece no caso de uso, depois de `authz.Check`): `set_config` de `app.user_id` e `app.team_id` com parâmetros, verificação de pertença à equipa na própria base (`ErrNotMember` → `NOT_FOUND` no interceptor de erros), papel da ligação verificado em cada ligação (`pitbox_app`, nunca dono, superutilizador nem `BYPASSRLS`), auditoria de equipa na mesma transação (`audit.RecordTeam`) e testes de arquitetura (só `platform/db` escreve o contexto) (ADR 0005, emenda) | Teste de integração com PostgreSQL real: o utilizador da equipa B não lê nem escreve dados da equipa A; quem não é membro recebe `NOT_FOUND`, igual para equipa de outro, inexistente ou apagada; o contexto não passa de um pedido para o seguinte na mesma ligação | Por fazer |
| F1-04b | Rate limiting na API | Interceptor no lugar 6 da cadeia (§4.4): limites por utilizador e por equipa (GCRA ou token bucket no Valkey), mais apertados nas escritas do que nas leituras; `ResourceExhausted` sem detalhes internos; definir o comportamento quando o Valkey falha (fail open com alerta, ou fail closed) | Esgotar a quota da equipa A não afeta a equipa B; teste com o Valkey em baixo que prova o comportamento escolhido | Por fazer |
| F1-05 | Autorização com OpenFGA | Modelo `.fga` (§6.1) com ficheiro de asserções; `Check` em cada caso de uso; worker sincroniza membros → relações | Asserções no CI; piloto recebe `PERMISSION_DENIED` ao editar um setup | Por fazer |
| F1-05b | Worker da outbox (membros → tuplos) | ADR 0015 (poller próprio até ao F2-04); `platform/outbox.Record` com eventos de estado `membership.changed`; `authz.SetTeamRole`; claim com lease (`FOR UPDATE SKIP LOCKED`), ordem estrita, backoff e runbook; `cmd/worker` + `bootstrap.RunWorker`; sqlc (schema, `make gen`, CI) | Evento na outbox → `Check` reflete o papel; remover remove o acesso; reaplicar não muda nada; duas instâncias não repetem; `SIGTERM` não perde eventos (testcontainers Postgres + OpenFGA) | Por fazer |
| F1-06 | Equipas e convites | `TeamService` e `MemberService`: criar equipa, convidar por email ou link, aceitar, mudar papéis | Fluxo completo testado; convite expirado ou reutilizado falha | Por fazer |
| F1-06b | Limites de propriedade e idempotência | Limite de 10 equipas dono em `CreateTeam` e `AcceptTransfer`, com lock por conta; `GetIdempotencyKey` respeita `expires_at`; takeover atómico da chave expirada; comparação do `method` no replay; `CHECK (expires_at > created_at)`; testes de concorrência e transferência; token de email mantém-se até ao F5 (#124) | 11.ª equipa recusada na criação e na transferência; replay de chave viva funciona no limite; chave expirada não faz replay da equipa antiga; método diferente é recusado; `make db-test` e CI verdes | Por fazer |
| F1-07a | Motorização e escala extensíveis | ADR 0016; catálogos globais `garage.scale`/`garage.powertrain`; `garage.car.scale`/`powertrain` com FK/defaults; contratos do F1-07 com os dois campos; testes SQL e de estrutura; Fase 6 no plano | Carro sem os campos fica 1/8/nitro; escala/motorização inexistente recusada (FK); a app não escreve nos catálogos; uma escala nova é uma linha (teste); `make db-test` e CI verdes | Por fazer |
| F1-07 | Garagem no backend | `CarService`, `EngineService`, `TyreService`, `PartService` com queries sqlc; montagem de motor com histórico; `Car.scale`/`powertrain` validados contra os catálogos (ADR 0016); inativo só bloqueia criação/mudança; autorização `team` (`member`/`mechanic`) + RLS, sem tuplos por recurso; custos omitidos ao piloto; `garage/api.go` (`CarExists`, catálogo); idempotência movida para `platform/idempotency` | CRUD testado com testcontainers; motor não fica em dois carros; 1/8 nitro sem regressão | Por fazer |
| F1-07b | Peças montadas e abate no backend | `InstallPart`, `RemovePart` e `ListPartInstalls` no `PartService`: montagem em carro ou motor com movimento `installed`, abate com destino (`stock`, `scrap` com motivo, `spare`); migração aditiva de `part_install`; idempotência e auditoria | Montar tira 1 ao stock; desmontar para o stock devolve 1; abater regista o motivo; peça sem vida útil recusada; outra equipa `NOT_FOUND`, piloto `PERMISSION_DENIED`; repetir o `request_id` não duplica | Por fazer |
| F1-08 | Setups no backend | `SetupService`: criar, nova versão, comparar duas versões, link de partilha; esquema JSON v1 dos campos 1/8 TT | Diferenças entre versões corretas; link revogado deixa de funcionar | Feito |
| F1-09 | Esqueleto da app | Vite + React + TypeScript strict, TanStack Router e Query, Tailwind, cliente Connect gerado, login via BFF | App abre, faz login e mostra a lista de equipas | Feito |
| F1-09b | Ecrãs de equipa: criar, convidar e aceitar | Criar equipa com fator forte, aceitar convite por link, listar/revogar convites e gerir membros; gestão online-only (ADR 0020) | Conta nova cria equipa e chega à garagem; segundo utilizador aceita convite; token não fica na URL; piloto não vê nem executa gestão | Em curso |
| F1-10 | Base local na app | SQLite (wa-sqlite + OPFS), migrações locais, repositórios por funcionalidade | Dados persistem depois de fechar e reabrir o browser | Feito |
| F1-11 | Ecrãs de garagem | Listas e formulários de carros, motores, pneus e peças, a ler e escrever na base local | Registar um carro completo em menos de 1 min no telemóvel | Em curso |
| F1-12 | Ecrãs de setup | Ficha por grupos (§5.1 da especificação), copiar de outro setup, comparar lado a lado, feedback do piloto | Setup completo gravado em menos de 2 min | Por fazer |
| F1-13 | Sincronização | `SyncService` Push/Pull, fila de operações com UUIDv7, assinatura por dispositivo, resolução de conflitos por campo (§8) | Reenviar a mesma fila não duplica nada; conflito aparece ao mecânico | Por fazer |
| F1-14 | Teste em modo avião | Playwright: criar setup offline, voltar a ligar, verificar no servidor | Teste E2E verde no CI | Por fazer |
| F1-15 | **Gate v0.1** | Usar a app num treino ou prova real para registar todos os setups | Nenhum setup registado em papel nesse dia; lista de problemas anotada | Por fazer |

## Fase 2 — v0.2 Box

Objetivo: gerir um fim de semana de corrida inteiro na app, incluindo uma final com reabastecimentos, sem rede e sem papel.

**Tecnologias desta fase**

| Área | Tecnologias e bibliotecas | Para quê |
| --- | --- | --- |
| Backend | `riverqueue/river`, outbox em `platform.outbox` | Atualizar contadores do motor e gerar avisos de manutenção de forma assíncrona |
| Modo final | XState v5 + `@xstate/react` | Máquina de estados da corrida (em pista, em box, flameout) |
| Lógica de estratégia | TypeScript puro, `fast-check` | Cálculos de paragens e testes de propriedades |
| App nativa | Capacitor 8, Android Studio | Empacotar a app para Android |
| Plugins Capacitor | `@capacitor/haptics`, `@capacitor/local-notifications`, `@capacitor-community/keep-awake`, `@capacitor-community/sqlite` (SQLCipher), armazenamento seguro (Keystore) | Vibração, avisos, ecrã sempre ligado, base local cifrada, guardar tokens |
| Login no telemóvel | AppAuth (via plugin Capacitor) | Login OIDC + PKCE no browser do sistema |
| Datas e tempos | `date-fns`, `performance.now()` | Horário do dia e cronómetro preciso |
| Testes | Vitest, Playwright (emulação de telemóvel e modo avião) | Simular uma final completa sem rede |

| ID | Cartão | O que fazer | Pronto quando | Estado |
| --- | --- | --- | --- | --- |
| F2-01 | Provas e sessões no backend | `EventService` e `SessionService`: pistas, campeonatos, provas, sessões com carro, motor, pneus, setup e condições | Sessão ligada a uma versão de setup imutável | Por fazer |
| F2-02 | Horário do dia na app | Lista de sessões, contagem decrescente, aviso a 15 e 5 min, ajuste de atraso global | Notificação local dispara com o telemóvel bloqueado | Por fazer |
| F2-03 | Resultado manual e voltas | Introdução rápida de resultado e voltas; incidentes por volta | Resultado de uma manga registado em menos de 1 min | Por fazer |
| F2-04 | Manutenção | `MaintenanceService`: intervenções com peças do stock, regras, vista de estado; contadores do motor atualizados por eventos (outbox + River) | Motor passa a "vencido" ao atingir o limite de depósitos | Por fazer |
| F2-05 | Checklists | Modelos de checklist pré-prova e pré-sessão; itens obrigatórios bloqueiam o "pronto" | Checklist pré-prova gerada a partir das regras vencidas | Por fazer |
| F2-06 | Capacitor no Android | Empacotar a app; plugins de vibração, ecrã sempre ligado, notificações locais, armazenamento seguro, SQLite nativo cifrado; login móvel com AppAuth e caminho `Authorization: Bearer` no backend (ADR 0013: `client_id` permitido, rejeitar tokens com `nonce`, `at_hash` ou `auth_time`, JWKS com TTL e limite de pedidos, revogação da família de refresh tokens, que o Zitadel não faz) | App instalada no teu telemóvel por APK de teste; ID token apresentado como Bearer é rejeitado num teste | Por fazer |
| F2-07 | Motor de estratégia | Funções puras em TypeScript: número de paragens, volta de aviso (fórmula §9.1), recálculo após flameout; testes de propriedades | Cobertura total; casos de 30 e 45 min verificados à mão | Por fazer |
| F2-08 | Ecrã do modo final | Máquina de estados (XState), cronómetro, contador de voltas por toque, botões grandes, avisos com vibração e som | Simulação de 45 min em modo avião sem falhas nem atrasos | Por fazer |
| F2-09 | Paragens e plano | Registo de paragens só de acrescento; plano antes da final com cenários | Paragem corrigida fica como nova linha, a original mantém-se | Por fazer |
| F2-10 | Relatório pós-final | Linha do tempo da corrida, tempo na box vs plano, atualização de voltas por depósito | Relatório gerado automaticamente ao terminar a final | Por fazer |
| F2-11 | Ensaio com o teu pai | Treino com final simulada de 30 min: tu na box com a app, o teu pai a pilotar | Lista de melhorias anotada e as críticas resolvidas | Por fazer |
| F2-12 | **Gate v0.2** | Gerir uma prova real inteira, incluindo a final, só na app | Todos os avisos de reabastecimento chegaram com ≥ 2 voltas de antecedência; nenhum dado perdido | Por fazer |

## Fase 3 — v0.3 Análise

Objetivo: os tempos de cada prova entram sozinhos (ou por ficheiro) e a app mostra que alterações de setup tornaram o carro mais rápido e mais consistente.

**Tecnologias desta fase**

| Área | Tecnologias e bibliotecas | Para quê |
| --- | --- | --- |
| Importação (Go) | `encoding/csv`, `golang.org/x/net/html`, `golang.org/x/time/rate` | Ler CSV e HTML de resultados com limite de pedidos |
| Armazenamento | `aws-sdk-go-v2` (S3), SeaweedFS em local | Guardar as respostas brutas para reprocessar |
| Segurança dos parsers | Go native fuzzing (`go test -fuzz`) | Garantir que páginas malformadas não partem o serviço |
| Streaming | Connect server-streaming | Atualizações ao vivo durante uma final |
| Gráficos na app | uPlot, Recharts | Voltas por sessão e resumos |
| Análise (Python) | uv, JupyterLab, Polars (ou pandas), `psycopg` 3, Matplotlib, SciPy | Estudar o efeito dos setups e o desgaste dos pneus |

| ID | Cartão | O que fazer | Pronto quando | Estado |
| --- | --- | --- | --- | --- |
| F3-01 | Levantamento das fontes | Ver que plataformas de resultados usam as provas onde correm, que exportações oferecem e o que os termos permitem | Tabela de fontes com decisão por fornecedor (usar, só CSV, não usar) | Por fazer |
| F3-02 | Importação por ficheiro | Upload de CSV/HTML, `raw_import` no armazenamento de objetos, parser com limites, correspondência por transponder | Resultados de uma prova real importados corretamente | Por fazer |
| F3-03 | Fuzzing dos parsers | Go native fuzzing com páginas reais guardadas como corpus | 1 h de fuzzing sem panics no CI noturno | Por fazer |
| F3-04 | Primeiro conector automático | Implementar `TimingProvider` para o fornecedor aprovado em F3-01, no worker, com limite de pedidos; se houver streaming para a app: reconexão a partir de um cursor e heartbeat periódico (a borda corta streams sem dados; a confirmar no F5-05) | Resultados chegam sem ação manual até 5 min após publicados | Por fazer |
| F3-05 | Rivais | Competidores vistos nos resultados; marcar rivais; comparação volta a volta | Gráfico tu vs rival numa manga | Por fazer |
| F3-06 | Análise por sessão | Gráfico de voltas com incidentes; melhor, média, mediana, consistência, melhores 5 consecutivas | Valores batem com um cálculo manual numa folha de cálculo | Por fazer |
| F3-07 | Exploração em Python | Notebook em `analysis/` que lê a base de dados (papel `pitbox_readonly`) e estuda efeito de setups e desgaste de pneus | Uma conclusão real sobre o vosso carro, com o número de sessões em que se baseia | Por fazer |
| F3-08 | Análises ao longo da época na app | Evolução por pista; antes/depois de uma alteração com condições semelhantes | Ecrã mostra o número de sessões usadas em cada conclusão | Por fazer |
| F3-09 | **Gate v0.3** | Prova inteira com tempos importados e análise feita na mesma noite | Tempos casados pelo transponder sem correções manuais | Por fazer |

## Fase 4 — v0.4 Telemetria

Objetivo: um módulo no carro grava temperatura, rotações, tensão e impactos para cartão, e os dados aparecem na app sobre os tempos de volta. Esta fase pode começar em paralelo com a Fase 3: as peças demoram a chegar.

**Tecnologias desta fase**

| Área | Tecnologias e bibliotecas | Para quê |
| --- | --- | --- |
| Hardware | ESP32-S3, SX1262, MAX31855 + termopar K, sensor Hall, LSM6DSO, leitor microSD | Módulo no carro |
| Ferramentas Rust | `espup`, `espflash` / `cargo-espflash`, `usbipd-win` (para ver o USB dentro do WSL) | Compilar e gravar firmware no ESP32 |
| Firmware (Rust) | `esp-hal`, `esp-hal-embassy`, `embassy-executor`, `embassy-time`, `embassy-sync`, `embedded-hal` 1.0 | Base do firmware assíncrono sem sistema operativo |
| Dados no firmware | `postcard` + `serde`, `heapless`, `crc`, `embedded-sdmmc` | Formato compacto, estruturas sem alocação, gravação no cartão |
| BLE | `trouble-host` (ou o BLE do `esp-wifi`) no módulo; `@capacitor-community/bluetooth-le` na app | Emparelhamento e descarga dos ficheiros |
| Segurança do dispositivo | Secure boot v2 e cifra da flash (ferramentas `espsecure` / `espefuse` do ESP-IDF), `ed25519-dalek` | Assinar firmware e impedir firmware não autorizado |
| Ingestão (Go) | `pgx` `CopyFrom`, TimescaleDB, URLs pré-assinados S3 | Inserir milhares de amostras de forma eficiente |
| Gráficos | uPlot | Séries longas de telemetria sobre os tempos de volta |

| ID | Cartão | O que fazer | Pronto quando | Estado |
| --- | --- | --- | --- | --- |
| F4-01 | Confirmar regulamento | Ler o regulamento das provas sobre equipamento a bordo e transmissão; registar em `events.event.telemetry_allowed` | Resposta escrita para treinos, mangas e finais | Por fazer |
| F4-02 | Encomendar o hardware | ESP32-S3 (2 unidades), SX1262 (2), MAX31855 + termopar K, sensor Hall + íman, LSM6DSO, leitor microSD, regulador, caixa | Peças recebidas | Por fazer |
| F4-03 | Rust embarcado: primeiros passos | Ler o livro "The Rust on ESP Book"; piscar um LED e ler um sensor com `esp-hal` + Embassy | Leitura de temperatura na consola a 5 Hz | Por fazer |
| F4-04 | Protocolo partilhado | Crate `firmware/protocol` (`no_std`, postcard): formato dos blocos no cartão com CRC, pacote LoRa resumido; usado no firmware e num leitor em Go | Teste de ida e volta Rust → Go passa | Por fazer |
| F4-05 | Aquisição em bancada | Tarefas Embassy por sensor, às frequências da especificação (§10.1); watchdog | 30 min em bancada sem perder amostras | Por fazer |
| F4-06 | Gravação em cartão | Escrita em blocos de 4 KiB, início e fim automáticos pela rotação | Cartão lido no computador com o leitor em Go | Por fazer |
| F4-07 | Emparelhamento e descarga por BLE | Emparelhamento por código da app (§5.3); descarga do ficheiro para o telemóvel | Ficheiro de uma sessão descarregado na app | Por fazer |
| F4-08 | `pitbox-ingest` | Upload por URL pré-assinado, validação, inserção com `COPY`, associação à sessão | Ficheiro de 20 min processado em menos de 5 min | Por fazer |
| F4-09 | Gráficos e alertas | uPlot sobre os tempos de volta; limites de temperatura, tensão e impacto | Alerta gerado num teste com dados sintéticos | Por fazer |
| F4-10 | Montagem no carro | Caixa, fixação anti-vibração, alimentação isolada do sinal dos servos; pesar | ≤ 40 g; carro comporta-se igual com o módulo desligado ou avariado | Por fazer |
| F4-11 | Segurança do dispositivo | Secure boot v2, cifra da flash, firmware assinado com Ed25519, anti-rollback | Firmware não assinado é recusado no arranque | Por fazer |
| F4-12 | **Gate v0.4** | 10 sessões de treino gravadas e vistas na app | Nenhuma falha que afete o controlo do carro; dados de todas as sessões completos | Por fazer |

## Fase 5 — v1.0 Produção

Objetivo: a plataforma corre na cloud, segura, monitorizada e com backups testados, pronta para outras equipas. Os cartões F5-04 a F5-09 podem ser feitos mais cedo se quiseres prática de infraestrutura entre fases.

**Tecnologias desta fase**

| Área | Tecnologias e bibliotecas | Para quê |
| --- | --- | --- |
| LoRa (Rust) | `lora-phy`, `chacha20poly1305` (RustCrypto) | Envio em tempo real autenticado e cifrado |
| Atualização de firmware | Partições OTA do ESP32 (`esp-bootloader-esp-idf` / `esp-storage`) | Atualizar o módulo com regresso à versão anterior |
| Infraestrutura | OpenTofu, Google Cloud (Cloud Run, base de dados gerida, Memorystore, Cloud Storage, Secret Manager, KMS) | Ambientes de staging e produção como código |
| Borda | Cloudflare (DNS, TLS, WAF, rate limiting), Cloudflare Pages | Proteger a API e servir a app web |
| Entrega | `ko`, Syft, Trivy, cosign, `slsa-github-generator`, Renovate | Imagens mínimas, SBOM, scanning, assinatura e proveniência |
| Observabilidade | OpenTelemetry Collector, Grafana Cloud (Prometheus, Tempo, Loki), Sentry | Traces, métricas, logs, alertas e erros da app |
| Carga e segurança | k6, ghz, OWASP ZAP, checklist OWASP ASVS 5.0 | Testes de carga e revisão de segurança |
| Lojas | Google Play Console, Apple Developer, runners macOS do GitHub Actions, fastlane | Publicar a app (iOS exige macOS) |

| ID | Cartão | O que fazer | Pronto quando | Estado |
| --- | --- | --- | --- | --- |
| F5-01 | Recetor LoRa na box | Segundo ESP32-S3 + SX1262 como ponte LoRa → BLE para a app | Pacotes recebidos a 1 Hz a 100 m numa pista | Por fazer |
| F5-02 | LoRa autenticado | ChaCha20-Poly1305 com chave de sessão do emparelhamento; sequência no nonce; rejeitar repetições; respeitar duty cycle da sub-banda e medir o tempo de antena real com o pacote final (ADR 0007) | Pacote repetido ou alterado é descartado num teste; tempo de antena medido abaixo de 10% a 1 Hz (ou alternativa do ADR 0007) | Por fazer |
| F5-03 | Atualização OTA assinada | Firmware descarregado do servidor pela app e passado por BLE; verificação de assinatura; regresso à versão anterior se o arranque falhar | Atualização interrompida a meio não estraga o módulo | Por fazer |
| F5-04 | Infraestrutura como código | OpenTofu: projetos de staging e produção, Cloud Run, base de dados gerida, Valkey, armazenamento, Secret Manager, KMS | `tofu apply` cria o ambiente de staging do zero; o timeout de pedido do serviço Cloud Run da API é de 60 min (acima do teto de 55 min dos streams no código; confirmar o máximo da plataforma) | Por fazer |
| F5-05 | Borda com Cloudflare | DNS, TLS, WAF, rate limiting (incluindo `/auth/*` por IP: o BFF não limita, ADR 0013); origem só aceita tráfego da Cloudflare | Pedido direto à origem é recusado | Por fazer |
| F5-06 | Pipeline de entrega | Build com `ko`, SBOM com Syft, Trivy, assinatura cosign, proveniência SLSA, deploy gradual com rollback (§14) | Imagem não assinada não chega a produção | Por fazer |
| F5-07 | Observabilidade e SLOs | OpenTelemetry para Grafana Cloud; dashboards por módulo; alertas dos SLOs (§13) com runbooks | Erro provocado em staging gera alerta no telemóvel | Por fazer |
| F5-08 | Backups e restauro | PITR, `pg_dump` diário para outra conta com bloqueio de objetos, teste de restauro mensal automático | Restauro completo feito e cronometrado (≤ 4 h) | Por fazer |
| F5-09 | Testes de carga | k6 e ghz em staging com 500 equipas simuladas | p95 ≤ 300 ms nas leituras e escritas | Por fazer |
| F5-10 | RGPD | Exportar e apagar conta na app, política de privacidade, termos, registo de tratamentos, procedimento de violação de dados | Exportação e apagamento testados de ponta a ponta | Por fazer |
| F5-11 | Revisão de segurança interna | Rever o threat model; correr OWASP ZAP contra staging; verificar ASVS nível 2 item a item | Lista de falhas corrigidas ou aceites por escrito | Por fazer |
| F5-12 | Teste de intrusão externo | Pedir a um colega do mestrado ou a uma empresa um teste à API, web e app; publicar `security.txt` | Relatório recebido e falhas críticas corrigidas | Por fazer |
| F5-13 | Publicação nas lojas | Contas Google Play e Apple Developer; testes fechados com 2–3 equipas amigas | App aprovada nas duas lojas | Por fazer |
| F5-14 | **Gate v1.0** | Uma prova com outra equipa a usar o PitBox além da vossa | Nenhum incidente de segurança ou perda de dados; SLOs cumpridos nesse fim de semana | Por fazer |

## Fase 6 — v1.1 Elétricos e outras escalas

Objetivo: servir equipas de elétricos (1/8 e-buggy, 1/10) e outras escalas/combustíveis sem
regressões no nitro, usando os catálogos e as costuras do F1-07a.

**Tecnologias desta fase**

| Área | Tecnologias e bibliotecas | Para quê |
| --- | --- | --- |
| Backend | `garage/api.go` (catálogo/snapshot), outbox/River da Fase 2 | Catálogo local, contadores por família |
| App | XState v5 (já na Fase 2), base local/SQLite | `EnergyModel` elétrico e modo offline |
| Firmware | Sensores de tensão/corrente, telemetria de ESC | Telemetria elétrica isolada do sinal de controlo |
| Testes | `fast-check` (já na Fase 2), Playwright em modo avião | Estratégia determinística e final offline |

| ID | Cartão | O que fazer | Pronto quando | Estado |
| --- | --- | --- | --- | --- |
| F6-01 | Templates de setup por escala e motorização | Templates versionados de `setup.field_definition` por `(scale, powertrain)`, ligados aos catálogos do F1-07a; fallback por família | Um carro nitro e um elétrico têm fichas diferentes; copiar um setup copia só os campos comuns; um template novo não exige código | Por fazer |
| F6-02 | Baterias, motores brushless e ESC na garagem | Tabelas de equipa para bateria, log de bateria, motor brushless e ESC; montagem com histórico; contadores e avisos | CRUD com testcontainers, isolamento e papéis; bateria não fica em dois carros; `retired` não monta; aviso de ciclos/resistência | Por fazer |
| F6-03 | Modo final para carros elétricos | `EnergyModel` elétrico: margem por volta, troca de bateria se o regulamento permitir, relatório pós-final | Previsão determinística; final elétrica sem reabastecimentos; funciona em modo avião | Por fazer |
| F6-04 | Telemetria para carros elétricos | Perfil de sensores por motorização: tensão/corrente, temperatura de motor/ESC, rotações; ingestão, gráficos e alertas | Bancada vs multímetro dentro da tolerância; protocolo nitro compatível; alerta de temperatura na app | Por fazer |
| F6-06 | Outras escalas e combustão | Ativar 1/10 nitro, 1/5 gasolina e afins; defaults por escala a partir do catálogo/configuração | Um 1/10 nitro e um 1/5 gasolina percorrem o fluxo completo; 1/8 nitro sem regressão | Por fazer |
| F6-05 | **Gate v1.1** | Uma equipa de elétricos usa a app num treino/prova real; a equipa nitro usa a mesma versão no mesmo período; um carro de outra escala/combustível também; problemas convertidos em issues | Nenhum setup/registo de bateria em papel; nenhuma regressão no modo final nitro; um 1/10 nitro usado num treino real sem regressões | Por fazer |

Issues: #115–#119 e #125 (F6-06). O Gate (#119) depende de F6-01..F6-04 e de F6-06.

## Cartões contínuos

Estes hábitos repetem-se em todas as fases; são eles que transformam o projeto num portfólio que se mostra numa entrevista.

- [ ] No fim de cada fase: rever o threat model ([`docs/threat-model/`](threat-model/README.md)) e atualizar os ADRs ([`docs/adr/`](adr/))
- [ ] No fim de cada fase: escrever um artigo curto no blog ou no `docs/` (ex.: "Como isolei equipas com RLS e FKs compostas", "Firmware Rust num carro de 1/8")
- [ ] Todas as semanas: deixar o Renovate atualizar dependências e rever os alertas de segurança
- [ ] Depois de cada prova: 15 minutos com o teu pai a anotar o que falhou ou fez falta na app, e criar cartões para isso
- [ ] Uma vez por mês: confirmar que o teste de restauro passou e que o CI noturno de fuzzing está verde
- [ ] Sempre que possível: ligar um cartão a um trabalho do mestrado (threat modeling, criptografia do LoRa, segurança da supply chain)
