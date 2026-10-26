-- F1-11: tighten garage invariants and store stock in integer centi.
--
-- The local part tables were created in 0002 but no F1-10 code wrote to them. They are
-- rebuilt here so the local schema matches the server/proto centi representation and can use
-- real CHECK constraints instead of triggers. SQLite cannot change column types in place.

CREATE TABLE part_new (
  id               TEXT PRIMARY KEY,
  team_id          TEXT NOT NULL REFERENCES team (id) ON DELETE CASCADE,
  kind             TEXT NOT NULL CHECK (kind IN (
    'glow_plug', 'clutch_shoe', 'clutch_spring', 'clutch_bell', 'bearing', 'conrod',
    'piston_sleeve', 'air_filter', 'shock_oil', 'diff_oil', 'fuel', 'spring', 'tyre',
    'insert', 'body', 'wing', 'other'
  )),
  brand            TEXT CHECK (brand IS NULL OR length(brand) <= 80),
  reference        TEXT CHECK (reference IS NULL OR length(reference) <= 80),
  description      TEXT NOT NULL CHECK (length(description) BETWEEN 1 AND 200),
  unit             TEXT NOT NULL DEFAULT 'un' CHECK (unit IN ('un', 'ml', 'l', 'g')),
  specs            TEXT NOT NULL DEFAULT '{}'
                     CHECK (json_valid(specs) AND json_type(specs) = 'object'),
  min_stock_centi  INTEGER NOT NULL DEFAULT 0
                     CHECK (min_stock_centi BETWEEN 0 AND 9999999999),
  unit_cost_cents  INTEGER CHECK (unit_cost_cents IS NULL OR unit_cost_cents >= 0),
  currency         TEXT NOT NULL DEFAULT 'EUR'
                     CHECK (length(currency) = 3 AND currency = upper(currency)),
  has_service_life INTEGER NOT NULL DEFAULT 0 CHECK (has_service_life IN (0, 1)),
  created_at       TEXT NOT NULL,
  updated_at       TEXT NOT NULL,
  version          INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
  local_rev        INTEGER NOT NULL DEFAULT 0 CHECK (local_rev >= 0),
  deleted_at       TEXT,
  UNIQUE (team_id, id)
) STRICT;

INSERT INTO part_new (
  id, team_id, kind, brand, reference, description, unit, specs, min_stock_centi,
  unit_cost_cents, currency, has_service_life, created_at, updated_at, version,
  local_rev, deleted_at
)
SELECT
  id, team_id, kind, brand, reference, description, unit, specs,
  CAST(round(min_stock * 100) AS INTEGER),
  unit_cost_cents, currency, has_service_life, created_at, updated_at, version,
  0, deleted_at
FROM part;

CREATE TABLE stock_movement_new (
  id               TEXT PRIMARY KEY,
  team_id          TEXT NOT NULL,
  part_id          TEXT NOT NULL,
  qty_delta_centi  INTEGER NOT NULL
                     CHECK (qty_delta_centi <> 0 AND qty_delta_centi BETWEEN -9999999999 AND 9999999999),
  reason           TEXT NOT NULL CHECK (reason IN (
    'purchase', 'consumed', 'installed', 'adjustment', 'lost', 'returned'
  )),
  note             TEXT CHECK (note IS NULL OR length(note) <= 500),
  occurred_at      TEXT NOT NULL,
  created_at       TEXT NOT NULL,
  FOREIGN KEY (team_id, part_id) REFERENCES part_new (team_id, id) ON DELETE CASCADE,
  CHECK (
    (reason IN ('purchase', 'returned') AND qty_delta_centi > 0) OR
    (reason IN ('consumed', 'installed', 'lost') AND qty_delta_centi < 0) OR
    (reason = 'adjustment' AND qty_delta_centi <> 0)
  )
) STRICT;

INSERT INTO stock_movement_new (
  id, team_id, part_id, qty_delta_centi, reason, note, occurred_at, created_at
)
SELECT
  id, team_id, part_id, CAST(round(qty_delta * 100) AS INTEGER), reason, note,
  occurred_at, created_at
FROM stock_movement;

CREATE TABLE part_install_new (
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
  FOREIGN KEY (team_id, part_id) REFERENCES part_new (team_id, id) ON DELETE CASCADE,
  FOREIGN KEY (team_id, car_id) REFERENCES car (team_id, id) ON DELETE CASCADE,
  FOREIGN KEY (team_id, engine_id) REFERENCES engine (team_id, id) ON DELETE CASCADE,
  CHECK ((car_id IS NULL) + (engine_id IS NULL) = 1),
  CHECK (removed_at IS NULL OR removed_at > installed_at)
) STRICT;

INSERT INTO part_install_new (
  id, team_id, part_id, car_id, engine_id, installed_at, removed_at, removal_reason,
  run_seconds, sessions, created_at, updated_at, version
)
SELECT
  id, team_id, part_id, car_id, engine_id, installed_at, removed_at, removal_reason,
  run_seconds, sessions, created_at, updated_at, version
FROM part_install;

-- Drop children first: PRAGMA foreign_keys=OFF is ignored inside the migration transaction.
DROP TABLE part_install;
DROP TABLE stock_movement;
DROP TABLE part;

ALTER TABLE part_new RENAME TO part;
ALTER TABLE stock_movement_new RENAME TO stock_movement;
ALTER TABLE part_install_new RENAME TO part_install;

ALTER TABLE car ADD COLUMN local_rev INTEGER NOT NULL DEFAULT 0 CHECK (local_rev >= 0);
ALTER TABLE engine ADD COLUMN local_rev INTEGER NOT NULL DEFAULT 0 CHECK (local_rev >= 0);
ALTER TABLE tyre_set ADD COLUMN local_rev INTEGER NOT NULL DEFAULT 0 CHECK (local_rev >= 0);

DROP INDEX IF EXISTS engine_mount_active_engine_idx;
DROP INDEX IF EXISTS engine_mount_active_car_idx;

CREATE UNIQUE INDEX engine_mount_active_engine_idx
  ON engine_mount (team_id, engine_id) WHERE unmounted_at IS NULL;
CREATE UNIQUE INDEX engine_mount_active_car_idx
  ON engine_mount (team_id, car_id) WHERE unmounted_at IS NULL;

CREATE UNIQUE INDEX tyre_set_label_idx
  ON tyre_set (team_id, label) WHERE deleted_at IS NULL AND status <> 'retired';

CREATE INDEX car_team_idx ON car (team_id) WHERE deleted_at IS NULL;
CREATE INDEX engine_team_idx ON engine (team_id) WHERE deleted_at IS NULL;
CREATE INDEX part_team_description_idx
  ON part (team_id, description COLLATE NOCASE) WHERE deleted_at IS NULL;
CREATE INDEX engine_mount_engine_history_idx
  ON engine_mount (team_id, engine_id, mounted_at DESC);
CREATE INDEX part_install_active_idx
  ON part_install (team_id, part_id) WHERE removed_at IS NULL;

CREATE TRIGGER car_validate_insert
BEFORE INSERT ON car
WHEN length(trim(NEW.make)) NOT BETWEEN 1 AND 80
  OR length(trim(NEW.model)) NOT BETWEEN 1 AND 80
  OR (NEW.serial_number IS NOT NULL AND length(NEW.serial_number) > 64)
  OR (NEW.nickname IS NOT NULL AND length(NEW.nickname) > 40)
  OR (NEW.transponder_number IS NOT NULL AND length(NEW.transponder_number) > 64)
  OR (NEW.notes IS NOT NULL AND length(NEW.notes) > 2000)
BEGIN
  SELECT RAISE(ABORT, 'car field length');
END;

CREATE TRIGGER car_validate_update
BEFORE UPDATE OF make, model, serial_number, nickname, transponder_number, notes ON car
WHEN length(trim(NEW.make)) NOT BETWEEN 1 AND 80
  OR length(trim(NEW.model)) NOT BETWEEN 1 AND 80
  OR (NEW.serial_number IS NOT NULL AND length(NEW.serial_number) > 64)
  OR (NEW.nickname IS NOT NULL AND length(NEW.nickname) > 40)
  OR (NEW.transponder_number IS NOT NULL AND length(NEW.transponder_number) > 64)
  OR (NEW.notes IS NOT NULL AND length(NEW.notes) > 2000)
BEGIN
  SELECT RAISE(ABORT, 'car field length');
END;

CREATE TRIGGER engine_validate_insert
BEFORE INSERT ON engine
WHEN length(trim(NEW.make)) NOT BETWEEN 1 AND 80
  OR length(trim(NEW.model)) NOT BETWEEN 1 AND 80
  OR (NEW.serial_number IS NOT NULL AND length(NEW.serial_number) > 64)
  OR (NEW.purchased_on IS NOT NULL AND (
    NEW.purchased_on NOT GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'
    OR date(NEW.purchased_on) <> NEW.purchased_on
  ))
  OR (NEW.notes IS NOT NULL AND length(NEW.notes) > 2000)
BEGIN
  SELECT RAISE(ABORT, 'engine field length or date');
END;

CREATE TRIGGER engine_validate_update
BEFORE UPDATE OF make, model, serial_number, purchased_on, notes ON engine
WHEN length(trim(NEW.make)) NOT BETWEEN 1 AND 80
  OR length(trim(NEW.model)) NOT BETWEEN 1 AND 80
  OR (NEW.serial_number IS NOT NULL AND length(NEW.serial_number) > 64)
  OR (NEW.purchased_on IS NOT NULL AND (
    NEW.purchased_on NOT GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'
    OR date(NEW.purchased_on) <> NEW.purchased_on
  ))
  OR (NEW.notes IS NOT NULL AND length(NEW.notes) > 2000)
BEGIN
  SELECT RAISE(ABORT, 'engine field length or date');
END;

CREATE TRIGGER tyre_set_validate_insert
BEFORE INSERT ON tyre_set
WHEN length(trim(NEW.label)) NOT BETWEEN 1 AND 20
  OR length(trim(NEW.make)) NOT BETWEEN 1 AND 80
  OR length(trim(NEW.model)) NOT BETWEEN 1 AND 80
  OR (NEW.compound IS NOT NULL AND length(NEW.compound) > 40)
  OR (NEW.insert_type IS NOT NULL AND length(NEW.insert_type) > 40)
  OR (NEW.glued_on IS NOT NULL AND (
    NEW.glued_on NOT GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'
    OR date(NEW.glued_on) <> NEW.glued_on
  ))
  OR (NEW.wear_notes IS NOT NULL AND length(NEW.wear_notes) > 2000)
BEGIN
  SELECT RAISE(ABORT, 'tyre set field length or date');
END;

CREATE TRIGGER tyre_set_validate_update
BEFORE UPDATE OF label, make, model, compound, insert_type, glued_on, wear_notes ON tyre_set
WHEN length(trim(NEW.label)) NOT BETWEEN 1 AND 20
  OR length(trim(NEW.make)) NOT BETWEEN 1 AND 80
  OR length(trim(NEW.model)) NOT BETWEEN 1 AND 80
  OR (NEW.compound IS NOT NULL AND length(NEW.compound) > 40)
  OR (NEW.insert_type IS NOT NULL AND length(NEW.insert_type) > 40)
  OR (NEW.glued_on IS NOT NULL AND (
    NEW.glued_on NOT GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'
    OR date(NEW.glued_on) <> NEW.glued_on
  ))
  OR (NEW.wear_notes IS NOT NULL AND length(NEW.wear_notes) > 2000)
BEGIN
  SELECT RAISE(ABORT, 'tyre set field length or date');
END;
