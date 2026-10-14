# interceptors

Cadeia de interceptors Connect do servidor (`connect.ServerInterceptor`, ADR 0012), na ordem da arquitetura §4.4. O primeiro envolve todos os outros. A ordem está em `chain.go` (`build`) e é protegida por um teste.

| # | Interceptor | O que faz |
| --- | --- | --- |
| 1 | `Tracing` | Um span de servidor por RPC (`otelconnect`); não confia no `traceparent` do cliente |
| 2 | `Recovery` | Panic → `CodeInternal` com mensagem fixa; log com a stack e o `trace_id` |
| 3 | `Errors` | Erro que não é um `*connect.Error` nosso → `CodeInternal` com mensagem fixa; uma linha de log por RPC (sem corpos nem IP) |
| 4 | `Limits` | Deadline: 30 s nos unários, 55 min nos streams; um prazo menor do cliente mantém-se |
| 5 | *(F1-03)* | Autenticação |
| 6 | *(F1-04b)* | Rate limiting |
| 7 | `Validation` | protovalidate com o validador compilado no arranque (`NewValidator`) |
| 8 | `Audit` | `AuditMeta` no contexto das escritas |

O limite de **tamanho** (4 MiB por mensagem, também depois de descomprimir) e o corte de corpos lentos são do transporte: `httpserver.MountRPC`.

## Regras para quem escreve código de serviço

- **Erros para o cliente:** `connect.NewError(code, "mensagem fixa").WithCause(err)`. Só um `*connect.Error` escrito por nós chega ao cliente; qualquer outro erro sai como `Internal` / `internal error`. A mensagem tem de ser um literal ou uma constante do pacote; `internal/archtest` falha o `make test` se não for. Nunca `err.Error()`, `fmt.Sprintf` nem `connect.Errorf`.
- **Não encontrado:** um caso de uso devolve `apperr.ErrNotFound` (ou `db.ErrNotMember`, que o embrulha), embrulhado como quiser; o interceptor de erros responde `NOT_FOUND` com a mensagem fixa "not found" e a causa só vai para o log se for falha do servidor (aqui não é). Não se converte nos handlers.
- **Argumento inválido que o contrato não apanha** (por exemplo o UUID nulo): `apperr.ErrInvalidArgument` (que `db.ErrInvalidTeamID` embrulha) sai como `INVALID_ARGUMENT` com a mensagem fixa "invalid argument", registado como erro do cliente; nunca um `INTERNAL`.
- **Embrulhar erros:** com o nome da operação, nunca com valores do pedido (`fmt.Errorf("create team: %w", err)`). A causa vai para o log nas falhas do servidor, e pode ter dados pessoais se alguém lá os pôs.
- **Escritas e leituras:** uma leitura nova declara `option idempotency_level = NO_SIDE_EFFECTS;` no `.proto`. Sem isso conta como escrita e recebe `AuditMeta` (`AuditMetaFromContext`), que o caso de uso usa para escrever em `audit.audit_log` na mesma transação. O `request_id` vem do campo da mensagem.
- **Serviços novos:** acrescenta os tipos de pedido a `requestMessages()` em `internal/bootstrap/rpc.go`; o validador recusa tipos que não conheça e `TestRequestMessagesCoverEveryService` falha se faltar algum.
- **Sem `panic`** (o linter proíbe). O `Recovery` só protege a goroutine do handler: uma goroutine lançada por ti precisa do seu próprio `recover`.

## Fica de fora

- Pedidos para métodos desconhecidos respondem `Unimplemented` sem passar pela cadeia (sem log nem span): um scanner não consegue criar nomes de spans.
- O IP vem do peer TCP. Um cabeçalho (`CF-Connecting-IP`) só é aceite com `PITBOX_ORIGIN_CLOUDFLARE_ONLY=true` (F5-05).
- As métricas RED do `otelconnect` já são produzidas, mas só têm destino quando houver um `MeterProvider` (F5-07).
