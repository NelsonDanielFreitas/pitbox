-- name: CreateTyreSet :one
INSERT INTO garage.tyre_set (
  team_id, label, make, model, compound, insert_type, glued_on, status, wear_notes, created_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7::date, $8::garage.tyre_status, $9, $10)
RETURNING id, label, make, model, compound, insert_type, COALESCE(glued_on::text, '')::text AS glued_on,
          status::text AS status, sessions_total, run_seconds_total, wear_notes, created_at, version;

-- name: GetTyreSet :one
SELECT id, label, make, model, compound, insert_type, COALESCE(glued_on::text, '')::text AS glued_on,
       status::text AS status, sessions_total, run_seconds_total, wear_notes, created_at, version
FROM garage.tyre_set
WHERE team_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: ListTyreSets :many
SELECT id, label, make, model, compound, insert_type, COALESCE(glued_on::text, '')::text AS glued_on,
       status::text AS status, sessions_total, run_seconds_total, wear_notes, created_at, version
FROM garage.tyre_set
WHERE team_id = $1 AND deleted_at IS NULL AND id > $2
ORDER BY id
LIMIT $3;

-- name: UpdateTyreSet :one
UPDATE garage.tyre_set
SET label = $1, make = $2, model = $3, compound = $4, insert_type = $5,
    glued_on = $6::date, status = $7::garage.tyre_status, wear_notes = $8
WHERE team_id = $9 AND id = $10 AND version = $11 AND deleted_at IS NULL
RETURNING id, label, make, model, compound, insert_type, COALESCE(glued_on::text, '')::text AS glued_on,
          status::text AS status, sessions_total, run_seconds_total, wear_notes, created_at, version;

-- name: SoftDeleteTyreSet :one
UPDATE garage.tyre_set
SET deleted_at = now()
WHERE team_id = $1 AND id = $2 AND version = $3 AND deleted_at IS NULL
RETURNING id;

-- name: TyreSetExists :one
SELECT EXISTS (
  SELECT 1 FROM garage.tyre_set
  WHERE team_id = $1 AND id = $2 AND deleted_at IS NULL
);
