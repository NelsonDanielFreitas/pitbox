-- F2-00 (ADR 0030): sheet templates per car model. Expand only: new tables, a new nullable
-- column, a new view and a new function; nothing existing is dropped, renamed or rewritten.

-- Template identity. team_id NULL = global catalogue (written only by reviewed migrations).
CREATE TABLE setup.sheet_template (
  id              uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id         uuid        REFERENCES identity.team (id) ON DELETE CASCADE,
  slug            text        NOT NULL CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
  make            text        NOT NULL CHECK (char_length(make) BETWEEN 1 AND 80),
  model           text        NOT NULL CHECK (char_length(model) BETWEEN 1 AND 80),
  scale           text        NOT NULL CONSTRAINT sheet_template_scale_fk REFERENCES garage.scale (code),
  powertrain      text        NOT NULL CONSTRAINT sheet_template_powertrain_fk REFERENCES garage.powertrain (code),
  name            text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
  source_url      text        CHECK (source_url IS NULL OR (source_url ~ '^https://' AND char_length(source_url) <= 500)),
  current_version integer     NOT NULL DEFAULT 1 CHECK (current_version >= 1),
  based_on_id     uuid        REFERENCES setup.sheet_template (id),
  created_by      uuid        REFERENCES identity.user_account (id),
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  version         integer     NOT NULL DEFAULT 1,
  deleted_at      timestamptz,
  UNIQUE (team_id, id),
  CHECK (based_on_id IS NULL OR team_id IS NOT NULL)
);
CREATE UNIQUE INDEX sheet_template_global_slug ON setup.sheet_template (slug) WHERE team_id IS NULL;
CREATE UNIQUE INDEX sheet_template_team_slug ON setup.sheet_template (team_id, slug) WHERE team_id IS NOT NULL;

-- Immutable versions of a template. team_id is copied from the template by a trigger (for the RLS).
CREATE TABLE setup.sheet_template_version (
  template_id     uuid        NOT NULL REFERENCES setup.sheet_template (id) ON DELETE CASCADE,
  team_id         uuid,
  version         integer     NOT NULL CHECK (version >= 1),
  definition      jsonb       NOT NULL CHECK (jsonb_typeof(definition) = 'object'
                                              AND octet_length(definition::text) <= 262144),
  created_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (template_id, version)
);

-- The current version must exist. Deferred so a template and its first version go in together.
ALTER TABLE setup.sheet_template
  ADD CONSTRAINT sheet_template_current_version_fk FOREIGN KEY (id, current_version)
  REFERENCES setup.sheet_template_version (template_id, version) DEFERRABLE INITIALLY DEFERRED;

-- A car may point to a template. A plain FK ignores the RLS and would accept another team's private
-- template, so the check is a trigger (below).
ALTER TABLE garage.car ADD COLUMN sheet_template_id uuid;

CREATE FUNCTION setup.sheet_template_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    -- A team copy may only be based on a visible global template (RLS hides other teams' ones).
    IF NEW.based_on_id IS NOT NULL AND NOT EXISTS (
         SELECT 1 FROM setup.sheet_template g WHERE g.id = NEW.based_on_id AND g.team_id IS NULL) THEN
      RAISE EXCEPTION 'sheet template not available' USING ERRCODE = 'PB020';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.id <> OLD.id OR NEW.team_id IS DISTINCT FROM OLD.team_id OR NEW.based_on_id IS DISTINCT FROM OLD.based_on_id
     OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at <> OLD.created_at
     OR NEW.current_version < OLD.current_version THEN
    RAISE EXCEPTION 'sheet_template: identity columns are immutable and current_version only grows'
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END
$$;

CREATE FUNCTION setup.sheet_template_version_team() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE t uuid;
BEGIN
  SELECT s.team_id INTO t FROM setup.sheet_template s WHERE s.id = NEW.template_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'sheet template not available' USING ERRCODE = 'PB020';
  END IF;
  NEW.team_id := t;
  RETURN NEW;
END
$$;

CREATE FUNCTION garage.check_car_sheet_template() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.sheet_template_id IS NULL THEN RETURN NEW; END IF;
  IF TG_OP = 'UPDATE' AND NEW.sheet_template_id IS NOT DISTINCT FROM OLD.sheet_template_id THEN RETURN NEW; END IF;
  IF NOT EXISTS (SELECT 1 FROM setup.sheet_template t
                  WHERE t.id = NEW.sheet_template_id AND t.deleted_at IS NULL
                    AND (t.team_id IS NULL OR t.team_id = NEW.team_id)) THEN
    RAISE EXCEPTION 'sheet template not available' USING ERRCODE = 'PB020';
  END IF;
  RETURN NEW;
END
$$;

REVOKE ALL ON FUNCTION setup.sheet_template_guard() FROM PUBLIC;
REVOKE ALL ON FUNCTION setup.sheet_template_version_team() FROM PUBLIC;
REVOKE ALL ON FUNCTION garage.check_car_sheet_template() FROM PUBLIC;

CREATE TRIGGER guard BEFORE INSERT OR UPDATE ON setup.sheet_template
  FOR EACH ROW EXECUTE FUNCTION setup.sheet_template_guard();
CREATE TRIGGER touch BEFORE UPDATE ON setup.sheet_template
  FOR EACH ROW EXECUTE FUNCTION app.touch();
CREATE TRIGGER team_sync BEFORE INSERT ON setup.sheet_template_version
  FOR EACH ROW EXECUTE FUNCTION setup.sheet_template_version_team();
CREATE TRIGGER immutable BEFORE UPDATE OR DELETE ON setup.sheet_template_version
  FOR EACH ROW EXECUTE FUNCTION app.forbid_mutation();
CREATE TRIGGER sheet_template_check BEFORE INSERT OR UPDATE OF sheet_template_id ON garage.car
  FOR EACH ROW EXECUTE FUNCTION garage.check_car_sheet_template();

-- RLS: everybody reads the global catalogue; a team writes only its own rows. Separate policies per
-- command: with a single FOR ALL policy (as in events.track) an UPDATE could move a global row to
-- the caller's team, because the WITH CHECK only looks at the new row.
ALTER TABLE setup.sheet_template ENABLE ROW LEVEL SECURITY;
CREATE POLICY template_select ON setup.sheet_template
  FOR SELECT USING (team_id IS NULL OR team_id = app.current_team_id());
CREATE POLICY template_insert ON setup.sheet_template
  FOR INSERT WITH CHECK (team_id = app.current_team_id());
CREATE POLICY template_update ON setup.sheet_template
  FOR UPDATE USING (team_id = app.current_team_id()) WITH CHECK (team_id = app.current_team_id());
ALTER TABLE setup.sheet_template_version ENABLE ROW LEVEL SECURITY;
CREATE POLICY template_version_select ON setup.sheet_template_version
  FOR SELECT USING (team_id IS NULL OR team_id = app.current_team_id());
CREATE POLICY template_version_insert ON setup.sheet_template_version
  FOR INSERT WITH CHECK (team_id = app.current_team_id());

REVOKE DELETE ON setup.sheet_template FROM pitbox_app;
REVOKE UPDATE, DELETE ON setup.sheet_template_version FROM pitbox_app;

-- Analysis columns for v1 and v2 (D1). The 7 GENERATED columns stay as they are; v2 rows carry the
-- server-computed data.canonical. security_invoker: without it the view would ignore the RLS.
CREATE VIEW setup.setup_version_metrics WITH (security_invoker = true) AS
SELECT v.team_id, v.setup_id, v.version, v.schema_version,
  COALESCE(v.diff_front_cst, CASE WHEN jsonb_typeof(v.data #> '{canonical,diffs.front_cst}') = 'number'
    THEN (v.data #>> '{canonical,diffs.front_cst}')::numeric::integer END) AS diff_front_cst,
  COALESCE(v.diff_center_cst, CASE WHEN jsonb_typeof(v.data #> '{canonical,diffs.center_cst}') = 'number'
    THEN (v.data #>> '{canonical,diffs.center_cst}')::numeric::integer END) AS diff_center_cst,
  COALESCE(v.diff_rear_cst, CASE WHEN jsonb_typeof(v.data #> '{canonical,diffs.rear_cst}') = 'number'
    THEN (v.data #>> '{canonical,diffs.rear_cst}')::numeric::integer END) AS diff_rear_cst,
  COALESCE(v.shock_front_oil_cst, CASE WHEN jsonb_typeof(v.data #> '{canonical,shocks.front.oil_cst}') = 'number'
    THEN (v.data #>> '{canonical,shocks.front.oil_cst}')::numeric::integer END) AS shock_front_oil_cst,
  COALESCE(v.shock_rear_oil_cst, CASE WHEN jsonb_typeof(v.data #> '{canonical,shocks.rear.oil_cst}') = 'number'
    THEN (v.data #>> '{canonical,shocks.rear.oil_cst}')::numeric::integer END) AS shock_rear_oil_cst,
  COALESCE(v.clutch_bell_teeth, CASE WHEN jsonb_typeof(v.data #> '{canonical,transmission.clutch_bell_teeth}') = 'number'
    THEN (v.data #>> '{canonical,transmission.clutch_bell_teeth}')::numeric::smallint END) AS clutch_bell_teeth,
  COALESCE(v.spur_teeth, CASE WHEN jsonb_typeof(v.data #> '{canonical,transmission.spur_teeth}') = 'number'
    THEN (v.data #>> '{canonical,transmission.spur_teeth}')::numeric::smallint END) AS spur_teeth
FROM setup.setup_version v;

-- setup.open_share_link_v2 — same as open_share_link plus the definition of the template version
-- the shared version was made with (NULL for v1). A new function instead of changing the RETURNS
-- TABLE of the old one, which would need a DROP.
CREATE FUNCTION setup.open_share_link_v2(p_token_hash bytea)
  RETURNS TABLE (
    name text,
    version integer,
    schema_version integer,
    data jsonb,
    change_summary text,
    version_created_at timestamptz,
    template_definition jsonb
  )
  LANGUAGE sql
  VOLATILE
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
  WITH opened AS (
    UPDATE setup.share_link l
       SET last_used_at = now(), use_count = use_count + 1
     WHERE l.token_hash = p_token_hash
       AND octet_length(p_token_hash) = 32
       AND l.revoked_at IS NULL
       AND (l.expires_at IS NULL OR l.expires_at > now())
       AND EXISTS (
         SELECT 1
           FROM setup.setup s
           JOIN identity.team t ON t.id = l.team_id AND t.deleted_at IS NULL
           JOIN setup.setup_version v
             ON v.team_id = s.team_id
            AND v.setup_id = s.id
            AND v.version = COALESCE(l.pinned_version, s.current_version)
          WHERE s.team_id = l.team_id
            AND s.id = l.setup_id
            AND s.deleted_at IS NULL
       )
    RETURNING l.team_id, l.setup_id, l.pinned_version
  )
  SELECT s.name, v.version, v.schema_version, v.data, v.change_summary, v.created_at, tv.definition
    FROM opened o
    JOIN setup.setup s ON s.team_id = o.team_id AND s.id = o.setup_id
    JOIN setup.setup_version v
      ON v.team_id = s.team_id
     AND v.setup_id = s.id
     AND v.version = COALESCE(o.pinned_version, s.current_version)
    LEFT JOIN setup.sheet_template_version tv
      ON v.schema_version = 2
     AND jsonb_typeof(v.data #> '{template,version}') = 'number'
     AND tv.template_id = CASE WHEN (v.data #>> '{template,id}') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                               THEN (v.data #>> '{template,id}')::uuid END
     AND tv.version = CASE WHEN (v.data #>> '{template,version}') ~ '^[0-9]{1,9}$'
                           THEN (v.data #>> '{template,version}')::integer END
     AND (tv.team_id IS NULL OR tv.team_id = s.team_id);
$$;

REVOKE ALL ON FUNCTION setup.open_share_link_v2(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION setup.open_share_link_v2(bytea) TO pitbox_app;
COMMENT ON FUNCTION setup.open_share_link_v2(bytea) IS
  'Como open_share_link, mais a definição do modelo da versão partilhada. SECURITY DEFINER, só pitbox_app.';
