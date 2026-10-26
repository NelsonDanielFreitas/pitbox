-- Align the local team/setup boundary with the server and seed the v0.1 catalogs.
-- This is a separate migration because the F1-10 branch was already exercised locally.

ALTER TABLE team ADD COLUMN created_at TEXT NOT NULL DEFAULT '';
ALTER TABLE team ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
UPDATE team SET created_at = updated_at WHERE created_at = '';

CREATE TRIGGER team_length_insert
BEFORE INSERT ON team
WHEN length(NEW.name) NOT BETWEEN 1 AND 80
  OR length(NEW.category) NOT BETWEEN 1 AND 40
  OR (NEW.club IS NOT NULL AND length(NEW.club) NOT BETWEEN 1 AND 80)
BEGIN
  SELECT RAISE(ABORT, 'team field length');
END;

CREATE TRIGGER team_length_update
BEFORE UPDATE OF name, category, club ON team
WHEN length(NEW.name) NOT BETWEEN 1 AND 80
  OR length(NEW.category) NOT BETWEEN 1 AND 40
  OR (NEW.club IS NOT NULL AND length(NEW.club) NOT BETWEEN 1 AND 80)
BEGIN
  SELECT RAISE(ABORT, 'team field length');
END;

CREATE TRIGGER setup_version_object_insert
BEFORE INSERT ON setup_version
WHEN json_type(NEW.data) <> 'object'
BEGIN
  SELECT RAISE(ABORT, 'setup data must be an object');
END;

INSERT OR IGNORE INTO scale (code, name, active, sort_order)
VALUES ('1/8', '1/8', 1, 10);

INSERT OR IGNORE INTO powertrain (code, family, name, active, sort_order)
VALUES ('nitro', 'combustion', 'Nitro', 1, 10);
