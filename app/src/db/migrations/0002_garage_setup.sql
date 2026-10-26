CREATE TABLE IF NOT EXISTS scale (
  code       TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  active     INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
  sort_order INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE TABLE IF NOT EXISTS powertrain (
  code       TEXT PRIMARY KEY,
  family     TEXT NOT NULL,
  name       TEXT NOT NULL,
  active     INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
  sort_order INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE TABLE IF NOT EXISTS car (
  id                 TEXT PRIMARY KEY,
  team_id            TEXT NOT NULL REFERENCES team (id) ON DELETE CASCADE,
  make               TEXT NOT NULL,
  model              TEXT NOT NULL,
  model_year         INTEGER CHECK (model_year IS NULL OR model_year BETWEEN 1990 AND 2100),
  serial_number      TEXT,
  nickname           TEXT,
  transponder_number TEXT,
  status             TEXT NOT NULL CHECK (status IN ('active', 'spare', 'retired')),
  scale              TEXT NOT NULL REFERENCES scale (code),
  powertrain         TEXT NOT NULL REFERENCES powertrain (code),
  notes              TEXT,
  created_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL,
  version            INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
  deleted_at         TEXT,
  UNIQUE (team_id, id)
) STRICT;

CREATE UNIQUE INDEX IF NOT EXISTS car_transponder_idx
  ON car (team_id, transponder_number)
  WHERE transponder_number IS NOT NULL AND deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS engine (
  id                TEXT PRIMARY KEY,
  team_id           TEXT NOT NULL REFERENCES team (id) ON DELETE CASCADE,
  make              TEXT NOT NULL,
  model             TEXT NOT NULL,
  serial_number     TEXT,
  purchased_on      TEXT,
  status            TEXT NOT NULL CHECK (status IN ('active', 'spare', 'retired')),
  tanks_total       REAL NOT NULL DEFAULT 0 CHECK (tanks_total >= 0),
  run_seconds_total INTEGER NOT NULL DEFAULT 0 CHECK (run_seconds_total >= 0),
  sessions_total    INTEGER NOT NULL DEFAULT 0 CHECK (sessions_total >= 0),
  notes             TEXT,
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL,
  version           INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
  deleted_at        TEXT,
  UNIQUE (team_id, id)
) STRICT;

CREATE TABLE IF NOT EXISTS engine_mount (
  id          TEXT PRIMARY KEY,
  team_id     TEXT NOT NULL,
  engine_id   TEXT NOT NULL,
  car_id      TEXT NOT NULL,
  mounted_at  TEXT NOT NULL,
  unmounted_at TEXT,
  created_at  TEXT NOT NULL,
  FOREIGN KEY (team_id, engine_id) REFERENCES engine (team_id, id) ON DELETE CASCADE,
  FOREIGN KEY (team_id, car_id) REFERENCES car (team_id, id) ON DELETE CASCADE,
  CHECK (unmounted_at IS NULL OR unmounted_at > mounted_at)
) STRICT;

CREATE UNIQUE INDEX IF NOT EXISTS engine_mount_active_engine_idx
  ON engine_mount (engine_id) WHERE unmounted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS engine_mount_active_car_idx
  ON engine_mount (car_id) WHERE unmounted_at IS NULL;

CREATE TABLE IF NOT EXISTS tyre_set (
  id                TEXT PRIMARY KEY,
  team_id           TEXT NOT NULL REFERENCES team (id) ON DELETE CASCADE,
  label             TEXT NOT NULL,
  make              TEXT NOT NULL,
  model             TEXT NOT NULL,
  compound          TEXT,
  insert_type       TEXT,
  glued_on          TEXT,
  status            TEXT NOT NULL CHECK (status IN ('new', 'used', 'worn', 'retired')),
  sessions_total    INTEGER NOT NULL DEFAULT 0 CHECK (sessions_total >= 0),
  run_seconds_total INTEGER NOT NULL DEFAULT 0 CHECK (run_seconds_total >= 0),
  wear_notes        TEXT,
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL,
  version           INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
  deleted_at        TEXT,
  UNIQUE (team_id, id)
) STRICT;

CREATE TABLE IF NOT EXISTS part (
  id              TEXT PRIMARY KEY,
  team_id         TEXT NOT NULL REFERENCES team (id) ON DELETE CASCADE,
  kind            TEXT NOT NULL,
  brand           TEXT,
  reference       TEXT,
  description     TEXT NOT NULL,
  unit            TEXT NOT NULL DEFAULT 'un',
  specs           TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(specs)),
  min_stock       REAL NOT NULL DEFAULT 0 CHECK (min_stock >= 0),
  unit_cost_cents INTEGER CHECK (unit_cost_cents IS NULL OR unit_cost_cents >= 0),
  currency        TEXT NOT NULL DEFAULT 'EUR' CHECK (length(currency) = 3),
  has_service_life INTEGER NOT NULL DEFAULT 0 CHECK (has_service_life IN (0, 1)),
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  version         INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
  deleted_at      TEXT,
  UNIQUE (team_id, id)
) STRICT;

CREATE TABLE IF NOT EXISTS stock_movement (
  id         TEXT PRIMARY KEY,
  team_id    TEXT NOT NULL,
  part_id    TEXT NOT NULL,
  qty_delta  REAL NOT NULL CHECK (qty_delta <> 0),
  reason     TEXT NOT NULL,
  note       TEXT,
  occurred_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY (team_id, part_id) REFERENCES part (team_id, id) ON DELETE CASCADE
) STRICT;

CREATE TABLE IF NOT EXISTS part_install (
  id             TEXT PRIMARY KEY,
  team_id        TEXT NOT NULL,
  part_id        TEXT NOT NULL,
  car_id         TEXT,
  engine_id      TEXT,
  installed_at   TEXT NOT NULL,
  removed_at     TEXT,
  removal_reason TEXT,
  run_seconds    INTEGER NOT NULL DEFAULT 0 CHECK (run_seconds >= 0),
  sessions       INTEGER NOT NULL DEFAULT 0 CHECK (sessions >= 0),
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  version        INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
  FOREIGN KEY (team_id, part_id) REFERENCES part (team_id, id) ON DELETE CASCADE,
  FOREIGN KEY (team_id, car_id) REFERENCES car (team_id, id) ON DELETE CASCADE,
  FOREIGN KEY (team_id, engine_id) REFERENCES engine (team_id, id) ON DELETE CASCADE,
  CHECK ((car_id IS NULL) + (engine_id IS NULL) = 1),
  CHECK (removed_at IS NULL OR removed_at > installed_at)
) STRICT;

CREATE TABLE IF NOT EXISTS field_definition (
  id          TEXT PRIMARY KEY,
  team_id     TEXT NOT NULL REFERENCES team (id) ON DELETE CASCADE,
  key         TEXT NOT NULL,
  label       TEXT NOT NULL,
  group_key   TEXT NOT NULL,
  data_type   TEXT NOT NULL,
  unit        TEXT,
  min_value   REAL,
  max_value   REAL,
  options     TEXT,
  sort_order  INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  version     INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
  deleted_at  TEXT,
  UNIQUE (team_id, key),
  CHECK (min_value IS NULL OR max_value IS NULL OR min_value <= max_value),
  CHECK (options IS NULL OR json_valid(options))
) STRICT;

CREATE TABLE IF NOT EXISTS setup (
  id              TEXT PRIMARY KEY,
  team_id         TEXT NOT NULL REFERENCES team (id) ON DELETE CASCADE,
  car_id          TEXT NOT NULL,
  name            TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  current_version INTEGER NOT NULL DEFAULT 1 CHECK (current_version >= 1),
  notes           TEXT,
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  version         INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
  deleted_at      TEXT,
  UNIQUE (team_id, id),
  FOREIGN KEY (team_id, car_id) REFERENCES car (team_id, id) ON DELETE CASCADE
) STRICT;

CREATE TABLE IF NOT EXISTS setup_version (
  team_id           TEXT NOT NULL,
  setup_id          TEXT NOT NULL,
  version           INTEGER NOT NULL CHECK (version >= 1),
  data              TEXT NOT NULL CHECK (json_valid(data)),
  schema_version    INTEGER NOT NULL,
  based_on_setup_id TEXT,
  based_on_version  INTEGER,
  change_summary    TEXT,
  created_at        TEXT NOT NULL,
  PRIMARY KEY (setup_id, version),
  UNIQUE (team_id, setup_id, version),
  FOREIGN KEY (team_id, setup_id) REFERENCES setup (team_id, id) ON DELETE CASCADE,
  CHECK ((based_on_setup_id IS NULL) = (based_on_version IS NULL))
) STRICT;
