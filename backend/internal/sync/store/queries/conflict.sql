-- name: InsertConflict :exec
INSERT INTO sync.conflict (team_id, op_id, entity, entity_id, field, server_value, client_value)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListOpenConflicts :many
SELECT id, op_id, entity, entity_id, field, server_value, client_value, created_at
FROM sync.conflict
WHERE team_id = $1 AND resolved_at IS NULL
ORDER BY created_at, id
LIMIT 200;

-- name: GetConflict :one
SELECT id, op_id, entity, entity_id, field, server_value, client_value, created_at, resolved_at
FROM sync.conflict
WHERE team_id = $1 AND id = $2;

-- name: ResolveConflict :one
UPDATE sync.conflict
SET resolution = $3, resolved_value = $4, resolved_by = $5, resolved_at = now()
WHERE team_id = $1 AND id = $2 AND resolved_at IS NULL
RETURNING id, op_id, entity, entity_id, field, server_value, client_value, created_at;
