-- F1-13b (ADR 0025): local state of the sync engine.
-- Forward-only: new tables only. The cursor, catalogue hash, device id and offline pass
-- live in app_meta (keys "sync.<what>.<team id>").

-- Operations waiting to be sent. A row is written in the SAME transaction as the local data
-- it describes, so a crash can never leave a change without its operation (or the reverse).
-- fields_json holds the exact text that will be signed and sent (UTF-8, JSON object).
CREATE TABLE sync_outbox (
  seq          INTEGER PRIMARY KEY AUTOINCREMENT,
  op_id        TEXT NOT NULL UNIQUE CHECK (length(op_id) = 36),
  team_id      TEXT NOT NULL CHECK (length(team_id) = 36),
  entity       TEXT NOT NULL CHECK (length(entity) BETWEEN 3 AND 64),
  entity_id    TEXT NOT NULL CHECK (length(entity_id) = 36),
  kind         INTEGER NOT NULL CHECK (kind BETWEEN 1 AND 4),
  fields_json  TEXT NOT NULL CHECK (json_valid(fields_json) AND json_type(fields_json) = 'object'),
  base_version INTEGER CHECK (base_version IS NULL OR base_version >= 1),
  client_ts    INTEGER NOT NULL CHECK (client_ts > 0),
  -- pending: still to be sent. rejected / conflict: the server answered; kept so the
  -- mechanic can see what did not go through (the row is removed when dismissed).
  state        TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'rejected', 'conflict')),
  reason       TEXT CHECK (reason IS NULL OR length(reason) <= 64),
  attempts     INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  created_at   TEXT NOT NULL
) STRICT;

CREATE INDEX sync_outbox_pending_idx ON sync_outbox (state, seq);
CREATE INDEX sync_outbox_entity_idx ON sync_outbox (team_id, entity, entity_id, state);

-- Server changes that could not be applied yet because the entity still has pending local
-- operations (or the row did not fit). The last one wins; they are applied when the entity
-- is free. kind: 1 = upsert, 2 = delete.
CREATE TABLE sync_held (
  team_id   TEXT NOT NULL,
  entity    TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  kind      INTEGER NOT NULL CHECK (kind IN (1, 2)),
  data      TEXT CHECK (data IS NULL OR json_valid(data)),
  PRIMARY KEY (team_id, entity, entity_id)
) STRICT;

-- Open conflicts, replaced as a whole by each ListConflicts. Values are JSON text.
CREATE TABLE sync_conflict (
  id           TEXT PRIMARY KEY CHECK (length(id) = 36),
  team_id      TEXT NOT NULL,
  op_id        TEXT NOT NULL,
  entity       TEXT NOT NULL,
  entity_id    TEXT NOT NULL,
  field        TEXT NOT NULL,
  server_value TEXT NOT NULL CHECK (json_valid(server_value)),
  client_value TEXT NOT NULL CHECK (json_valid(client_value)),
  created_at   TEXT NOT NULL
) STRICT;

CREATE INDEX sync_conflict_team_idx ON sync_conflict (team_id, created_at);
