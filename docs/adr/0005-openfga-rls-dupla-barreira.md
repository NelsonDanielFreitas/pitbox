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
