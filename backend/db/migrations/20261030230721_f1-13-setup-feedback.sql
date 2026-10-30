-- F1-13: driver feedback on a setup version now lives on the server (ADR 0026).
-- Expand migration: a new table, nothing existing changes. Append-only like setup_version.

CREATE TABLE setup.setup_feedback (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  setup_id            uuid        NOT NULL,
  version             integer     NOT NULL,
  author_id           uuid        NOT NULL REFERENCES identity.user_account (id),
  steering_entry      smallint    CHECK (steering_entry  BETWEEN 1 AND 5),
  steering_mid        smallint    CHECK (steering_mid    BETWEEN 1 AND 5),
  steering_exit       smallint    CHECK (steering_exit   BETWEEN 1 AND 5),
  traction            smallint    CHECK (traction        BETWEEN 1 AND 5),
  jump_stability      smallint    CHECK (jump_stability  BETWEEN 1 AND 5),
  braking             smallint    CHECK (braking         BETWEEN 1 AND 5),
  engine_low_end      smallint    CHECK (engine_low_end  BETWEEN 1 AND 5),
  engine_top_end      smallint    CHECK (engine_top_end  BETWEEN 1 AND 5),
  track_zone          text        CHECK (track_zone IS NULL OR char_length(btrim(track_zone)) BETWEEN 1 AND 80),
  note                text        CHECK (note IS NULL OR char_length(btrim(note)) BETWEEN 1 AND 2000),
  created_at          timestamptz NOT NULL DEFAULT now(),
  UNIQUE (team_id, id),
  FOREIGN KEY (team_id, setup_id, version)
    REFERENCES setup.setup_version (team_id, setup_id, version) ON DELETE CASCADE,
  CONSTRAINT setup_feedback_content_ck CHECK (
    num_nonnulls(steering_entry, steering_mid, steering_exit, traction, jump_stability,
                 braking, engine_low_end, engine_top_end, note) > 0
  )
);
CREATE INDEX setup_feedback_version_idx ON setup.setup_feedback (team_id, setup_id, version, created_at);

CREATE TRIGGER immutable BEFORE UPDATE OR DELETE ON setup.setup_feedback
  FOR EACH ROW EXECUTE FUNCTION app.forbid_mutation();
CREATE TRIGGER change_log AFTER INSERT OR UPDATE OR DELETE ON setup.setup_feedback
  FOR EACH ROW EXECUTE FUNCTION sync.log_change('id');
ALTER TABLE setup.setup_feedback ENABLE ROW LEVEL SECURITY;
CREATE POLICY team_isolation ON setup.setup_feedback
  USING (team_id = app.current_team_id()) WITH CHECK (team_id = app.current_team_id());
REVOKE UPDATE, DELETE ON setup.setup_feedback FROM pitbox_app;
