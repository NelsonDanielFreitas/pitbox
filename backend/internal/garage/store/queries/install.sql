-- installed_at defaults to the database clock; a client that was offline may send the
-- real time (validated as not in the future before it gets here).
-- name: CreatePartInstall :one
INSERT INTO garage.part_install (team_id, part_id, car_id, engine_id, installed_at, created_by)
VALUES ($1, $2, sqlc.narg('car_id'), sqlc.narg('engine_id'), COALESCE(sqlc.narg('installed_at')::timestamptz, clock_timestamp()), $3)
RETURNING id, part_id, car_id, engine_id, installed_at, removed_at,
          removal_destination, removal_reason, run_seconds, sessions, version;

-- name: GetPartInstallForUpdate :one
SELECT id, part_id, car_id, engine_id, installed_at, removed_at,
       removal_destination, removal_reason, run_seconds, sessions, version
FROM garage.part_install
WHERE team_id = $1 AND id = $2
FOR UPDATE;

-- clock_timestamp() is read after the locks, like engine mounts.
-- name: ClosePartInstall :one
UPDATE garage.part_install
SET removed_at = clock_timestamp(), removal_destination = $3, removal_reason = sqlc.narg('removal_reason')
WHERE team_id = $1 AND id = $2 AND removed_at IS NULL
RETURNING id, part_id, car_id, engine_id, installed_at, removed_at,
          removal_destination, removal_reason, run_seconds, sessions, version;

-- Exactly one of part_id, car_id and engine_id is set (the application enforces it).
-- name: ListPartInstalls :many
SELECT id, part_id, car_id, engine_id, installed_at, removed_at,
       removal_destination, removal_reason, run_seconds, sessions, version
FROM garage.part_install
WHERE team_id = $1
  AND (sqlc.narg('part_id')::uuid IS NULL OR part_id = sqlc.narg('part_id'))
  AND (sqlc.narg('car_id')::uuid IS NULL OR car_id = sqlc.narg('car_id'))
  AND (sqlc.narg('engine_id')::uuid IS NULL OR engine_id = sqlc.narg('engine_id'))
  AND (sqlc.arg('include_history')::boolean OR removed_at IS NULL)
  AND id < sqlc.arg('after')
ORDER BY id DESC
LIMIT sqlc.arg('row_limit');
