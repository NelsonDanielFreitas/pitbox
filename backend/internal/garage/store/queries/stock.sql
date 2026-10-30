-- name: LockPartForStock :one
SELECT id FROM garage.part
WHERE team_id = $1 AND id = $2 AND deleted_at IS NULL
FOR UPDATE;

-- name: GetPartStockCenti :one
SELECT (COALESCE(sum(qty_delta), 0) * 100)::bigint
FROM garage.stock_movement
WHERE team_id = $1 AND part_id = $2;

-- occurred_at uses the database clock so movements created by different API instances
-- share one time source.
-- name: CreateStockMovement :one
INSERT INTO garage.stock_movement (
  team_id, part_id, qty_delta, reason, note, occurred_at, created_by, unit_cost_cents, loss_reason
)
VALUES ($1, $2, ($3::bigint::numeric / 100), $4::garage.stock_reason, $5, now(), $6,
        sqlc.narg('unit_cost_cents')::integer, sqlc.narg('loss_reason')::garage.loss_reason)
RETURNING id, part_id, (qty_delta * 100)::bigint AS qty_delta_centi,
          reason::text AS reason, note, occurred_at, unit_cost_cents, COALESCE(loss_reason::text, '')::text AS loss_reason;

-- name: ListStockMovements :many
SELECT id, part_id, (qty_delta * 100)::bigint AS qty_delta_centi,
       reason::text AS reason, note, occurred_at, unit_cost_cents, COALESCE(loss_reason::text, '')::text AS loss_reason
FROM garage.stock_movement
WHERE team_id = $1 AND part_id = $2 AND id < $3
ORDER BY id DESC
LIMIT $4;

-- name: GetStockMovement :one
SELECT id, part_id, (qty_delta * 100)::bigint AS qty_delta_centi,
       reason::text AS reason, note, occurred_at, unit_cost_cents, COALESCE(loss_reason::text, '')::text AS loss_reason
FROM garage.stock_movement
WHERE team_id = $1 AND id = $2;

-- The synced variant: the device chose the id (UUIDv7, the sync entity id) and the time the
-- movement happened. The caller has already clamped occurred_at to a sane window.
-- name: CreateStockMovementWithID :one
INSERT INTO garage.stock_movement (
  id, team_id, part_id, qty_delta, reason, note, occurred_at, created_by, unit_cost_cents, loss_reason
)
VALUES ($1, $2, $3, ($4::bigint::numeric / 100), $5::garage.stock_reason, $6, $7, $8,
        sqlc.narg('unit_cost_cents')::integer, sqlc.narg('loss_reason')::garage.loss_reason)
RETURNING id, part_id, (qty_delta * 100)::bigint AS qty_delta_centi,
          reason::text AS reason, note, occurred_at, unit_cost_cents, COALESCE(loss_reason::text, '')::text AS loss_reason;
