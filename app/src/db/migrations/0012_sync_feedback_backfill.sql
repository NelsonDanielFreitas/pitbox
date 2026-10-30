-- F1-13: the driver's feedback starts syncing (ADR 0026). Until now it was local only, so every
-- row of a live setup is local only: queue one append for each, oldest first. Forward-only; a
-- migration runs once. A setup that was archived is skipped (the server never learned about it).
-- Feedback on a version the server does not have (a setup made before sync that was never
-- sent) is rejected there like any other operation with a bad reference.
--
-- The op id is a random UUID (v4 shaped); the engine signs the operation when it sends it.
-- The author is not sent: the server always takes the authenticated user.

INSERT INTO sync_outbox (op_id, team_id, entity, entity_id, kind, fields_json, base_version, client_ts, created_at)
SELECT
  lower(substr(h, 1, 8) || '-' || substr(h, 9, 4) || '-4' || substr(h, 14, 3) || '-' ||
        substr('89ab', abs(random()) % 4 + 1, 1) || substr(h, 18, 3) || '-' || substr(h, 21, 12)),
  team_id, 'setup.setup_feedback', id, 3,
  json_object('setup_id', setup_id, 'version', version,
              'steering_entry', steering_entry, 'steering_mid', steering_mid,
              'steering_exit', steering_exit, 'traction', traction, 'jump_stability', jump_stability,
              'braking', braking, 'engine_low_end', engine_low_end, 'engine_top_end', engine_top_end,
              'track_zone', track_zone, 'note', note, 'created_at', created_at),
  NULL,
  CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER),
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM (
  SELECT f.*, hex(randomblob(16)) AS h
    FROM setup_feedback f
    JOIN setup s ON s.team_id = f.team_id AND s.id = f.setup_id AND s.deleted_at IS NULL
   ORDER BY f.created_at, f.id
);
