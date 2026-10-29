# 0021 — Especificações das peças por tipo

- **Estado:** Aceite (Nelson, 2026-10-29)
- **Cartão:** F1-07c (#133)
- **Relacionado:** [ADR 0016](0016-escala-e-motorizacao-extensiveis.md); especificação §4.4; `garage.part.specs`.

## Contexto

`garage.part.specs` é um `jsonb` de objeto livre (limite de 16 KiB e profundidade 4, validados no domínio). Não há nenhuma regra sobre que chaves existem para cada `part_kind`, por isso o protótipo e o backend divergem (texto livre vs. chaves como `viscosity_cst`) e os relatórios e a pesquisa não podem confiar nos nomes nem nos tipos. Aceitar chaves arbitrárias também deixa um cliente com defeito guardar lixo dentro do limite de tamanho.

## Decisão

1. As `specs` de uma peça são validadas **no domínio** (`garage/domain`) segundo o `part_kind`, sempre que a peça é criada ou atualizada, **incluindo quando só o `kind` muda** (as `specs` existentes têm de caber no novo tipo).
2. Cada tipo tem um conjunto fechado de chaves opcionais, com tipo e intervalo. Uma chave desconhecida, de tipo errado ou fora do intervalo é recusada com `INVALID_ARGUMENT`. `{}` é sempre válido.
3. Os limites de tamanho e profundidade atuais mantêm-se. Não há `CHECK` por tipo na base (o esquema JSON evolui sem migração); a base continua a garantir só `jsonb_typeof(specs) = 'object'`.
4. Os valores são números ou strings curtas (máx. 40 caracteres); nada de objetos aninhados.

| `part_kind` | Chaves permitidas |
| --- | --- |
| `shock_oil`, `diff_oil` | `viscosity_cst` (número > 0, ≤ 100 000) |
| `fuel` | `nitro_pct` (0–40), `oil_pct` (0–30) |
| `glow_plug` | `heat_range` (string curta) |
| `spring` | `rate` (string curta) |
| restantes (`clutch_*`, `bearing`, `conrod`, `piston_sleeve`, `air_filter`, `tyre`, `insert`, `body`, `wing`, `other`) | nenhuma (`{}`) |

5. Acrescentar uma chave ou um tipo é uma alteração de código no domínio, com teste, sem migração nem mudança de contrato. Retirar ou renomear uma chave exige migrar os dados existentes.

## Alternativas consideradas

| Alternativa | Motivo para rejeitar |
| --- | --- |
| Manter `specs` livre | Mantém a divergência e impede relatórios por viscosidade, %nitro, etc. |
| `CHECK` por tipo na base | Cada chave nova exigiria migração; a validação de domínio chega e dá erros melhores. |
| JSON Schema em ficheiros | Mais uma dependência e um segundo sítio com regras, para 4 tipos com chaves. |
| Colunas tipadas por tipo | Tabelas esparsas ou herança; desproporcionado para atributos opcionais. |

## Consequências

- A app e os testes que enviavam `specs` livres têm de usar estas chaves (hoje nenhum teste do backend define `specs`; a app não tem UI para `specs`).
- Dados existentes com chaves fora da lista não são migrados (não há produção); uma atualização que não mexa em `specs` nem em `kind` não os revalida.
- A lista de chaves é a fonte de verdade para os ecrãs de peças (#134).
