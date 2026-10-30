-- name: GetOperation :one
SELECT op_id, team_id, user_id, device_key_id, entity, entity_id,
       status::text AS status, reason, result_version
FROM sync.operation
WHERE op_id = $1;

-- name: InsertOperation :exec
INSERT INTO sync.operation (
  op_id, team_id, user_id, device_key_id, entity, entity_id, kind, base_version,
  fields, client_ts, status, reason, device_signature, result_version
)
VALUES ($1, $2, $3, $4, $5, $6, sqlc.arg(kind)::sync.op_kind, $7, $8, $9, sqlc.arg(status)::sync.op_status, $10, $11, $12);

-- Operations applied to an entity after the version a patch was based on. Fields that
-- ended in a conflict were NOT applied, so they are removed from what the operation changed.
-- name: ListAppliedSince :many
SELECT o.result_version,
       (o.fields - COALESCE(array_agg(c.field) FILTER (WHERE c.field IS NOT NULL), '{}')::text[])::jsonb AS fields
FROM sync.operation o
LEFT JOIN sync.conflict c ON c.op_id = o.op_id
WHERE o.team_id = $1 AND o.entity = $2 AND o.entity_id = $3
  AND o.status IN ('applied', 'merged', 'conflict') AND o.result_version > $4
GROUP BY o.op_id
ORDER BY o.result_version
LIMIT 200;
