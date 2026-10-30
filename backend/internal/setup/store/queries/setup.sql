-- name: CreateSetup :one
INSERT INTO setup.setup (team_id, car_id, name, notes, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, car_id, name, current_version, notes, created_at, updated_at, version;

-- name: GetSetup :one
SELECT id, car_id, name, current_version, notes, created_at, updated_at, version
FROM setup.setup
WHERE team_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: LockSetup :one
SELECT id, car_id, name, current_version, notes, created_at, updated_at, version
FROM setup.setup
WHERE team_id = $1 AND id = $2 AND deleted_at IS NULL
FOR UPDATE;

-- name: ListSetups :many
SELECT id, car_id, name, current_version, notes, created_at, updated_at, version
FROM setup.setup
WHERE team_id = $1 AND deleted_at IS NULL AND id > $2
ORDER BY id
LIMIT $3;

-- name: ListSetupsByCar :many
SELECT id, car_id, name, current_version, notes, created_at, updated_at, version
FROM setup.setup
WHERE team_id = $1 AND deleted_at IS NULL AND car_id = $2 AND id > $3
ORDER BY id
LIMIT $4;

-- name: UpdateCurrentVersion :one
UPDATE setup.setup
SET current_version = $1
WHERE team_id = $2 AND id = $3 AND current_version = $4 AND deleted_at IS NULL
RETURNING id, car_id, name, current_version, notes, created_at, updated_at, version;

-- name: CreateVersion :one
INSERT INTO setup.setup_version (
  team_id, setup_id, version, data, schema_version,
  based_on_setup_id, based_on_version, change_summary, created_by
)
VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9)
RETURNING setup_id, version, schema_version, based_on_setup_id, based_on_version,
          change_summary, created_at;

-- name: GetVersion :one
SELECT v.setup_id, v.version, v.schema_version, v.based_on_setup_id, v.based_on_version,
       v.change_summary, v.data::text AS data, v.created_at
FROM setup.setup_version v
JOIN setup.setup s ON s.team_id = v.team_id AND s.id = v.setup_id AND s.deleted_at IS NULL
WHERE v.team_id = $1 AND v.setup_id = $2
  AND v.version = CASE WHEN $3::integer = 0 THEN s.current_version ELSE $3::integer END;

-- name: ListVersions :many
SELECT v.setup_id, v.version, v.schema_version, v.based_on_setup_id, v.based_on_version,
       v.change_summary, v.created_at
FROM setup.setup_version v
JOIN setup.setup s ON s.team_id = v.team_id AND s.id = v.setup_id AND s.deleted_at IS NULL
WHERE v.team_id = $1 AND v.setup_id = $2 AND v.version < $3
ORDER BY v.version DESC
LIMIT $4;

-- The device generates the id (UUIDv7) so an offline record keeps it after syncing.
-- name: CreateSetupWithID :one
INSERT INTO setup.setup (id, team_id, car_id, name, notes, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, car_id, name, current_version, notes, created_at, updated_at, version;
