-- F1-13 part C: engine mounts and part installs start syncing. Until now both were local
-- only, and nothing has been pulled for them, so every row of a live engine / car / part is
-- local only: queue its operations. Forward-only; a migration runs once.
--
-- Order matters. A mount that was closed is queued as "create, then close" before the next
-- mount of the same engine or car (the server refuses a second open mount), so the rows
-- are sorted by the time of each event, and at equal times a close goes before a create.
-- Rows whose engine, car or part is archived are skipped: the server never learned about them.
-- The server keeps device times only inside [-90 days, +5 min]; older ones become the arrival
-- time (the order is kept, the exact old date of a very old mount is lost).
--
-- The op id is a random UUID (v4 shaped); the engine signs the operation when it sends it.
-- The install note is local only (ADR 0023) and is not sent.

INSERT INTO sync_outbox (op_id, team_id, entity, entity_id, kind, fields_json, base_version, client_ts, created_at)
SELECT
  lower(substr(h, 1, 8) || '-' || substr(h, 9, 4) || '-4' || substr(h, 14, 3) || '-' ||
        substr('89ab', abs(random()) % 4 + 1, 1) || substr(h, 18, 3) || '-' || substr(h, 21, 12)),
  team_id, 'garage.engine_mount', id, kind, fields, base,
  CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER),
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM (
  SELECT m.team_id, m.id, 1 AS kind, m.mounted_at AS at_time, 1 AS phase,
         json_object('engine_id', m.engine_id, 'car_id', m.car_id, 'mounted_at', m.mounted_at) AS fields,
         NULL AS base, hex(randomblob(16)) AS h
    FROM engine_mount m
    JOIN engine e ON e.team_id = m.team_id AND e.id = m.engine_id AND e.deleted_at IS NULL
    JOIN car c ON c.team_id = m.team_id AND c.id = m.car_id AND c.deleted_at IS NULL
  UNION ALL
  SELECT m.team_id, m.id, 2, m.unmounted_at, 0,
         json_object('unmounted_at', m.unmounted_at), 1, hex(randomblob(16))
    FROM engine_mount m
    JOIN engine e ON e.team_id = m.team_id AND e.id = m.engine_id AND e.deleted_at IS NULL
    JOIN car c ON c.team_id = m.team_id AND c.id = m.car_id AND c.deleted_at IS NULL
   WHERE m.unmounted_at IS NOT NULL
  ORDER BY at_time, phase
);

INSERT INTO sync_outbox (op_id, team_id, entity, entity_id, kind, fields_json, base_version, client_ts, created_at)
SELECT
  lower(substr(h, 1, 8) || '-' || substr(h, 9, 4) || '-4' || substr(h, 14, 3) || '-' ||
        substr('89ab', abs(random()) % 4 + 1, 1) || substr(h, 18, 3) || '-' || substr(h, 21, 12)),
  team_id, 'garage.part_install', id, kind, fields, base,
  CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER),
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM (
  SELECT i.team_id, i.id, 1 AS kind, i.installed_at AS at_time, 0 AS phase,
         json_object('part_id', i.part_id, 'car_id', i.car_id, 'engine_id', i.engine_id,
                     'installed_at', i.installed_at) AS fields,
         NULL AS base, hex(randomblob(16)) AS h
    FROM part_install i
    JOIN part p ON p.team_id = i.team_id AND p.id = i.part_id AND p.deleted_at IS NULL
   WHERE (i.car_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM car WHERE team_id = i.team_id AND id = i.car_id AND deleted_at IS NULL))
      OR (i.engine_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM engine WHERE team_id = i.team_id AND id = i.engine_id AND deleted_at IS NULL))
  UNION ALL
  SELECT i.team_id, i.id, 2, i.removed_at, 1,
         json_object('removed_at', i.removed_at, 'removal_destination', i.removal_destination,
                     'removal_reason', i.removal_reason), 1, hex(randomblob(16))
    FROM part_install i
    JOIN part p ON p.team_id = i.team_id AND p.id = i.part_id AND p.deleted_at IS NULL
   WHERE i.removed_at IS NOT NULL AND (
         (i.car_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM car WHERE team_id = i.team_id AND id = i.car_id AND deleted_at IS NULL))
      OR (i.engine_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM engine WHERE team_id = i.team_id AND id = i.engine_id AND deleted_at IS NULL)))
  ORDER BY at_time, phase
);
