-- name: CreatePart :one
INSERT INTO garage.part (
  team_id, kind, brand, reference, description, unit, specs, min_stock,
  unit_cost_cents, currency, has_service_life, created_by
)
VALUES (
  $1, $2::garage.part_kind, $3, $4, $5, $6, $7::jsonb,
  ($8::bigint::numeric / 100), $9, $10, $11, $12
)
RETURNING id, kind::text AS kind, brand, reference, description, unit, specs::text AS specs,
          (min_stock * 100)::bigint AS min_stock_centi, unit_cost_cents, currency,
          has_service_life, created_at, version;

-- name: GetPart :one
SELECT p.id, p.kind::text AS kind, p.brand, p.reference, p.description, p.unit,
       p.specs::text AS specs, (p.min_stock * 100)::bigint AS min_stock_centi,
       p.unit_cost_cents, p.currency, p.has_service_life,
       (COALESCE(sl.qty, 0) * 100)::bigint AS qty_centi,
       COALESCE(sl.below_min, false) AS below_min,
       p.created_at, p.version
FROM garage.part p
LEFT JOIN garage.stock_level sl ON sl.team_id = p.team_id AND sl.part_id = p.id
WHERE p.team_id = $1 AND p.id = $2 AND p.deleted_at IS NULL;

-- name: ListParts :many
SELECT p.id, p.kind::text AS kind, p.brand, p.reference, p.description, p.unit,
       p.specs::text AS specs, (p.min_stock * 100)::bigint AS min_stock_centi,
       p.unit_cost_cents, p.currency, p.has_service_life,
       (COALESCE(sl.qty, 0) * 100)::bigint AS qty_centi,
       COALESCE(sl.below_min, false) AS below_min,
       p.created_at, p.version
FROM garage.part p
LEFT JOIN garage.stock_level sl ON sl.team_id = p.team_id AND sl.part_id = p.id
WHERE p.team_id = $1 AND p.deleted_at IS NULL AND p.id > $2
ORDER BY p.id
LIMIT $3;

-- name: UpdatePart :one
UPDATE garage.part
SET kind = $1::garage.part_kind, brand = $2, reference = $3, description = $4,
    unit = $5, specs = $6::jsonb, min_stock = ($7::bigint::numeric / 100),
    unit_cost_cents = $8, currency = $9, has_service_life = $10
WHERE team_id = $11 AND id = $12 AND version = $13 AND deleted_at IS NULL
RETURNING id, kind::text AS kind, brand, reference, description, unit, specs::text AS specs,
          (min_stock * 100)::bigint AS min_stock_centi, unit_cost_cents, currency,
          has_service_life, created_at, version;

-- name: SoftDeletePart :one
UPDATE garage.part
SET deleted_at = now()
WHERE team_id = $1 AND id = $2 AND version = $3 AND deleted_at IS NULL
RETURNING id;
