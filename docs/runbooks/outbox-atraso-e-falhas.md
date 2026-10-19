# Runbook — outbox atrasada ou eventos a falhar

- **Alerta:** log `outbox event failed` com `attempts >= PITBOX_OUTBOX_ALERT_ATTEMPTS` (5 por omissão), ou métrica `outbox.failed` a subir / `outbox.applied` parada.
- **Impacto:** os tuplos do OpenFGA ficam atrasados em relação a `identity.membership`. Um membro novo pode não conseguir agir; um membro removido pode manter o acesso até o evento ser aplicado. A RLS continua a bloquear dados de outras equipas, mas não os papéis dentro da equipa.
- **Cartão:** F1-05b (ADR 0015).

## 1. Confirmar o atraso

```sql
-- Eventos por publicar e idade do mais antigo (sem dados pessoais).
SELECT count(*) AS pending,
       min(created_at) AS oldest,
       max(attempts) AS max_attempts
FROM platform.outbox
WHERE published_at IS NULL;

-- Um evento em backoff bloqueia os seguintes da mesma chave (team_id + aggregate + id).
SELECT id, aggregate, event_type, attempts, locked_until, created_at
FROM platform.outbox
WHERE published_at IS NULL
ORDER BY id
LIMIT 50;
```

Verificar se o processo está vivo e a chegar ao OpenFGA:

```bash
make run-worker          # local
# logs JSON: "outbox event failed", "outbox batch failed", "openfga connected"
```

## 2. Causas prováveis

| Sintoma | Causa | Ação |
| --- | --- | --- |
| `reason=apply failed`, OpenFGA inacessível | rede, credenciais, store/model errados | validar `PITBOX_OPENFGA_*`, `curl` ao endpoint; o worker continua a tentar |
| `reason=unknown event` | evento de outro módulo sem consumidor | não apagar o evento; registar o cartão do consumidor e filtrar o claim |
| `reason=invalid payload` | evento escrito com payload errado | investigar o produtor; corrigir o evento com um novo evento de estado (não editar a outbox) |
| `outbox batch failed` repetido | base de dados indisponível ou papel errado | verificar `PITBOX_DATABASE_URL` e o `verifyRole` do pool |
| `attempts` a subir sem erro de rede | tuplo inválido para o modelo (ex.: relação inexistente) | comparar `event_type`/`role_after` com `backend/authz/model.fga` |

## 3. Recuperação

1. **Não apagar nem editar eventos da outbox.** Os eventos são de estado: a correção é produzir o estado atual outra vez (nova linha), não alterar a história.
2. Corrigir a causa (OpenFGA, credenciais, rede) e deixar o backoff expirar. O teto é 5 min.
3. Para forçar uma tentativa imediata de um evento específico, depois de corrigida a causa:

```sql
UPDATE platform.outbox
SET locked_until = NULL
WHERE id = $1 AND published_at IS NULL;
```

4. Confirmar que o evento foi aplicado:

```sql
SELECT id, published_at, attempts FROM platform.outbox WHERE id = $1;
```

## 4. Reconciliação (dívida do F5)

Se houver suspeita de eventos perdidos (restauro de backup, bug no worker), comparar `identity.membership` com os tuplos do OpenFGA e reescrever as relações divergentes com `authz.SetTeamRole` (idempotente). A ferramenta de reconciliação periódica entra no F5.

## 5. Escalar

- Se o atraso afetar um treino/prova: informar o dono da equipa; o modo final funciona offline e não depende do worker.
- Abrir issue com o `outbox_id`, `attempts`, `aggregate` e `event_type` (nunca emails, tokens ou payloads com dados pessoais).
