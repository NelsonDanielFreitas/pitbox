# authz

- `model.fga` — modelo de relações OpenFGA (Arquitetura §6.1); fonte de verdade editada à mão.
- `model.json` — versão JSON do modelo, gerada por `fga model transform`. Usada pelos testes de integração para carregar o modelo numa store descartável; não editar à mão — regenerar quando `model.fga` mudar.
- `model.fga.yaml` — asserções testadas no CI com `fga model test`.

A wrapper do SDK fica em `backend/internal/platform/authz`.
