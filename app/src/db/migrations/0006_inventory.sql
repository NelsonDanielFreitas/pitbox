-- F1-11b (ADR 0023): align the local garage schema with the server inventory model
-- (F1-07b/c/d) and add the local-only extensions the inventory screens need.
-- Forward-only: every change adds a nullable column, a table or a trigger.

-- "Repor até": the restock target of a part. NULL means restock up to the minimum.
ALTER TABLE part ADD COLUMN reorder_to_centi INTEGER
  CHECK (reorder_to_centi IS NULL OR (
    reorder_to_centi >= min_stock_centi AND reorder_to_centi <= 9999999999
  ));

-- Price paid (purchases only), structured loss reason, store (local only, ADR 0023),
-- author (user id only, no names) and the link of an undo to the movement it reverts.
ALTER TABLE stock_movement ADD COLUMN unit_cost_cents INTEGER
  CHECK (unit_cost_cents IS NULL OR (unit_cost_cents >= 0 AND reason = 'purchase'));
ALTER TABLE stock_movement ADD COLUMN loss_reason TEXT
  CHECK (loss_reason IS NULL OR (
    loss_reason IN ('broken', 'worn', 'lost', 'expired') AND reason = 'lost'
  ));
ALTER TABLE stock_movement ADD COLUMN store TEXT
  CHECK (store IS NULL OR (length(trim(store)) BETWEEN 1 AND 80 AND reason = 'purchase'));
ALTER TABLE stock_movement ADD COLUMN created_by TEXT
  CHECK (created_by IS NULL OR length(created_by) = 36);
ALTER TABLE stock_movement ADD COLUMN reverts_movement_id TEXT
  REFERENCES stock_movement (id)
  CHECK (reverts_movement_id IS NULL OR reason = 'adjustment');

-- A movement can be undone only once.
CREATE UNIQUE INDEX stock_movement_reverts_idx
  ON stock_movement (reverts_movement_id) WHERE reverts_movement_id IS NOT NULL;

-- Rows created before this migration have no loss reason; new "lost" rows need one.
CREATE TRIGGER stock_movement_loss_reason_insert
BEFORE INSERT ON stock_movement
WHEN NEW.reason = 'lost' AND NEW.loss_reason IS NULL
BEGIN
  SELECT RAISE(ABORT, 'loss reason required');
END;

-- Movements are append-only. Deletes are not blocked here because the foreign keys cascade
-- them when a part or a team leaves the local database.
CREATE TRIGGER stock_movement_immutable
BEFORE UPDATE ON stock_movement
BEGIN
  SELECT RAISE(ABORT, 'stock movements are immutable');
END;

-- What happened to a dismounted unit (like the server's part_install_removal_ck).
ALTER TABLE part_install ADD COLUMN removal_destination TEXT
  CHECK (removal_destination IS NULL OR removal_destination IN ('stock', 'scrap', 'spare'));
ALTER TABLE part_install ADD COLUMN note TEXT
  CHECK (note IS NULL OR length(note) <= 500);

CREATE TRIGGER part_install_removal_validate
BEFORE UPDATE OF removed_at, removal_destination, removal_reason ON part_install
WHEN NEW.removed_at IS NULL AND (NEW.removal_destination IS NOT NULL OR NEW.removal_reason IS NOT NULL)
  OR NEW.removed_at IS NOT NULL AND (
    NEW.removal_destination IS NULL
    OR (NEW.removal_destination = 'scrap'
        AND COALESCE(NEW.removal_reason NOT IN ('broken', 'worn', 'lost'), 1))
    OR (NEW.removal_destination IN ('stock', 'spare') AND NEW.removal_reason IS NOT NULL)
  )
BEGIN
  SELECT RAISE(ABORT, 'part install removal');
END;

-- Team shopping list: only the exceptions to the computed list are stored (manual lines,
-- adjusted quantities and hidden automatic lines). Mirrors garage.shopping_item.
CREATE TABLE shopping_item (
  id          TEXT PRIMARY KEY,
  team_id     TEXT NOT NULL,
  part_id     TEXT NOT NULL,
  qty_centi   INTEGER CHECK (qty_centi IS NULL OR (qty_centi > 0 AND qty_centi <= 9999999999)),
  dismissed   INTEGER NOT NULL DEFAULT 0 CHECK (dismissed IN (0, 1)),
  note        TEXT CHECK (note IS NULL OR length(note) <= 500),
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  version     INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
  local_rev   INTEGER NOT NULL DEFAULT 0 CHECK (local_rev >= 0),
  UNIQUE (team_id, id),
  UNIQUE (team_id, part_id),
  FOREIGN KEY (team_id, part_id) REFERENCES part (team_id, id) ON DELETE CASCADE
) STRICT;

-- Inventory count draft: local only, never synchronised (ADR 0023). One open draft per team.
-- base_rowid is the last stock_movement rowid when the count started, to warn about
-- movements registered while counting.
CREATE TABLE inventory_count (
  team_id     TEXT PRIMARY KEY REFERENCES team (id) ON DELETE CASCADE,
  scope_kind  TEXT CHECK (scope_kind IS NULL OR scope_kind IN (
    'glow_plug', 'clutch_shoe', 'clutch_spring', 'clutch_bell', 'bearing', 'conrod',
    'piston_sleeve', 'air_filter', 'shock_oil', 'diff_oil', 'fuel', 'spring', 'tyre',
    'insert', 'body', 'wing', 'other'
  )),
  started_at  TEXT NOT NULL,
  base_rowid  INTEGER NOT NULL CHECK (base_rowid >= 0)
) STRICT;

CREATE TABLE inventory_count_line (
  team_id        TEXT NOT NULL,
  part_id        TEXT NOT NULL,
  counted_centi  INTEGER NOT NULL CHECK (counted_centi BETWEEN 0 AND 9999999999),
  PRIMARY KEY (team_id, part_id),
  FOREIGN KEY (team_id) REFERENCES inventory_count (team_id) ON DELETE CASCADE,
  FOREIGN KEY (team_id, part_id) REFERENCES part (team_id, id) ON DELETE CASCADE
) STRICT;
