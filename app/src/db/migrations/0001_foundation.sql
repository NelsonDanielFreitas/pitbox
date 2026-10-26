CREATE TABLE IF NOT EXISTS app_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS team (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  category     TEXT NOT NULL CHECK (length(category) BETWEEN 1 AND 80),
  club         TEXT,
  country_code TEXT NOT NULL CHECK (length(country_code) = 2),
  role         TEXT NOT NULL CHECK (role IN ('owner', 'mechanic', 'pilot', 'guest', 'unknown')),
  updated_at   TEXT NOT NULL,
  deleted_at   TEXT
) STRICT;

CREATE INDEX IF NOT EXISTS team_name_idx ON team (name COLLATE NOCASE) WHERE deleted_at IS NULL;
