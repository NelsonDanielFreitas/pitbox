-- F1-13 part B: stock movements and the shopping list start syncing. Until now both were
-- local only, and nothing has been pulled for them, so every row of a live part is local only:
-- queue one operation for each. Movements go first and in the order they happened, so the
-- server's stock is right when it checks a shopping line. Movements and lines of archived
-- parts are skipped: the server never learned about those parts (see 0009).
-- Forward-only; a migration runs once.
--
-- The op id is a random UUID (v4 shaped); the engine signs the operation when it sends it.
-- The store (local only, ADR 0023) is folded into the note, like movementOutboxStatement does.

INSERT INTO sync_outbox (op_id, team_id, entity, entity_id, kind, fields_json, base_version, client_ts, created_at)
SELECT
  lower(substr(h, 1, 8) || '-' || substr(h, 9, 4) || '-4' || substr(h, 14, 3) || '-' ||
        substr('89ab', abs(random()) % 4 + 1, 1) || substr(h, 18, 3) || '-' || substr(h, 21, 12)),
  team_id, 'garage.stock_movement', id, 3,
  json_object(
    'part_id', part_id,
    'qty_delta_centi', qty_delta_centi,
    'reason', reason,
    'note', substr(CASE
      WHEN store IS NULL THEN note
      WHEN note IS NULL THEN 'Loja: ' || store
      ELSE 'Loja: ' || store || ' · ' || note END, 1, 500),
    'occurred_at', occurred_at,
    'unit_cost_cents', unit_cost_cents,
    'loss_reason', loss_reason
  ),
  NULL,
  CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER),
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM (
  SELECT m.*, hex(randomblob(16)) AS h
    FROM stock_movement m
    JOIN part p ON p.team_id = m.team_id AND p.id = m.part_id AND p.deleted_at IS NULL
   ORDER BY m.occurred_at, m.rowid
);

INSERT INTO sync_outbox (op_id, team_id, entity, entity_id, kind, fields_json, base_version, client_ts, created_at)
SELECT
  lower(substr(h, 1, 8) || '-' || substr(h, 9, 4) || '-4' || substr(h, 14, 3) || '-' ||
        substr('89ab', abs(random()) % 4 + 1, 1) || substr(h, 18, 3) || '-' || substr(h, 21, 12)),
  team_id, 'garage.shopping_item', id, 1,
  json_object('part_id', part_id, 'qty_centi', qty_centi,
              'dismissed', CASE WHEN dismissed = 1 THEN json('true') ELSE json('false') END,
              'note', note),
  NULL,
  CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER),
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM (
  SELECT s.*, hex(randomblob(16)) AS h
    FROM shopping_item s
    JOIN part p ON p.team_id = s.team_id AND p.id = s.part_id AND p.deleted_at IS NULL
   ORDER BY s.created_at, s.id
);
