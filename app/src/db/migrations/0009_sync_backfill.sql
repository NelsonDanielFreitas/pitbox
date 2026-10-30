-- F1-13b: engines, tyre sets and parts start syncing. Rows created before this migration
-- were never sent (they were local only), and nothing has been pulled for these entities
-- yet, so every live row here is local only: queue one create operation for each.
-- Forward-only and idempotent per database (a migration runs once).
--
-- The op id is a random UUID (v4 shaped); the engine signs the operation when it sends it.
-- client_ts is "now", which is inside the window the server accepts.

INSERT INTO sync_outbox (op_id, team_id, entity, entity_id, kind, fields_json, base_version, client_ts, created_at)
SELECT
  lower(substr(h, 1, 8) || '-' || substr(h, 9, 4) || '-4' || substr(h, 14, 3) || '-' ||
        substr('89ab', abs(random()) % 4 + 1, 1) || substr(h, 18, 3) || '-' || substr(h, 21, 12)),
  team_id, 'garage.engine', id, 1,
  json_object('make', make, 'model', model, 'serial_number', serial_number,
              'purchased_on', purchased_on, 'status', status, 'notes', notes),
  NULL,
  CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER),
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM (SELECT *, hex(randomblob(16)) AS h FROM engine WHERE deleted_at IS NULL ORDER BY created_at, id);

INSERT INTO sync_outbox (op_id, team_id, entity, entity_id, kind, fields_json, base_version, client_ts, created_at)
SELECT
  lower(substr(h, 1, 8) || '-' || substr(h, 9, 4) || '-4' || substr(h, 14, 3) || '-' ||
        substr('89ab', abs(random()) % 4 + 1, 1) || substr(h, 18, 3) || '-' || substr(h, 21, 12)),
  team_id, 'garage.tyre_set', id, 1,
  json_object('label', label, 'make', make, 'model', model, 'compound', compound,
              'insert_type', insert_type, 'glued_on', glued_on, 'status', status,
              'wear_notes', wear_notes),
  NULL,
  CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER),
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM (SELECT *, hex(randomblob(16)) AS h FROM tyre_set WHERE deleted_at IS NULL ORDER BY created_at, id);

-- Parts: the initial quantity is not here; it travels with the stock movements (next F1-13 part).
INSERT INTO sync_outbox (op_id, team_id, entity, entity_id, kind, fields_json, base_version, client_ts, created_at)
SELECT
  lower(substr(h, 1, 8) || '-' || substr(h, 9, 4) || '-4' || substr(h, 14, 3) || '-' ||
        substr('89ab', abs(random()) % 4 + 1, 1) || substr(h, 18, 3) || '-' || substr(h, 21, 12)),
  team_id, 'garage.part', id, 1,
  json_object('kind', kind, 'brand', brand, 'reference', reference, 'description', description,
              'unit', unit, 'specs', json(specs), 'min_stock_centi', min_stock_centi,
              'reorder_to_centi', reorder_to_centi, 'unit_cost_cents', unit_cost_cents,
              'has_service_life', CASE WHEN has_service_life = 1 THEN json('true') ELSE json('false') END),
  NULL,
  CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER),
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM (SELECT *, hex(randomblob(16)) AS h FROM part WHERE deleted_at IS NULL ORDER BY created_at, id);
