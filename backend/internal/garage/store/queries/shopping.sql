-- qty_centi, item_qty_centi and reorder_to_centi are -1 when the column is NULL.
-- The list is computed: parts below their minimum, plus the stored lines that are not
-- hidden. Only exceptions live in garage.shopping_item. qty and min_stock come from the
-- stock_level view; reorder_to defaults to min_stock.
-- name: ListShoppingList :many
SELECT p.id AS part_id,
       (COALESCE(sl.qty, 0) * 100)::bigint AS qty_centi,
       (p.min_stock * 100)::bigint AS min_stock_centi,
       COALESCE((p.reorder_to * 100)::bigint, -1)::bigint AS reorder_to_centi,
       COALESCE(sl.below_min, false) AS below_min,
       p.unit_cost_cents,
       si.id AS item_id,
       COALESCE((si.qty * 100)::bigint, -1)::bigint AS item_qty_centi,
       si.note AS item_note,
       COALESCE(si.version, 0)::integer AS item_version
FROM garage.part p
LEFT JOIN garage.stock_level sl ON sl.team_id = p.team_id AND sl.part_id = p.id
LEFT JOIN garage.shopping_item si ON si.team_id = p.team_id AND si.part_id = p.id
WHERE p.team_id = $1 AND p.deleted_at IS NULL AND p.id > $2
  AND NOT COALESCE(si.dismissed, false)
  AND (COALESCE(sl.below_min, false) OR si.id IS NOT NULL)
ORDER BY p.id
LIMIT $3;

-- name: GetShoppingItemForUpdate :one
SELECT id, part_id, COALESCE((qty * 100)::bigint, -1)::bigint AS qty_centi, dismissed, note, version
FROM garage.shopping_item
WHERE team_id = $1 AND part_id = $2
FOR UPDATE;

-- name: CreateShoppingItem :one
INSERT INTO garage.shopping_item (team_id, part_id, qty, dismissed, note, created_by)
VALUES ($1, $2, (sqlc.narg('qty_centi')::bigint::numeric / 100), $3, sqlc.narg('note'), $4)
RETURNING id, part_id, COALESCE((qty * 100)::bigint, -1)::bigint AS qty_centi, dismissed, note, version;

-- name: UpdateShoppingItem :one
UPDATE garage.shopping_item
SET qty = (sqlc.narg('qty_centi')::bigint::numeric / 100), dismissed = $3, note = sqlc.narg('note')
WHERE team_id = $1 AND part_id = $2 AND version = $4
RETURNING id, part_id, COALESCE((qty * 100)::bigint, -1)::bigint AS qty_centi, dismissed, note, version;

-- name: DeleteShoppingItemForPart :execrows
DELETE FROM garage.shopping_item WHERE team_id = $1 AND part_id = $2;

-- A hidden line comes back once the stock is at the minimum again, so the alert works next time.
-- name: ClearDismissedShoppingItem :execrows
DELETE FROM garage.shopping_item si
USING garage.stock_level sl
WHERE si.team_id = $1 AND si.part_id = $2 AND si.dismissed
  AND sl.team_id = si.team_id AND sl.part_id = si.part_id AND NOT sl.below_min;

-- The synced variants address a line by its own id (the sync entity id), not by its part.
-- No lock: Pull reads in a read-only transaction, and the writes below check the version.
-- name: GetShoppingItemByID :one
SELECT id, part_id, COALESCE((qty * 100)::bigint, -1)::bigint AS qty_centi, dismissed, note, version
FROM garage.shopping_item
WHERE team_id = $1 AND id = $2;

-- name: CreateShoppingItemWithID :one
INSERT INTO garage.shopping_item (id, team_id, part_id, qty, dismissed, note, created_by)
VALUES ($1, $2, $3, (sqlc.narg('qty_centi')::bigint::numeric / 100), $4, sqlc.narg('note'), $5)
RETURNING id, part_id, COALESCE((qty * 100)::bigint, -1)::bigint AS qty_centi, dismissed, note, version;

-- name: UpdateShoppingItemByID :one
UPDATE garage.shopping_item
SET qty = (sqlc.narg('qty_centi')::bigint::numeric / 100), dismissed = $3, note = sqlc.narg('note')
WHERE team_id = $1 AND id = $2 AND version = $4
RETURNING id, part_id, COALESCE((qty * 100)::bigint, -1)::bigint AS qty_centi, dismissed, note, version;

-- name: DeleteShoppingItemByID :execrows
DELETE FROM garage.shopping_item WHERE team_id = $1 AND id = $2 AND version = $3;
