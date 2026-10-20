-- Queries da outbox transacional. O claim respeita a ordem por chave (ADR 0015).

-- name: InsertOutbox :exec
INSERT INTO platform.outbox (team_id, aggregate, aggregate_id, event_type, payload, trace_parent)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ClaimOutbox :many
-- Pega no evento mais antigo e por publicar de cada chave (team_id, aggregate, aggregate_id).
-- Um evento com backoff continua a bloquear os seguintes da mesma chave, mas não as outras.
UPDATE platform.outbox o
SET locked_until = now() + make_interval(secs => sqlc.arg(lease_seconds)::double precision),
    attempts = LEAST(o.attempts + 1, 32767)
WHERE o.id IN (
    SELECT o2.id
      FROM platform.outbox o2
     WHERE o2.published_at IS NULL
       AND (o2.locked_until IS NULL OR o2.locked_until < now())
       AND NOT EXISTS (
           SELECT 1
             FROM platform.outbox o3
            WHERE o3.published_at IS NULL
              AND o3.team_id IS NOT DISTINCT FROM o2.team_id
              AND o3.aggregate = o2.aggregate
              AND o3.aggregate_id = o2.aggregate_id
              AND o3.id < o2.id
       )
     ORDER BY o2.id
     LIMIT sqlc.arg(batch_size)
     FOR UPDATE SKIP LOCKED
)
RETURNING o.id, o.team_id, o.aggregate, o.aggregate_id, o.event_type, o.payload, o.trace_parent, o.attempts;

-- name: MarkOutboxPublished :exec
UPDATE platform.outbox
SET published_at = now(), locked_until = NULL
WHERE id = ANY($1::bigint[]);

-- name: RescheduleOutbox :exec
-- Substitui a lease pelo backoff do evento que falhou.
UPDATE platform.outbox
SET locked_until = now() + make_interval(secs => sqlc.arg(backoff_seconds)::double precision)
WHERE id = sqlc.arg(id);

-- name: ClaimOutboxKey :many
-- Claim the oldest event of one key only, for the post-commit synchronous drain.
-- The NOT EXISTS keeps the per-key order: if the worker already holds an older
-- event, the drain does nothing and lets the worker finish.
UPDATE platform.outbox o
SET locked_until = now() + make_interval(secs => sqlc.arg(lease_seconds)::double precision),
    attempts = LEAST(o.attempts + 1, 32767)
WHERE o.id IN (
    SELECT o2.id
      FROM platform.outbox o2
     WHERE o2.published_at IS NULL
       AND o2.team_id = sqlc.arg(team_id)::uuid
       AND o2.aggregate = sqlc.arg(aggregate)
       AND o2.aggregate_id = sqlc.arg(aggregate_id)::uuid
       AND (o2.locked_until IS NULL OR o2.locked_until < now())
       AND NOT EXISTS (
           SELECT 1
             FROM platform.outbox o3
            WHERE o3.published_at IS NULL
              AND o3.team_id = o2.team_id
              AND o3.aggregate = o2.aggregate
              AND o3.aggregate_id = o2.aggregate_id
              AND o3.id < o2.id
       )
     ORDER BY o2.id
     LIMIT sqlc.arg(batch_size)
     FOR UPDATE SKIP LOCKED
)
RETURNING o.id, o.team_id, o.aggregate, o.aggregate_id, o.event_type, o.payload, o.trace_parent, o.attempts;
