-- name: CreateEngine :one
INSERT INTO garage.engine (
  team_id, make, model, serial_number, purchased_on, status, notes, created_by
)
VALUES ($1, $2, $3, $4, $5::date, $6::garage.asset_status, $7, $8)
RETURNING id, make, model, serial_number, COALESCE(purchased_on::text, '')::text AS purchased_on,
          status::text AS status, (tanks_total * 100)::bigint AS tanks_centi,
          run_seconds_total, sessions_total, notes, created_at, version;

-- name: GetEngine :one
SELECT id, make, model, serial_number, COALESCE(purchased_on::text, '')::text AS purchased_on,
       status::text AS status, (tanks_total * 100)::bigint AS tanks_centi,
       run_seconds_total, sessions_total, notes, created_at, version
FROM garage.engine
WHERE team_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: ListEngines :many
SELECT id, make, model, serial_number, COALESCE(purchased_on::text, '')::text AS purchased_on,
       status::text AS status, (tanks_total * 100)::bigint AS tanks_centi,
       run_seconds_total, sessions_total, notes, created_at, version
FROM garage.engine
WHERE team_id = $1 AND deleted_at IS NULL AND id > $2
ORDER BY id
LIMIT $3;

-- name: UpdateEngine :one
UPDATE garage.engine
SET make = $1, model = $2, serial_number = $3, purchased_on = $4::date,
    status = $5::garage.asset_status, notes = $6
WHERE team_id = $7 AND id = $8 AND version = $9 AND deleted_at IS NULL
RETURNING id, make, model, serial_number, COALESCE(purchased_on::text, '')::text AS purchased_on,
          status::text AS status, (tanks_total * 100)::bigint AS tanks_centi,
          run_seconds_total, sessions_total, notes, created_at, version;

-- name: SoftDeleteEngine :one
UPDATE garage.engine
SET deleted_at = now()
WHERE team_id = $1 AND id = $2 AND version = $3 AND deleted_at IS NULL
RETURNING id;

-- name: EngineExists :one
SELECT EXISTS (
  SELECT 1 FROM garage.engine
  WHERE team_id = $1 AND id = $2 AND deleted_at IS NULL
);
