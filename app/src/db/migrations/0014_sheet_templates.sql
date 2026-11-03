-- F2-00 (ADR 0030): sheet templates per car model. Forward-only and additive: no existing row
-- is touched (setup_version rows stay as they are; v1 reads as the generic template).
-- team_id NULL = the global catalogue (read-only, replaced by the Pull snapshot).
CREATE TABLE sheet_template (
  id              TEXT PRIMARY KEY CHECK (length(id) = 36),
  team_id         TEXT CHECK (team_id IS NULL OR length(team_id) = 36),
  slug            TEXT NOT NULL,
  make            TEXT NOT NULL,
  model           TEXT NOT NULL,
  scale           TEXT NOT NULL,
  powertrain      TEXT NOT NULL,
  name            TEXT NOT NULL,
  source_url      TEXT,
  current_version INTEGER NOT NULL CHECK (current_version >= 1),
  based_on_id     TEXT,
  truncated       INTEGER NOT NULL DEFAULT 0 CHECK (truncated IN (0, 1)),
  version         INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
  updated_at      TEXT NOT NULL,
  deleted_at      TEXT
) STRICT;

CREATE INDEX sheet_template_lookup_idx ON sheet_template (make, model) WHERE deleted_at IS NULL;

-- Versions are immutable: a version never changes once published.
CREATE TABLE sheet_template_version (
  template_id TEXT NOT NULL REFERENCES sheet_template (id) ON DELETE CASCADE,
  version     INTEGER NOT NULL CHECK (version >= 1),
  definition  TEXT NOT NULL CHECK (json_valid(definition)),
  PRIMARY KEY (template_id, version)
) STRICT;

CREATE TRIGGER sheet_template_version_immutable
BEFORE UPDATE ON sheet_template_version
BEGIN
  SELECT RAISE(ABORT, 'sheet template versions are immutable');
END;

ALTER TABLE car ADD COLUMN sheet_template_id TEXT CHECK (sheet_template_id IS NULL OR length(sheet_template_id) = 36);
