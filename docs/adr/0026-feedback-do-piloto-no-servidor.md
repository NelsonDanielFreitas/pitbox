# 0026 — Feedback do piloto no servidor

- **Estado:** Aceite (Nelson, 2026-10-31)
- **Cartão:** F1-13
- **Substitui:** a decisão 1 do [ADR 0024](0024-extensoes-locais-dos-setups.md) («Feedback só local»)
- **Relacionado:** [ADR 0020](0020-gestao-online-equipas-convites.md), [ADR 0025](0025-formato-de-assinatura-e-cursor-do-sync.md); ameaças TM-SYNC-12.

## Contexto

O ADR 0024 guardou o feedback do piloto só na base local, à espera do F2, quando existir `events.session` e `events.driver_feedback` (que exige uma sessão). Na prática isso falhou duas vezes:

1. O que o piloto escreve não chega à base de dados: ao entrar com outra conta da mesma equipa, ou noutro dispositivo, o feedback não aparece, ao contrário de carros, setups, stock e montagens.
2. O logout apaga a base local sem avisar, por isso o feedback perde-se de vez.

## Decisão

1. **Tabela `setup.setup_feedback`** (equipa, `setup_id` + `version` com FK composta para `setup_version`, `author_id`, as oito escalas 1–5, `track_zone` ≤ 80, `note` ≤ 2000, `created_at`), com RLS, só de acrescento (*trigger* `forbid_mutation` + `REVOKE UPDATE, DELETE`) e *change log*. Pelo menos um valor ou a nota. Não substitui `events.driver_feedback`: o F2 liga as duas (o feedback de uma sessão continua a precisar de uma sessão).
2. **Entidade de sync `setup.setup_feedback`**, só `append`, sem versão (não há conflitos). O `id` do dispositivo é a chave primária (repetir é recusado). **O autor é sempre o utilizador autenticado**; um `author_id` no pedido é um campo desconhecido e é recusado. `created_at` do dispositivo só vale entre −90 dias e +5 min (como as outras horas).
3. **O piloto escreve.** Até agora o `Push` só aceitava dono e mecânico. Passa a aceitar qualquer membro (não convidados), e cada operação é verificada contra a sua entidade com o **papel atual**: dono e mecânico escrevem tudo; outro papel só as entidades cujo `Applier` implementa `syncapply.RoleGated` e o permite. O feedback permite dono, mecânico e piloto. Uma operação de um piloto noutra entidade é **rejeitada com `forbidden`** (fica registada), não falha o pedido. Registar um dispositivo e pedir o passe já eram permitidos a pilotos. `ResolveConflict` continua só do mecânico.
4. **Leitura:** todos os membros recebem todo o feedback da equipa no `Pull` (o `author_id` vai como id; os nomes resolvem-se só em memória, ADR 0020).
5. **Backfill** na base local (`0012_sync_feedback_backfill.sql`): o feedback que só existia localmente entra na fila (setups ativos, do mais antigo para o mais recente).
6. O travão do logout deixa de ter de tratar feedback: já sincroniza.

## Alternativas

| Alternativa | Motivo para rejeitar |
| --- | --- |
| Só travar o logout e manter o feedback local | O feedback continua preso a um dispositivo; o mecânico nunca o vê. |
| Esperar pelo F2 | Deixa o piloto sem forma de partilhar o que sentiu até haver sessões. |
| Reutilizar `events.driver_feedback` com uma sessão fictícia | Mistura dois conceitos e estraga o modelo do F2. |
| Dar ao piloto o papel de escrita completa no sync | Alarga demasiado a superfície: um piloto poderia editar carros e stock. |

## Consequências

- Migração `…f1-13-setup-feedback.sql` (expand: só uma tabela nova). `schema.sql` atualizado.
- A mudança do `Push` é de segurança: o teste `TestAuthorization` passa a provar que um piloto cria feedback mas não um carro, e que um convidado e uma equipa alheia continuam sem acesso.
- O `Pull` de feedback depende da versão do setup existir localmente; o `Pull` de `setup_version` só leva as últimas 20 versões, por isso feedback de versões mais antigas não aparece na app até haver outro mecanismo (limitação conhecida).
- O F2 mapeia o feedback de uma sessão para `events.driver_feedback`; este ADR não o antecipa.
