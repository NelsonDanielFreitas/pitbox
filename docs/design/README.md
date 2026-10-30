# Referências de design da app

## F1-09b — Equipas e convites

- [Figma Make — PitBox Design System](https://www.figma.com/make/j8YibTN5pp722o063koWgf/PitBox-Design-System)
- Ecrãs relevantes no ficheiro Figma: `src/screens/Auth.tsx` (escolher equipa, criar equipa e convite) e `src/screens/Mais.tsx` (Equipa).
- Capturas existentes do F1-09: [`teams-390x844.png`](f1-09/teams-390x844.png) e [`team-home-390x844.png`](f1-09/team-home-390x844.png).

O F1-09b adapta o desenho aos componentes e textos pt-PT já existentes na app. A gestão de equipa fica online-only (ADR 0020); o token do convite usa `#t=` e é removido antes de qualquer chamada.

## F1-11b — Inventário: montar, contar e comprar

- Desenho: [Figma Make — PitBox Design System](https://www.figma.com/make/j8YibTN5pp722o063koWgf/PitBox-Design-System), ecrã `src/screens/GaragemStock.tsx` (dados em `src/data/stock.ts`).
- Capturas a 390×844, geradas pelo e2e (`PITBOX_CAPTURE=1 pnpm e2e inventory`): [`part-form`](f1-11b/part-form-390x844.png), [`part-detail`](f1-11b/part-detail-390x844.png), [`mount-sheet`](f1-11b/mount-sheet-390x844.png), [`unmount-sheet`](f1-11b/unmount-sheet-390x844.png), [`count-scope`](f1-11b/count-scope-390x844.png), [`count`](f1-11b/count-390x844.png), [`count-review`](f1-11b/count-review-390x844.png) e [`shopping`](f1-11b/shopping-390x844.png).

Diferenças deliberadas em relação ao desenho: as especificações por tipo seguem os ADR 0021/0022 (não as do Figma); anular um movimento cria um ajuste inverso (ADR 0023), porque uma compra nunca pode ser negativa; a unidade `l` mostra-se «L»; o texto partilhado da lista de compras não leva custos quando o papel não os vê. O «Colar conjunto» e «A partir do stock» (F1-07d) não estão no desenho e ficam para um cartão próprio.

## F1-12 — Ecrãs de setup

- Sem desenho no Figma para estes ecrãs: seguem os componentes e os textos pt-PT da app (especificação §5; ADR 0024).
- Capturas a 390×844, geradas pelo e2e (`PITBOX_CAPTURE=1 pnpm e2e setups`): [`list-empty`](f1-12/list-empty-390x844.png), [`new`](f1-12/new-390x844.png), [`edit`](f1-12/edit-390x844.png), [`detail-v2`](f1-12/detail-v2-390x844.png), [`detail-changes`](f1-12/detail-changes-390x844.png), [`compare`](f1-12/compare-390x844.png), [`feedback-sheet`](f1-12/feedback-sheet-390x844.png) e [`list`](f1-12/list-390x844.png).

Decisões: a ficha é um conjunto de grupos recolhíveis com a unidade sempre à vista e o valor anterior como pista; o tipo de cada diferença é uma palavra com sinal, nunca só cor; o feedback do piloto é local e ligado à versão (ADR 0024); o esquema do servidor manda onde a especificação diverge (óleo em cSt inteiro, agulhas em voltas, um só campo de limitadores). O formulário não guarda rascunho se a página fechar a meio.

## Rever no browser

Validar os ecrãs móveis a 390×844, em pt-PT e inglês, com foco visível, teclado, axe e alvos de toque acessíveis. Não colocar tokens de convite em capturas, comentários, issues ou logs.
