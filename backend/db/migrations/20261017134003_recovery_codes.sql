-- Códigos de recuperação de MFA: guardados apenas como hash SHA-256.
CREATE TABLE identity.recovery_code (
  id          uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  user_id     uuid        NOT NULL REFERENCES identity.user_account (id) ON DELETE CASCADE,
  code_hash   bytea       NOT NULL CHECK (length(code_hash) = 32),
  used_at     timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, code_hash)
);

CREATE INDEX recovery_code_user_idx ON identity.recovery_code (user_id) WHERE used_at IS NULL;

ALTER TABLE identity.recovery_code ENABLE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON identity.recovery_code
  USING (user_id = app.current_user_id()) WITH CHECK (user_id = app.current_user_id());

-- Substitui os códigos não usados do utilizador (SECURITY DEFINER, sem contexto de app.user_id).
CREATE OR REPLACE FUNCTION identity.replace_recovery_codes(p_user_id uuid, p_code_hashes bytea[])
  RETURNS void
  LANGUAGE sql
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
  DELETE FROM identity.recovery_code
  WHERE user_id = p_user_id AND used_at IS NULL;

  INSERT INTO identity.recovery_code (user_id, code_hash)
  SELECT p_user_id, h
  FROM unnest(p_code_hashes) AS h
  WHERE length(h) = 32;
$$;

REVOKE ALL ON FUNCTION identity.replace_recovery_codes(uuid, bytea[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.replace_recovery_codes(uuid, bytea[]) TO pitbox_app;
COMMENT ON FUNCTION identity.replace_recovery_codes(uuid, bytea[]) IS
  'Substitui os códigos de recuperação não usados de um utilizador. SECURITY DEFINER, só pitbox_app.';

-- Consome um código de recuperação se existir e não estiver usado.
CREATE OR REPLACE FUNCTION identity.consume_recovery_code(p_user_id uuid, p_code_hash bytea)
  RETURNS boolean
  LANGUAGE sql
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
  WITH upd AS (
    UPDATE identity.recovery_code
    SET used_at = now()
    WHERE user_id = p_user_id
      AND code_hash = p_code_hash
      AND used_at IS NULL
    RETURNING id
  )
  SELECT count(*) > 0 FROM upd;
$$;

REVOKE ALL ON FUNCTION identity.consume_recovery_code(uuid, bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.consume_recovery_code(uuid, bytea) TO pitbox_app;
COMMENT ON FUNCTION identity.consume_recovery_code(uuid, bytea) IS
  'Consome um código de recuperação de MFA. SECURITY DEFINER, só pitbox_app.';
