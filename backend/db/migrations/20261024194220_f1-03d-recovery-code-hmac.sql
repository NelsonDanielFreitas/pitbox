-- F1-03d: HMAC com chave rodável para os códigos de recuperação.
-- key_id permite saber quando uma chave mestra antiga já não é necessária.
ALTER TABLE identity.recovery_code
  ADD COLUMN key_id text NOT NULL DEFAULT 'legacy';

ALTER TABLE identity.recovery_code ALTER COLUMN key_id DROP DEFAULT; -- lint:allow o default 'legacy' só preenche linhas antigas; os novos INSERTs dão o key_id

ALTER TABLE identity.recovery_code
  ADD CONSTRAINT recovery_code_key_id_valid
  CHECK (key_id ~ '^[a-z0-9][a-z0-9-]{0,15}$') NOT VALID;
ALTER TABLE identity.recovery_code
  VALIDATE CONSTRAINT recovery_code_key_id_valid;

-- lint:allow o verificador de um só hash é substituído pela versão com key_id
DROP FUNCTION identity.replace_recovery_codes(uuid, bytea[]);

-- lint:allow o verificador de um só hash é substituído pela versão com bytea[]
DROP FUNCTION identity.consume_recovery_code(uuid, bytea);

CREATE FUNCTION identity.replace_recovery_codes(
  p_user_id uuid,
  p_code_hashes bytea[],
  p_key_id text
)
  RETURNS void
  LANGUAGE plpgsql
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  invalid int;
BEGIN
  IF p_key_id IS NULL OR p_key_id !~ '^[a-z0-9][a-z0-9-]{0,15}$' THEN
    RAISE EXCEPTION 'replace_recovery_codes: invalid key id' USING ERRCODE = '22023';
  END IF;
  IF p_code_hashes IS NULL OR cardinality(p_code_hashes) NOT BETWEEN 1 AND 16 THEN
    RAISE EXCEPTION 'replace_recovery_codes: invalid hash count' USING ERRCODE = '22023';
  END IF;
  SELECT count(*) INTO invalid
  FROM unnest(p_code_hashes) AS h
  WHERE h IS NULL OR length(h) != 32;
  IF invalid > 0 THEN
    RAISE EXCEPTION 'replace_recovery_codes: invalid hash length' USING ERRCODE = '22023';
  END IF;

  DELETE FROM identity.recovery_code
  WHERE user_id = p_user_id AND used_at IS NULL;

  INSERT INTO identity.recovery_code (user_id, code_hash, key_id)
  SELECT p_user_id, h, p_key_id
  FROM unnest(p_code_hashes) AS h;
END
$$;

REVOKE ALL ON FUNCTION identity.replace_recovery_codes(uuid, bytea[], text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.replace_recovery_codes(uuid, bytea[], text) TO pitbox_app;
COMMENT ON FUNCTION identity.replace_recovery_codes(uuid, bytea[], text) IS
  'Substitui os códigos de recuperação não usados de um utilizador. SECURITY DEFINER, só pitbox_app.';

CREATE FUNCTION identity.consume_recovery_code(p_user_id uuid, p_code_hashes bytea[])
  RETURNS boolean
  LANGUAGE plpgsql
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF p_code_hashes IS NULL OR cardinality(p_code_hashes) NOT BETWEEN 1 AND 16 THEN
    RAISE EXCEPTION 'consume_recovery_code: invalid hash count' USING ERRCODE = '22023';
  END IF;
  IF EXISTS (
    SELECT 1 FROM unnest(p_code_hashes) AS h
    WHERE h IS NULL OR octet_length(h) <> 32
  ) THEN
    RAISE EXCEPTION 'consume_recovery_code: invalid hash length' USING ERRCODE = '22023';
  END IF;

  UPDATE identity.recovery_code
     SET used_at = now()
   WHERE user_id = p_user_id
     AND code_hash = ANY(p_code_hashes)
     AND used_at IS NULL;

  RETURN FOUND;
END
$$;

REVOKE ALL ON FUNCTION identity.consume_recovery_code(uuid, bytea[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.consume_recovery_code(uuid, bytea[]) TO pitbox_app;
COMMENT ON FUNCTION identity.consume_recovery_code(uuid, bytea[]) IS
  'Consome um código de recuperação de MFA. SECURITY DEFINER, só pitbox_app.';
