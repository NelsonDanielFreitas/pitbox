-- F1-12 (ADR 0024): local extensions for the setup screens.
-- Forward-only: columns, an index, triggers and one local-only table.

-- Optimistic concurrency for name/notes edits (local only) and the author (user id only).
ALTER TABLE setup ADD COLUMN local_rev INTEGER NOT NULL DEFAULT 0 CHECK (local_rev >= 0);
ALTER TABLE setup ADD COLUMN created_by TEXT CHECK (created_by IS NULL OR length(created_by) = 36);
ALTER TABLE setup_version ADD COLUMN created_by TEXT CHECK (created_by IS NULL OR length(created_by) = 36);

CREATE INDEX setup_car_idx ON setup (team_id, car_id) WHERE deleted_at IS NULL;

-- Versions are append-only. Deletes are not blocked because the foreign keys cascade them
-- when a setup, a car or a team leaves the local database.
CREATE TRIGGER setup_version_immutable
BEFORE UPDATE ON setup_version
BEGIN
  SELECT RAISE(ABORT, 'setup versions are immutable');
END;

-- Driver feedback on a setup version. Local only: never pushed (the server keeps feedback
-- per session in events.driver_feedback, F2).
CREATE TABLE setup_feedback (
  id          TEXT PRIMARY KEY,
  team_id     TEXT NOT NULL,
  setup_id    TEXT NOT NULL,
  version     INTEGER NOT NULL,
  author_id   TEXT NOT NULL CHECK (length(author_id) = 36),
  steering_entry      INTEGER CHECK (steering_entry BETWEEN 1 AND 5),
  steering_mid        INTEGER CHECK (steering_mid BETWEEN 1 AND 5),
  steering_exit       INTEGER CHECK (steering_exit BETWEEN 1 AND 5),
  traction            INTEGER CHECK (traction BETWEEN 1 AND 5),
  jump_stability      INTEGER CHECK (jump_stability BETWEEN 1 AND 5),
  braking             INTEGER CHECK (braking BETWEEN 1 AND 5),
  engine_low_end      INTEGER CHECK (engine_low_end BETWEEN 1 AND 5),
  engine_top_end      INTEGER CHECK (engine_top_end BETWEEN 1 AND 5),
  track_zone  TEXT CHECK (track_zone IS NULL OR length(trim(track_zone)) BETWEEN 1 AND 80),
  note        TEXT CHECK (note IS NULL OR length(trim(note)) BETWEEN 1 AND 2000),
  created_at  TEXT NOT NULL,
  UNIQUE (team_id, id),
  FOREIGN KEY (team_id, setup_id, version)
    REFERENCES setup_version (team_id, setup_id, version) ON DELETE CASCADE,
  CHECK (
    steering_entry IS NOT NULL OR steering_mid IS NOT NULL OR steering_exit IS NOT NULL
    OR traction IS NOT NULL OR jump_stability IS NOT NULL OR braking IS NOT NULL
    OR engine_low_end IS NOT NULL OR engine_top_end IS NOT NULL
    OR note IS NOT NULL
  )
) STRICT;

CREATE INDEX setup_feedback_version_idx ON setup_feedback (team_id, setup_id, version, created_at);

-- Feedback is append-only too.
CREATE TRIGGER setup_feedback_immutable
BEFORE UPDATE ON setup_feedback
BEGIN
  SELECT RAISE(ABORT, 'setup feedback is immutable');
END;
