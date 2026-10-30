# 0023 — Extensões locais do inventário

- **Estado:** Aceite (Nelson, 2026-10-30)
- **Cartão:** F1-11b (#134)
- **Relacionado:** [ADR 0003](0003-app-offline-first-sync-proprio.md), [ADR 0018](0018-base-local-web.md), [ADR 0020](0020-gestao-online-equipas-convites.md), [ADR 0021](0021-specs-por-tipo-de-peca.md); especificação §4.4; arquitetura §3 e §8.

## Contexto

Os ecrãs de inventário (montar, contar, comprar) escrevem só na base local (F1-10); o sync é o F1-13. O desenho pede quatro coisas que o modelo do servidor (F1-07b/c/d) não tem ou que a base local não deve guardar:

1. a **loja** onde se comprou (pré-preenchida na compra seguinte);
2. o **autor** de cada movimento, por nome;
3. um **rascunho de contagem** que sobrevive a fechar a app;
4. **anular** um movimento, que não tem RPC: os movimentos são imutáveis e o sinal depende do motivo (uma compra nunca é negativa).

O ADR 0020 proíbe guardar nomes e emails de membros na base local (dados pessoais desnecessários para o modo de prova).

## Decisão

1. **Autor por id.** `stock_movement.created_by` guarda só o id do utilizador, como o `created_by` do servidor. A UI mostra «Tu» para o utilizador da sessão e, para os outros, o nome da cache de membros em memória quando existir, senão «Outro membro». Nenhum nome nem email é persistido.
2. **Loja só local.** `stock_movement.store` (texto, ≤ 80, anulável, só em compras) existe apenas na base local. No Push (F1-13) dobra-se na `note` («Loja: X · nota»). Se um relatório de custos (#135) precisar de agrupar por loja, propõe-se então um campo no servidor, com contrato e migração próprios.
3. **Rascunho de contagem só local.** Tabelas `inventory_count` (um rascunho por equipa: âmbito, início, `base_rowid`) e `inventory_count_line` (peça, valor contado). **Nunca** entram no sync. Aplicar a contagem é uma única transação que relê o stock atual, cria um movimento `adjustment` (contado − atual) por peça alterada e apaga o rascunho; os movimentos registados durante a contagem (`rowid > base_rowid`) entram no cálculo e a revisão avisa-os.
4. **Anular = `adjustment` inverso.** Cria um movimento `adjustment` de sinal contrário com a nota «Anula o movimento de <data>», mais a coluna local `reverts_movement_id` (índice único parcial: um movimento só se anula uma vez). O original fica intacto. Limitação: anular uma compra com preço não retira o preço da compra no relatório de custos (#135).
5. **Restrições locais iguais às do servidor** para os dados que sincronizam: preço só em `purchase` e ≥ 0; `loss_reason` obrigatório só em `lost`; `removal_destination`/`removal_reason` como `part_install_removal_ck`; `shopping_item` com `UNIQUE (team_id, part_id)`; movimentos imutáveis (`UPDATE` recusado por *trigger*; o `DELETE` só acontece por cascata quando a peça ou a equipa saem da base local).
6. **Custos por papel na camada de dados.** As leituras locais só selecionam `unit_cost_cents` quando o papel é dono ou mecânico; a UI esconde-os também. O servidor e o Pull (F1-13) continuam a ser o filtro real.

## Alternativas consideradas

| Alternativa | Motivo para rejeitar |
| --- | --- |
| Guardar o nome do autor em cada movimento | Dados pessoais persistidos no browser contra o ADR 0020; fica errado quando o membro muda de nome ou sai. |
| Loja como prefixo da `note` | Pré-preencher a última loja passaria a depender de ler texto livre; qualquer edição da nota quebra-o. |
| Loja já no servidor | Alarga o cartão a contrato, migração e testes de backend sem haver ainda um relatório que a use. |
| Rascunho de contagem em `localStorage` | Os dados de negócio vivem na base local; `localStorage` só guarda a língua (regra do projeto). |
| Anular com o mesmo motivo e sinal oposto | O servidor e a base local recusam (compra negativa, consumo positivo). |
| Anular apagando o movimento | Viola a imutabilidade e o Push de acrescento. |

## Consequências

- A migração local `0006` acrescenta colunas e tabelas só para a frente; nada do servidor muda (`buf breaking` e `make gen` ficam estáveis).
- O F1-13 tem de: dobrar `store` na `note` no Push; ignorar `inventory_count*` e `reverts_movement_id`; resolver nomes de autores só em memória.
- A anulação de compras com preço fica registada como limitação conhecida para o #135.
