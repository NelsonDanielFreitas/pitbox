-- name: LockPartForStock :one
SELECT id FROM garage.part
WHERE team_id = $1 AND id = $2 AND deleted_at IS NULL
FOR UPDATE;

-- name: GetPartStockCenti :one
SELECT (COALESCE(sum(qty_delta), 0) * 100)::bigint
FROM garage.stock_movement
WHERE team_id = $1 AND part_id = $2;

-- name: CreateStockMovement :one
INSERT INTO garage.stock_movement (
  team_id, part_id, qty_delta, reason, note, occurred_at, created_by
)
VALUES ($1, $2, ($3::bigint::numeric / 100), $4::garage.stock_reason, $5, $6, $7)
RETURNING id, part_id, (qty_delta * 100)::bigint AS qty_delta_centi,
          reason::text AS reason, note, occurred_at;

-- name: ListStockMovements :many
SELECT id, part_id, (qty_delta * 100)::bigint AS qty_delta_centi,
       reason::text AS reason, note, occurred_at
FROM garage.stock_movement
WHERE team_id = $1 AND part_id = $2 AND id < $3
ORDER BY id DESC
LIMIT $4;

-- name: GetStockMovement :one
SELECT id, part_id, (qty_delta * 100)::bigint AS qty_delta_centi,
       reason::text AS reason, note, occurred_at
FROM garage.stock_movement
WHERE team_id = $1 AND id = $2;
