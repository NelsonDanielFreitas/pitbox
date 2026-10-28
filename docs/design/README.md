# Referências de design da app

## F1-09b — Equipas e convites

- [Figma Make — PitBox Design System](https://www.figma.com/make/j8YibTN5pp722o063koWgf/PitBox-Design-System)
- Ecrãs relevantes no ficheiro Figma: `src/screens/Auth.tsx` (escolher equipa, criar equipa e convite) e `src/screens/Mais.tsx` (Equipa).
- Capturas existentes do F1-09: [`teams-390x844.png`](f1-09/teams-390x844.png) e [`team-home-390x844.png`](f1-09/team-home-390x844.png).

O F1-09b adapta o desenho aos componentes e textos pt-PT já existentes na app. A gestão de equipa fica online-only (ADR 0020); o token do convite usa `#t=` e é removido antes de qualquer chamada.

## Rever no browser

Validar os ecrãs móveis a 390×844, em pt-PT e inglês, com foco visível, teclado, axe e alvos de toque acessíveis. Não colocar tokens de convite em capturas, comentários, issues ou logs.
