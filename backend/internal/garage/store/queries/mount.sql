-- name: LockEngine :one
SELECT id FROM garage.engine
WHERE team_id = $1 AND id = $2 AND deleted_at IS NULL
FOR UPDATE;

-- name: LockCar :one
SELECT id FROM garage.car
WHERE team_id = $1 AND id = $2 AND deleted_at IS NULL
FOR UPDATE;

-- name: GetOpenMountForEngine :one
SELECT id, engine_id, car_id, mounted_at, unmounted_at
FROM garage.engine_mount
WHERE team_id = $1 AND engine_id = $2 AND unmounted_at IS NULL;

-- name: GetOpenMountForCar :one
SELECT id, engine_id, car_id, mounted_at, unmounted_at
FROM garage.engine_mount
WHERE team_id = $1 AND car_id = $2 AND unmounted_at IS NULL;

-- clock_timestamp() is read after the locks; now() would be the transaction start and could
-- be earlier than a concurrent mount that won the lock first.
-- name: CreateMount :one
INSERT INTO garage.engine_mount (team_id, engine_id, car_id, mounted_at, created_by)
VALUES ($1, $2, $3, clock_timestamp(), $4)
RETURNING id, engine_id, car_id, mounted_at, unmounted_at;

-- name: CloseMount :one
UPDATE garage.engine_mount
SET unmounted_at = clock_timestamp()
WHERE team_id = $1 AND id = $2 AND unmounted_at IS NULL
RETURNING id;

-- name: ListMountsForEngine :many
SELECT id, engine_id, car_id, mounted_at, unmounted_at
FROM garage.engine_mount
WHERE team_id = $1 AND engine_id = $2 AND id < $3
ORDER BY id DESC
LIMIT $4;

-- The synced variants: the device chose the id and the times (the caller has clamped them).
-- name: CreateMountWithID :one
INSERT INTO garage.engine_mount (id, team_id, engine_id, car_id, mounted_at, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, engine_id, car_id, mounted_at, unmounted_at;

-- name: CloseMountAt :execrows
UPDATE garage.engine_mount
SET unmounted_at = $3
WHERE team_id = $1 AND id = $2 AND unmounted_at IS NULL;

-- No lock: Pull reads in a read-only transaction.
-- name: GetMountByID :one
SELECT id, engine_id, car_id, mounted_at, unmounted_at
FROM garage.engine_mount
WHERE team_id = $1 AND id = $2;
