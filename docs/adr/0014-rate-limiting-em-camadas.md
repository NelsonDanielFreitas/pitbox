# 0014 — Rate limiting em camadas

- **Estado:** Aceite (Nelson, 2026-10-16)
- **Data:** 2026-10-16
- **Cartão:** F1-04b
- **Relacionado:** [ADR 0012](0012-connect-v2-antes-do-f1-02.md) (Connect v2), arquitetura §4.4, threat model TM-API-01b.

## Contexto

Sem limites, um utilizador autenticado (ou uma sessão comprometida) ou um cliente anónimo pode esgotar a API ou a base de dados para todas as equipas. O OWASP API4:2023 classifica isto como *Unrestricted Resource Consumption*; o threat model do PitBox regista-o como TM-API-01b.

Os requisitos são:

- Limitar por **utilizador** e por **equipa**, para uma equipa barulhenta não degradar as outras.
- Rejeitar pedidos acima da quota **antes** de gastar CPU em protovalidate ou no caso de uso.
- Continuar a proteger a API quando o Valkey (a cache/distribuição partilhada) estiver indisponível.
- Endpoints públicos sem sessão devem falhar fechados (`UNAVAILABLE`) quando o limite global não está disponível.
- Nunca revelar quotas, chaves nem estado interno ao cliente.

## Decisão

Implementar rate limiting em **três camadas**, no lugar 6 da cadeia de interceptors Connect (depois da autenticação, antes da validação):

1. **Token bucket local em memória** (`internal/platform/ratelimit/local.go`)
   - Sempre ativo, por utilizador/equipa.
   - Mais largo do que o limite distribuído do Valkey, para não rejeitar pedidos que o Valkey aceitaria.
   - Protege a instância quando o Valkey está em baixo.

2. **GCRA atómico no Valkey** (`internal/platform/ratelimit/valkey.go`)
   - Chaves `pitbox:rl:user:<id>:<read|write>`, `pitbox:rl:team:<id>:<read|write>`, `pitbox:rl:public:<hash>:<read|write>`.
   - Script Lua atómico implementa Generic Cell Rate Algorithm.
   - Escritas (RPCs sem `NO_SIDE_EFFECTS`) têm quotas mais apertadas do que leituras.

3. **Circuit breaker à volta do Valkey** (`internal/platform/ratelimit/breaker.go`)
   - Abre após 5 falhas consecutivas; half-open após 5 s.
   - Quando aberto, pedidos autenticados recaem no limite local; pedidos públicos falham fechados.
   - Métrica OpenTelemetry do estado do breaker.

### Extração do `team_id` no interceptor

O `team_id` vem no corpo do pedido Protobuf. No lugar 6 a mensagem ainda não foi descodificada, por isso o interceptor:

1. Deriva o nome do tipo de pedido a partir do nome do procedimento Connect (`/pkg.Service/Method` → `pkg.MethodRequest`).
2. Procura o descritor em `protoregistry.GlobalFiles`.
3. Se o descritor tiver um campo `team_id`, faz *peek* da primeira mensagem com `dynamicpb.NewMessage` e aplica o limite de equipa.
4. O `peekingStream` repete a mensagem para o resto da cadeia, para que protovalidate e o handler a recebam.

### Configuração

Variáveis de ambiente `PITBOX_RL_*` com valores por omissão:

| Variável | Valor | Nota |
|---|---|---|
| `PITBOX_RL_USER_READ_RPS` | 20 | leituras por utilizador |
| `PITBOX_RL_USER_READ_BURST` | 30 | burst de leitura por utilizador |
| `PITBOX_RL_USER_WRITE_RPS` | 5 | escritas por utilizador |
| `PITBOX_RL_USER_WRITE_BURST` | 10 | burst de escrita por utilizador |
| `PITBOX_RL_TEAM_READ_RPS` | 100 | leituras por equipa |
| `PITBOX_RL_TEAM_READ_BURST` | 150 | burst de leitura por equipa |
| `PITBOX_RL_TEAM_WRITE_RPS` | 20 | escritas por equipa |
| `PITBOX_RL_TEAM_WRITE_BURST` | 40 | burst de escrita por equipa |
| `PITBOX_RL_PUBLIC_RPS` | 10 | endpoints públicos |
| `PITBOX_RL_PUBLIC_BURST` | 20 | burst de endpoints públicos |
| `PITBOX_RL_VALKEY_TIMEOUT` | 50 ms | deadline curto para o GCRA |

Validação: `0 < RPS ≤ 1000`, `Burst >= RPS`, `ValkeyTimeout ∈ (0, 200ms]`. O teto de 1000 RPS vem da precisão em milissegundos do script Lua GCRA; se no futuro forem precisas quotas superiores, o script passa para microssegundos.

O limite local é **2× o limite do Valkey** (multiplicador fixo). O Valkey continua a ser a camada restritiva em funcionamento normal; o local é apenas rede de segurança.

### Erros

`ResourceExhausted` com a mensagem fixa `"rate limit exceeded"`. O cliente nunca vê quotas, chaves nem estado do circuit breaker. Falhas do Valkey são `UNAVAILABLE` apenas para pedidos públicos (fail closed); pedidos autenticados mantêm o limite local.

## Alternativas consideradas

| Alternativa | Porque não |
|---|---|
| Apenas token bucket local | Não partilha quota entre instâncias do `pitbox-api`; uma equipa com múltiplas instâncias multiplicava a quota |
| Apenas Valkey | Se o Valkey falhasse, a API ficaria desprotegida ou indisponível para todos |
| Biblioteca pronta (throttled, tollbooth) | Adiciona dependência; a ACL do Valkey e os requisitos de chave/fail closed justificam o código próprio, que é pequeno |
| Rate limiting por IP no BFF | Por IP é frágil (NAT, VPN) e não isola equipas; fica para a borda (F5-05) |
| Team limit depois da validação | Mais simples, mas desvia da arquitetura §4.4 e do objetivo de rejeitar antes de gastar CPU |

## Consequências

- O `pitbox-api` requer Valkey no arranque, mesmo sem autenticação configurada, porque o rate limiting usa Valkey.
- O `bootstrap/api.go` cria o cliente Valkey antes da stack de autenticação e partilha-o com ela e com o limiter.
- A ACL do utilizador `pitbox_api` no compose já inclui `~pitbox:rl:*`.
- Novos RPCs com `team_id` no pedido são automaticamente limitados por equipa; os sem `team_id` são limitados só por utilizador.
- Testes de integração usam `testutil.StartValkey`; sem Docker saltam, exceto no CI (`PITBOX_REQUIRE_DOCKER=1`).
- O pacote `auth/principal` foi criado para evitar ciclo de imports entre `auth` e `interceptors`.

## Prova

- `backend/internal/platform/ratelimit/*_test.go`: token bucket local, circuit breaker, GCRA no Valkey, isolamento user/team, comportamento quando o Valkey falha.
- `backend/internal/platform/interceptors/ratelimit_test.go`: rejeição por utilizador, rejeição por equipa (com peek dinâmico), fail closed em pedidos públicos, mensagem fixa.
- `backend/internal/platform/config/config_test.go`: validação das variáveis `PITBOX_RL_*`.
- `backend/internal/platform/interceptors/chain_test.go`: ordem da cadeia com `ratelimit` no lugar 6.
