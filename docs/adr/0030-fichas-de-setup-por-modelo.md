# 0030 — Fichas de setup por modelo de carro

- **Estado:** Aceite (Nelson, 2026-11-01)
- **Data:** 2026-11-01
- **Cartão:** F2-00 (#162); recolha do catálogo no F2-00b (#163); encolhe o F6-01
- **Relacionado:** [ADR 0016](0016-motorizacao-e-escala.md), [ADR 0024](0024-extensoes-locais-dos-setups.md), [ADR 0026](0026-feedback-do-piloto-no-servidor.md); `backend/internal/setup/domain/schema_v1.json`; arquitetura §6 e §8.2; exemplo em [`0030-exemplo-modelo-ficha.json`](0030-exemplo-modelo-ficha.json)

## Contexto

Hoje o setup é uma ficha única e genérica para 1/8 TT nitro (`setup/domain/schema_v1.json`, 82 campos em 11 grupos: `diffs`, `shocks`, `geometry`, ...). Cada versão guarda `data` com esses caminhos, e o servidor tem 7 colunas geradas a partir deles.

Na pista, as equipas e os pilotos usam a **ficha do fabricante** do seu carro (Mugen, Xray, Associated, Kyosho, Tekno, ...). Cada fabricante tem os seus grupos, a sua ordem e opções que só existem naquele modelo:
- **posições** escolhidas numa fila de furos ("Shock pos. ○○●", "Upper arm pin inside/outside", "Lower arm pos.");
- **peças opcionais** com referência, marcadas ou não ("T2128 (disk) ☐", "Chassis stiffener original ☒ / T2311 / T2236");
- **valores numéricos** com unidade (caster 2 mm, camber 1°, ride height 7 mm, óleo 1.000.000 cSt);
- **valores do kit** como referência ("Hex: kit", "Spring: Black (kit)").

O mecânico olha para o papel e quer ver a mesma coisa no telemóvel. A ficha genérica obriga-o a traduzir ("tower_position" em texto livre) e perde as opções do modelo. A app comercial So Dialed faz isto com um modelo por carro (mais de 200 modelos, com o setup do kit e as peças opcionais do fabricante), o que confirma que é o caminho esperado pelos utilizadores.

## Decisão

1. **Modelo de ficha** (`sheet template`) por modelo de carro e revisão (ex.: "Mugen MBX8R — ficha 2024"). Cada modelo tem **versões imutáveis**, como as versões de setup: um setup antigo aponta sempre para a versão da ficha com que foi feito e continua legível.
2. **Conteúdo de uma versão do modelo** (JSON validado, com limites):
   - grupos e ordem iguais aos da ficha do fabricante;
   - campos com `key` estável, `label` (pt/en), `type` (`number`, `int`, `choice` = posição ou opção única, `flag` = peça opcional sim/não, `text`), unidade, `min`/`max`/`step`, `options` para `choice`, referência da peça quando existe (`part_ref: "T2128"`);
   - `kit` = valor de fábrica de cada campo (opcional);
   - `canonical` = ligação opcional a um campo comum (`diffs.front_cst`, `shocks.front.oil_cst`, `transmission.spur_teeth`, ...). É isto que mantém as colunas geradas, as análises do F3 e a comparação entre carros diferentes.
3. **Versão de setup v2:** `data = { "template": {"id", "version"}, "values": { key: valor } }`, `schema_version = 2`. Só se guardam os campos preenchidos. O servidor valida contra a versão do modelo (tipo, intervalo, opção existente, nenhuma chave desconhecida).
4. **Ficha genérica como modelo:** o `schema_v1.json` passa a ser o modelo "Genérico 1/8 TT nitro v1". As versões v1 existentes não mudam (imutáveis) e leem-se como esse modelo. Um carro sem modelo próprio usa o genérico da sua escala e motorização (o "fallback" que o F6-01 previa).
5. **Origem dos modelos:**
   - **Catálogo global** (sem `team_id`, só de leitura, como as escalas e motorizações do ADR 0016), escrito em ficheiros JSON no repositório (`backend/internal/setup/templates/*.json`), revistos em PR e carregados por migração. Começa com o genérico e o modelo do vosso carro; os restantes modelos vêm da investigação do F2-00b (#163), porque há uma grande variedade de carros entre as equipas;
   - **Modelo da equipa** (`team_id`, RLS) criado a partir de um global ou do zero, para carros sem modelo no catálogo. O editor na app fica para mais tarde; na v0.2 chega duplicar um global e mudar campos.
6. **Carro ↔ modelo:** `garage.car.sheet_template_id` (opcional). Ao criar o carro, a app sugere o modelo pelo `make`/`model`.
7. **Comparação:** entre versões do mesmo modelo, por `key`; entre modelos diferentes, só pelos campos `canonical`, e a app diz quais ficaram de fora.
8. **Sync:** os modelos globais entram como catálogo (`catalog_version` no Pull, arquitetura §8.2 regra 6); os da equipa sincronizam como as outras entidades.
9. **Desenhos:** não copiamos os desenhos nem os PDFs do fabricante (direitos de autor). Guardamos a estrutura (nomes de campos, posições, referências de peças) e um link para o PDF oficial. Um esquema próprio, simples, só se for mesmo preciso.

## Alternativas

| Alternativa | Porque não |
| --- | --- |
| Manter a ficha genérica e usar `field_definition` por equipa | Não reproduz as posições nem as peças opcionais; cada equipa repete o trabalho para o mesmo carro |
| Fotografar a ficha em papel | Não compara, não analisa, não funciona com o modo final |
| Importar o PDF do fabricante automaticamente | Formatos diferentes por marca e direitos de autor; muito trabalho para pouco ganho |
| Modelos só por escala e motorização (F6-01 original) | Resolve elétrico vs nitro, mas não "ficha da Mugen vs da Xray" |

## Consequências

- O F6-01 encolhe: passa a ser "mais modelos e editor de modelos na app".
- Novo trabalho contínuo: escrever e rever um modelo por carro. Começar com o vosso; o F2-00b decide que modelos entram a seguir e de onde vem cada ficha.
- O validador passa a ler regras de dados (o modelo) em vez de um esquema fixo: o próprio modelo tem de ser validado (número máximo de campos e opções, `key` com regex, tamanho) e coberto por fuzzing.
- As colunas geradas passam a vir dos campos `canonical` (uma coluna gerada não consegue seguir um mapeamento por modelo; alternativa: preencher colunas normais na escrita, decidir no F2-00).
- Partilhar modelos entre equipas fica fora da v0.2 (nova superfície: conteúdo de uma equipa a aparecer noutra).

## Segurança

| STRIDE | Ameaça | Mitigação |
| --- | --- | --- |
| T | Modelo do catálogo alterado sem revisão | Só entra por PR com revisão (CODEOWNERS) e migração; imutável depois de publicado |
| D | Modelo ou setup com milhares de campos ou opções | Limites no validador do modelo e do setup; fuzzing |
| T | Setup gravado contra uma versão do modelo que não existe ou de outra equipa | FK composta para o modelo da equipa, ou referência ao catálogo global validada no servidor |
| I | Modelo da equipa visível a outra | RLS como nas outras tabelas da equipa |

## Adenda 2026-11-02 — escolha múltipla e disposição em tabela (F2-00, PR 5)

**Estado:** aceite por Nelson (2026-11-02). Não muda nenhuma decisão acima; acrescenta dois elementos ao formato do modelo, porque a ficha do Mugen MBX8 (Ver. 1801E, o carro do piloto) tem grupos de peças onde se marcam várias referências ao mesmo tempo e tabelas frente/centro/trás.

1. **Tipo de campo `multi`.** Várias opções de uma lista ao mesmo tempo (ex.: `E0160`, `E0159`, `E2218` num grupo de peças).
   - O valor é uma lista **não vazia** de opções distintas do campo. "Nada marcado" não se guarda (a chave é omitida), como nos outros tipos.
   - O servidor e a app **normalizam a lista pela ordem das `options`**: dois dispositivos que marcam as mesmas caixas por ordem diferente guardam o mesmo valor, e a comparação por índice e o `domain.Merge` não veem diferenças falsas.
   - Sem `part_ref` (as referências são as próprias opções), sem desenho, sem `canonical` (os campos canónicos do v1 são escalares). `kit` é uma lista de opções.
   - Os mesmos limites de opções do `choice` (2 a 20, 40 caracteres cada).
2. **Disposição em tabela (`table`) num grupo.** Dica de apresentação: colunas (2 a 4) e linhas (até 30) com a chave do campo em cada célula (`""` = célula vazia).
   - Cada campo do grupo aparece em **exatamente uma** célula e nenhuma célula nomeia um campo de outro grupo (senão um campo nunca seria mostrado).
   - Só tem chaves e etiquetas: não transporta valores, por isso não altera o que se guarda nem a validação do setup. Violações: `setup.template.table_layout` (e `setup.template.limits` para o número de colunas e linhas).
3. **Compatibilidade.** `format` continua `1`: um modelo sem `multi` nem `table` é igual ao de antes, e os setups v2 existentes não mudam. Um servidor ou app antigos recusam um modelo com `multi` ou `table` (campos desconhecidos, validação estrita): o catálogo novo só é publicado com a app nova.
4. **Fora do âmbito.** Continuamos sem importar nem alojar PDFs, desenhos ou logótipos do fabricante. Os desenhos continuam a ser o conjunto fechado (`hole_row`, `hole_grid`, `angle`, `length`, `part`). Uma folha interativa sobre o PDF do próprio utilizador, guardado só no telemóvel, seria um ADR à parte.
5. **Fonte do MBX8.** O site da Mugen estava indisponível a 2026-11-02; o `source_url` provisório aponta para a publicação pública da ficha na página da Mugen no Facebook, com a data da consulta, e troca-se pelo link oficial quando o site voltar.

| STRIDE | Ameaça | Mitigação |
| --- | --- | --- |
| D | Lista `multi` ou tabela enormes | Limites de opções, colunas e linhas no validador do modelo; `FuzzParseTemplate`; o valor conta para o limite de `MaxDataBytes` |
| T | Valor `multi` com opções repetidas, desconhecidas ou fora de ordem | Validação no servidor e na app; normalização pela ordem das opções; vetores partilhados Go ↔ TS |
| T | Tabela que esconde um campo ou aponta para outro grupo | Cada campo do grupo em exatamente uma célula (teste em Go e em TS) |

