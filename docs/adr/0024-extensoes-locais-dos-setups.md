# 0024 — Extensões locais dos setups

- **Estado:** Aceite (Nelson, 2026-10-30). A decisão 1 («Feedback só local») foi substituída pelo [ADR 0026](0026-feedback-do-piloto-no-servidor.md).
- **Cartão:** F1-12 (#40)
- **Relacionado:** [ADR 0003](0003-app-offline-first-sync-proprio.md), [ADR 0018](0018-base-local-web.md), [ADR 0020](0020-gestao-online-equipas-convites.md), [ADR 0023](0023-extensoes-locais-do-inventario.md); especificação §5; arquitetura §3 e §8.

## Contexto

O backend dos setups (F1-08) tem esquema JSON v1, versões imutáveis, `Compare` e cópia de outro setup. Os ecrãs de setup (F1-12) escrevem só na base local; o sync é o F1-13. O desenho pede coisas que o servidor não tem ou que a base local ainda não garante:

1. **Feedback do piloto** por versão de setup. O servidor só tem `events.driver_feedback`, que exige uma sessão (`session_id NOT NULL`, F2).
2. **Imutabilidade das versões** também na base local: hoje `setup_version` aceita `UPDATE`.
3. **Autor** de setups e versões, e **concorrência otimista** em `setup` (nome e notas), como no inventário.
4. **Catálogo de campos** (tipos, intervalos, unidades) para formulário e validação, sem o duplicar à mão do esquema do servidor.

## Decisão

1. **Feedback só local.** Tabela `setup_feedback` (`team_id`, `id`, `setup_id`, `version`, `author_id`, oito escalas `smallint` 1–5 anuláveis, `track_zone` ≤ 80, `note` ≤ 2000, `created_at`), com FK composta para `setup_version (team_id, setup_id, version)` e restrição «pelo menos um valor ou a nota». Nunca entra no sync do F1-13; o F2 mapeia-a para `events.driver_feedback` quando existir sessão.
2. **Versões imutáveis localmente.** *Trigger* recusa `UPDATE` em `setup_version` (o `DELETE` só acontece por cascata). Gravar é sempre uma versão nova `base + 1`, inserida só se `setup.current_version = base`, na mesma transação que sobe `current_version`; base desatualizada é conflito `stale`.
3. **Autor por id e `local_rev`.** `created_by` (id do utilizador, sem nome nem email, ADR 0020/0023) em `setup` e `setup_version`; `setup.local_rev` para concorrência otimista local de nome e notas. Ambos só locais.
4. **Catálogo em TypeScript** (`features/setups/domain/catalogue.ts`), espelho do `schema_v1.json` do backend, com **teste de deriva** que lê o JSON e falha se divergirem. Onde a especificação difere do servidor, manda o servidor (óleo em cSt inteiro, agulhas em voltas, um só campo de limitadores). Chaves desconhecidas preservam-se ao gravar.
5. **Validação igual à do servidor**: mesmos `rule_id` (`setup.data.*`), limites de 64 KiB e profundidade 8, nunca `null`.
6. **Papéis**: ler = dono, mecânico, piloto; escrever = dono, mecânico; feedback = os três. O servidor continua a ser a barreira real.

## Alternativas consideradas

| Alternativa | Motivo para rejeitar |
| --- | --- |
| Feedback já no servidor | `driver_feedback` exige sessão; criar API e migração alarga o cartão e antecipa o F2. |
| Feedback dentro de `setup_version.data` | Viola a imutabilidade e mistura medições com a ficha. |
| Gerar o catálogo a partir do JSON em tempo de build | Mais ferramentas para um ficheiro pequeno; o teste de deriva dá a mesma garantia. |
| Editar a versão atual in-place | Perde o histórico e o «o que mudou desde a anterior». |

## Consequências

- Migração local `0007` só para a frente; nada do servidor muda (`buf breaking` e `make gen` ficam estáveis).
- O F1-13 tem de: enviar `CreateVersion` com `base_version` e `request_id`; ignorar `setup_feedback`, `local_rev` e `created_by` locais onde o servidor já tem o seu; tratar `ABORTED` como conflito.
- O F2 tem de mapear `setup_feedback` para `driver_feedback` (associando a sessão).
- Fora do âmbito do F1-12: condições da sessão (F2), favoritos por pista, PDF, importar/exportar JSON, links de partilha na UI, campos personalizados, notas de voz, setup «de casa», sync.
