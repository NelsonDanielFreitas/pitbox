-- name: CreateCar :one
INSERT INTO garage.car (
  team_id, make, model, model_year, serial_number, nickname, transponder_number,
  status, scale, powertrain, notes, created_by, sheet_template_id
)
VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8::garage.asset_status, $9, $10, $11, $12, $13
)
RETURNING id, make, model, model_year, serial_number, nickname, transponder_number,
          status::text AS status, scale, powertrain, notes, created_at, version, sheet_template_id;

-- name: GetCar :one
SELECT id, make, model, model_year, serial_number, nickname, transponder_number,
       status::text AS status, scale, powertrain, notes, created_at, version, sheet_template_id
FROM garage.car
WHERE team_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: ListCars :many
SELECT id, make, model, model_year, serial_number, nickname, transponder_number,
       status::text AS status, scale, powertrain, notes, created_at, version, sheet_template_id
FROM garage.car
WHERE team_id = $1 AND deleted_at IS NULL AND id > $2
ORDER BY id
LIMIT $3;

-- name: UpdateCar :one
UPDATE garage.car
SET make = $1, model = $2, model_year = $3, serial_number = $4, nickname = $5,
    transponder_number = $6, status = $7::garage.asset_status, scale = $8,
    powertrain = $9, notes = $10, sheet_template_id = $11
WHERE team_id = $12 AND id = $13 AND version = $14 AND deleted_at IS NULL
RETURNING id, make, model, model_year, serial_number, nickname, transponder_number,
          status::text AS status, scale, powertrain, notes, created_at, version, sheet_template_id;

-- name: SoftDeleteCar :one
UPDATE garage.car
SET deleted_at = now()
WHERE team_id = $1 AND id = $2 AND version = $3 AND deleted_at IS NULL
RETURNING id;

-- name: CarExists :one
SELECT EXISTS (
  SELECT 1 FROM garage.car
  WHERE team_id = $1 AND id = $2 AND deleted_at IS NULL
);

-- The device generates the id (UUIDv7) so an offline record keeps it after syncing.
-- name: CreateCarWithID :one
INSERT INTO garage.car (
  id, team_id, make, model, model_year, serial_number, nickname, transponder_number,
  status, scale, powertrain, notes, created_by, sheet_template_id
)
VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9::garage.asset_status, $10, $11, $12, $13, $14
)
RETURNING id, make, model, model_year, serial_number, nickname, transponder_number,
          status::text AS status, scale, powertrain, notes, created_at, version, sheet_template_id;
