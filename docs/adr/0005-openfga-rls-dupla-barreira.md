# 0005 — OpenFGA + RLS como dupla barreira de autorização

- **Estado:** Aceite (Nelson, 2026-10-11)
- **Data:** 2026-10-11 (decisão); registado em 2026-10-12
- **Cartão:** F0-07 (registo); origem: arquitetura §6

## Contexto

O maior risco de um produto multi-equipa é uma equipa ver ou alterar dados de outra (IDOR/BOLA, OWASP API1:2023). Uma só barreira, se tiver um bug, expõe tudo.

## Decisão

Duas barreiras independentes: (1) OpenFGA decide, por chamada, se o utilizador pode fazer a ação no recurso (`Check(user, relation, object)` antes de tocar nos dados); (2) Row-Level Security no PostgreSQL garante que nenhuma consulta devolve dados de outra equipa. A aplicação liga-se como `pitbox_app`/`pitbox_api` (sem `BYPASSRLS`) e cada transação faz `SET LOCAL app.team_id` e `app.user_id`. As FKs entre tabelas de equipa são compostas `(team_id, x_id)`. Um recurso de outra equipa responde `NOT_FOUND`, nunca `PERMISSION_DENIED`, para não permitir sondar IDs.

## Alternativas consideradas

| Alternativa | Porque não |
| --- | --- |
| Só verificações no código | Um bug esquecido expõe dados de todas as equipas |
| Só RLS | Não expressa papéis nem partilhas por recurso (links, convidados) |

## Consequências

- Custo: cada caso de uso faz `Check` e abre uma transação com contexto; o `db.WithTeamTx` centraliza-o.
- Há testes que provam a segunda barreira sozinha: `backend/db/tests/rls_and_constraints_test.sql` e `structure_test.sql` (toda a tabela com RLS e política), executados no CI pelo `make db-test` (F0-06).
- Cada endpoint precisa de um teste com um utilizador de outra equipa (`NOT_FOUND`) e com papel sem permissão (`PERMISSION_DENIED`).
- Um erro de contexto (`app.team_id` vazio) falha fechado: sem linhas visíveis.

## Emenda (2026-10-14, F1-04)

A decisão mantém-se. O F1-04 concretiza o `db.WithTeamTx` e fixa o que a base de dados faz por si.

- **Contexto por `set_config`, não por `SET LOCAL`.** `SET LOCAL` não aceita parâmetros (verificado: `PREPARE … AS SET LOCAL app.team_id = $1` dá erro de sintaxe). Usa-se `SELECT set_config('app.user_id', $1, true), set_config('app.team_id', $2, true)`, com o `is_local = true`: o valor dura até ao fim da transação e a ligação do pool nunca o leva ao pedido seguinte (verificado: depois do `COMMIT`, `current_setting('app.team_id', true)` é vazio). Um valor malformado dá erro (`invalid input syntax for type uuid`), nunca dados.
- **A pertença é verificada na própria base.** Antes de correr o caso de uso, no mesmo *round trip* do contexto, `WithTeamTx` lê `identity.membership` (com a equipa e o utilizador não apagados) e devolve o papel. Se não houver linha: `ErrNotMember`, que embrulha `apperr.ErrNotFound` e o interceptor de erros responde `NOT_FOUND`, igual para "equipa de outro", "não existe" e "apagada". Não substitui o OpenFGA (quem pode fazer o quê): é a fonte de verdade de quem pertence a que equipa, cobre o período até ao F1-05 e, depois dele, fecha a janela em que o OpenFGA, que é uma cópia sincronizada pelo worker, ainda tem um membro que já foi removido.
- **Papel da ligação verificado em cada ligação** (`AfterConnect`, lista de permitidos): `current_user` (não `session_user`: o migrador faz `SET role = pitbox_owner` em cada sessão) tem de ser membro de `pitbox_app` e não pode ser superutilizador, ter `BYPASSRLS` nem ser membro de `pitbox_owner` (o dono das tabelas é isento do RLS). Um URL com o papel errado deixa de funcionar em silêncio. A ligação também tem de **começar sem contexto** (`app.team_id` e `app.user_id` vazios): um URL com `options=-c app.team_id=…`, ou um `ALTER ROLE … SET app.team_id`, daria uma equipa a tudo o que corre fora do `WithTeamTx`.
- **Transações de leitura** (`WithTeamReadTx`) para os RPCs `NO_SIDE_EFFECTS`; **`WithUserTx`** só com `app.user_id` (listar as minhas equipas). Criar equipa **não** cabe em `WithUserTx`: as políticas de `team` e `membership` pedem o id da equipa nova no contexto no `INSERT`; é uma variante própria no F1-06.
- **A função não recebe o `pgx.Tx`**, só a interface estreita `db.DBTX` (a de `sqlc`, mais lote e cópia): não pode fazer `Commit`, `Rollback` nem abrir uma transação aninhada, e deixa de funcionar quando a transação acaba. `UserID` e `TeamID` são tipos distintos: trocá-los é erro de compilação.
- **Limite conhecido do desenho.** O contexto é uma variável de sessão: **qualquer SQL que corra como `pitbox_api` pode reescrevê-lo** (verificado: `set_config('app.team_id', <outra equipa>, true)` a meio da transação funciona; `TestKnownLimit…` documenta-o). A proteção é que nenhum SQL nosso se constrói com dados de fora (parâmetros e `sqlc`) e que **nada fora de `platform/db` pode escrever o contexto**: um teste de arquitetura recusa `set_config`, `SET LOCAL`, `SET ROLE`, `app.team_id`/`app.user_id` em *strings* Go e nos `.sql` do `sqlc` fora desse pacote, e recusa `Begin`/`BeginTx` e `pgxpool` fora dele. O teste avalia as expressões constantes inteiras (`"set_" + "config("`, constantes feitas de constantes), tira as aspas, o `||` e o espaço em branco antes de procurar, e procura nos `.sql` o ficheiro todo, comentários incluídos (um `--` dentro de uma *string* não é um comentário). **Não vê texto construído em tempo de execução** (`fmt.Sprintf`, `strings.Join`): a revisão de qualquer uso novo disso com SQL é a segunda linha. Endurecer isto (por exemplo, um contexto assinado) não é necessário agora; se um dia for, é um ADR novo.
- **Um `team_id` do cliente que o contrato não recusa não é um erro interno.** O protovalidate aceita o UUID nulo; `ParseTeamID` recusa-o com `ErrInvalidTeamID`, que embrulha `apperr.ErrInvalidArgument`, e o interceptor de erros responde `INVALID_ARGUMENT` com mensagem fixa (nível Info, não Error). O `ParseUserID` devolve só `ErrInvalidScope`: o id do utilizador vem da nossa sessão, e um inválido é um erro interno.
- **Auditoria na mesma transação** (`audit.RecordTeam`): a equipa e o autor vêm de `app.current_team_id()` e `app.current_user_id()` no próprio SQL, não de argumentos, para nenhum caso de uso auditar na equipa errada; sem contexto não escreve (`ErrNoContext`).


## Emenda (2026-10-19, F1-06)

A decisão mantém-se. O F1-06 acrescenta duas variantes de transação, porque criar uma equipa e aceitar um convite não cabem no `WithTeamTx` nem no `WithUserTx`:

- **`WithNewTeamTx`** (criar equipa): o próprio `platform/db` gera o UUIDv7 e define `app.user_id` e `app.team_id` antes de correr a função. Não há pertença para verificar — a equipa e a primeira `membership` são criadas na mesma transação. O chamador nunca escolhe o id da equipa.
- **`WithJoinTeamTx`** (aceitar convite): abre a transação só com `app.user_id`, corre o `join` (o `identity.accept_invitation`, `SECURITY DEFINER`, porque a RLS esconde o convite antes de a equipa ser conhecida), define `app.team_id` com a equipa devolvida e **só então** corre a verificação de pertença normal. Nunca existe um contexto de equipa sem pertença.
- **Uma exceção estreita, revista:** o `archtest` só permite chamar estas duas variantes em `internal/identity/app`; o `WithTeamTx` continua a ser o caminho normal. O `identity.accept_invitation` entra na lista exata de funções `SECURITY DEFINER` e só `pitbox_app` o executa.
- **Um só dono por equipa** é garantido na base (`membership_one_owner_uq`, índice único parcial por `team_id` onde `role = 'owner'`); a transferência despromove o dono antigo antes de promover o novo, na mesma transação.
- **Prova:** `backend/internal/platform/db/tx_test.go` (`TestNewTeamTx`, `TestJoinTeamTx`), `backend/internal/archtest`, `backend/db/tests/rls_and_constraints_test.sql` e `backend/internal/identity/app/integration_test.go`.

## Emenda (2026-10-25, F1-07)

A garagem usa `Check` na relação `team` (`member` para ler, `mechanic` para escrever) + RLS e FKs compostas. Não cria tuplos por carro, motor, pneu ou peça: esses recursos não têm partilha granular. O `type car` do modelo OpenFGA fica reservado para recursos com partilha; os setups (F1-08) podem usar tuplos por recurso. A decisão mantém as duas barreiras e evita um consumidor de tuplos por recurso no worker nesta fase.

## Emenda (2026-10-25, F1-08)

O F1-08 implementa os setups com o mesmo padrão da garagem: `Check` na relação `team` (`member` para ler, `mechanic` para escrever) + RLS e FKs compostas. Os tuplos por recurso do tipo `setup` ficam adiados até existir partilha direta (`shared_with`), que está fora de âmbito. O link público não passa pelo OpenFGA: é lido por `setup.open_share_link`, uma função `SECURITY DEFINER` estreita, com hash de token e só `pitbox_app` a executar. Quando a partilha direta chegar, um cartão próprio acrescenta `setup.team` à outbox e migra os setups existentes; não é necessária uma alteração estrutural à tabela.
