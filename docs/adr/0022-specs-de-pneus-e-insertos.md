# 0022 — Especificações de pneus e insertos

- **Estado:** Aceite (Nelson, 2026-10-30)
- **Cartão:** F1-07d (#136)
- **Relacionado:** [ADR 0021](0021-specs-por-tipo-de-peca.md); especificação §4.3 e §4.4; `garage.part.specs`, `garage.tyre_set`.

## Contexto

O [ADR 0021](0021-specs-por-tipo-de-peca.md) fixou que as `specs` de `tyre` e `insert` são `{}`. O F1-07d permite "colar" pneus e insertos em stock num `garage.tyre_set`, que tem `compound` e `insert_type`. Sem estas informações na peça, o composto e o tipo de inserto teriam de ser escritos à mão em cada colagem, e o conjunto não herdaria nada da peça comprada.

## Decisão

1. Acrescentar duas chaves opcionais ao conjunto fechado do ADR 0021, validadas no domínio com as mesmas regras (chave desconhecida, tipo errado ou valor fora do intervalo ⇒ `INVALID_ARGUMENT`; `{}` continua válido):

| `part_kind` | Chaves permitidas |
| --- | --- |
| `tyre` | `compound` (string, 1–40 caracteres) |
| `insert` | `insert_type` (string, 1–40 caracteres) |

2. "Colar conjunto" copia `compound` da peça `tyre` e `insert_type` da peça `insert` para o `tyre_set`, e `brand`/`description` para `make`/`model`. Qualquer valor enviado no pedido tem prioridade; o conjunto fica editável como hoje (`UpdateTyreSet`).
3. Sem `CHECK` por tipo na base e sem migração (como no ADR 0021). Os dados existentes não são revalidados enquanto não se mexer em `specs` ou `kind`.
4. Os limites de tamanho e profundidade, e as restantes chaves do ADR 0021, não mudam.

## Alternativas consideradas

| Alternativa | Motivo para rejeitar |
| --- | --- |
| Composto só no pedido, specs `{}` | Obriga a repetir o composto em cada colagem e não aproveita o que se sabe ao comprar a peça. |
| Colunas `compound`/`insert_type` em `garage.part` | Colunas esparsas só para dois tipos; as `specs` já existem para isso (ADR 0021). |
| Enum fechado de compostos | Cada fabricante usa nomes próprios; uma lista fechada recusaria dados legítimos. |

## Consequências

- O ADR 0021 mantém-se válido; este acrescenta duas linhas à sua tabela (a lista de chaves passa a ser a soma dos dois).
- A app (#134) pode oferecer "Composto" no formulário de pneus e "Tipo" no de insertos.
- Renomear ou retirar uma destas chaves exige migrar os dados existentes.
