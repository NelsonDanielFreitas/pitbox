# 0015 — Outbox com poller próprio até ao F2-04

- **Estado:** Aceite (Nelson, 2026-10-19)
- **Data:** 2026-10-19
- **Cartão:** F1-05b
- **Relacionado:** [ADR 0005](0005-openfga-rls-dupla-barreira.md) (OpenFGA + RLS), [ADR 0001](0001-monolito-modular-go.md) (monólito modular), arquitetura §2/§3/§6.1.

## Contexto

O F1-05 trouxe o modelo, as asserções e o cliente OpenFGA, mas não o consumidor da outbox: `platform.outbox` existe, `cmd/worker` está vazio e `identity.membership` não chega aos tuplos. Sem esse consumidor, um membro novo não consegue agir e um membro removido mantém o acesso — a dupla barreira do ADR 0005 fica incompleta.

Os requisitos do consumidor deste cartão são estreitos:

- Um **só tipo de evento** (`membership.changed`) e um **só consumidor** (o worker de identidade).
- **Ordem por chave**, não ordem global: eventos do mesmo membro têm de ser aplicados pela ordem em que foram escritos; eventos de membros diferentes são independentes.
- Um evento com problemas não pode bloquear a fila inteira (nem membros de outras equipas).
- Funcionar com **várias instâncias** do worker, sem processar o mesmo evento duas vezes.
- Não fazer I/O de rede (OpenFGA) com locks de base de dados presos.
- Perder um evento é inaceitável; reaplicá-lo não pode mudar o resultado (os eventos são de **estado**, não delta).

O plano de trabalho e a arquitetura listam hoje `riverqueue/river` nas tecnologias da Fase 1 (§2, §3 e comentário de `platform.outbox`), mas o River não é necessário para este consumidor. O River traz as suas próprias migrações e tabelas, e só se justifica quando houver tarefas agendadas, retries com política rica e vários tipos de job — o que acontece no F2-04 (contadores de manutenção e avisos).

## Decisão

Usar um **poller próprio, sem dependência nova**, até ao F2-04.

1. **Eventos de estado.** `event_type = "membership.changed"`, `aggregate = "identity.membership"`, `team_id = equipa`, `aggregate_id = user_id`, `payload = {team_id, user_id, role_after}`; `role_after = null` na remoção. Um só tipo cobre criar, mudar papel, remover, sair e transferir.
2. **Chave de ordem.** A chave é `(team_id, aggregate, aggregate_id)`. O claim só pega num evento se não existir outro mais antigo e por publicar com a mesma chave. A ordem é **por chave**, não global.
3. **Claim com lease, em autocommit.** `UPDATE ... SET locked_until = now() + interval '30 seconds', attempts = attempts + 1 WHERE id IN (SELECT ... WHERE published_at IS NULL AND (locked_until IS NULL OR locked_until < now()) AND NOT EXISTS (... o2.id < o.id ...) ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED) RETURNING ...`. O statement é atómico e termina antes de qualquer chamada ao OpenFGA.
4. **Backoff por evento.** Numa falha, `locked_until = now() + min(2^attempts × 2 s, 5 min)`. Os eventos seguintes da mesma chave ficam à espera (o `NOT EXISTS` continua a vê-los como bloqueados); as outras chaves continuam.
5. **Aplicação idempotente.** `authz.SetTeamRole` faz um único `Write` que apaga `owner`/`mechanic`/`pilot` diferentes do papel e escreve o papel (nenhuma relação para `guest` ou `null`), com `on_duplicate: ignore` e `on_missing: ignore`. Reaplicar o mesmo evento não muda nada.
6. **Marcação.** Só depois de aplicar com sucesso é que `published_at = now(), locked_until = NULL`; os não aplicados ficam por publicar.
7. **Índice.** `(team_id, aggregate, aggregate_id, id) WHERE published_at IS NULL` suporta o claim e o `NOT EXISTS`.

O River passa a ser adotado no **F2-04** para as tarefas agendadas e os contadores de manutenção. A arquitetura e o comentário de `platform.outbox` são corrigidos para refletir esta decisão.

## Alternativas consideradas

| Alternativa | Porque não (agora) |
| --- | --- |
| River já no F1-05b | Dependência nova, migrações próprias e superfície operacional (dashboard, jobs, políticas de retry) sem necessidade: um consumidor, um tipo de evento. O plano revisto coloca-o no F2-04. |
| `LISTEN/NOTIFY` | Não é durável: se o worker estiver em baixo, o evento perde-se. Não dá retry nem lease, e não funciona com várias instâncias sem trabalho extra. |
| Ordem global (`ORDER BY id` apenas) | Um evento falhado com backoff deixa de aparecer no claim e o evento seguinte passa-lhe à frente — exatamente a reordenação que os eventos de estado não podem ter. |
| Eventos delta (`membership.added`/`removed`) | Um "added" reaplicado depois de um "removed" devolve o acesso. Os eventos de estado tornam a reaplicação inofensiva. |
| Claim dentro de uma transação longa | Prenderia locks durante a chamada ao OpenFGA; com o OpenFGA lento, a outbox e os writers ficariam bloqueados. |

## Consequências

- Sem dependência nova no backend; o worker é um binário Go próprio (`cmd/worker`), corre com `make run-worker`.
- O `pitbox-api` continua a escrever os eventos na outbox dentro da transação do caso de uso; o worker é o único consumidor.
- A ordem é por chave: um membro com problemas não bloqueia os outros. O preço é que o claim precisa do `NOT EXISTS` e do índice composto.
- **Dívida registada:** reconciliação periódica de tuplos (varrer `identity.membership` e corrigir o OpenFGA) no F5. A outbox garante o caminho normal; a reconciliação cobre perdas catastróficas (restauro de backup, bug no worker).
- **Dívida registada:** purga das linhas já publicadas da outbox no F2 (retenção e operação).
- **Reavaliar no F2-04:** quando entrarem tarefas agendadas/retries ricos, decidir se o River substitui o poller ou coexiste.
- O alerta é uma métrica/log de tentativas (a partir de 5) com runbook; a configuração do alerta no fornecedor de métricas entra com a infraestrutura.

## Prova

- `backend/internal/platform/outbox/worker_test.go`: evento aplicado → `Check` reflete o papel; remoção → acesso desaparece; reaplicar não muda nada; A falha e B da mesma chave não passa antes de A, C de outra chave passa; duas instâncias não repetem; `SIGTERM` não perde eventos.
- Testes de mutação: retirar o `NOT EXISTS`/`ORDER BY` ou a paragem no erro faz falhar um teste.
- `backend/internal/platform/authz/authz_test.go`: `SetTeamRole` cobre as quatro transições e a remoção.
