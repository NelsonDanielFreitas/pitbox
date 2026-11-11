-- F2-06b (#158, ADR 0035): os códigos de recuperação como 2.º fator da recuperação de conta.
-- Quem pede a recuperação ainda não tem sessão: só se conhece o `sub` do Zitadel.

-- Tenta um código de recuperação para a conta com este `sub`. Devolve só um resultado fixo:
--   'unknown_user'  o PitBox não conhece a conta (nunca entrou)
--   'no_codes'      a conta não tem códigos por usar (não há 2.º fator a exigir)
--   'invalid'       tem códigos e nenhum dos candidatos serve (ou não veio nenhum)
--   'consumed'      um candidato serve e ficou gasto
-- O UPDATE bloqueia a linha: dois pedidos em paralelo com o mesmo código não passam os dois.
CREATE FUNCTION identity.attempt_recovery_by_subject(p_subject text, p_code_hashes bytea[])
  RETURNS text
  LANGUAGE plpgsql
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_user uuid;
BEGIN
  IF p_subject IS NULL OR length(p_subject) NOT BETWEEN 1 AND 255 THEN
    RAISE EXCEPTION 'attempt_recovery_by_subject: invalid subject' USING ERRCODE = '22023';
  END IF;
  IF p_code_hashes IS NULL OR cardinality(p_code_hashes) > 16 THEN
    RAISE EXCEPTION 'attempt_recovery_by_subject: invalid hash count' USING ERRCODE = '22023';
  END IF;
  IF EXISTS (SELECT 1 FROM unnest(p_code_hashes) AS h WHERE h IS NULL OR octet_length(h) <> 32) THEN
    RAISE EXCEPTION 'attempt_recovery_by_subject: invalid hash length' USING ERRCODE = '22023';
  END IF;

  SELECT id INTO v_user FROM identity.user_account WHERE idp_subject = p_subject AND deleted_at IS NULL;
  IF NOT FOUND THEN
    RETURN 'unknown_user';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM identity.recovery_code WHERE user_id = v_user AND used_at IS NULL) THEN
    RETURN 'no_codes';
  END IF;

  UPDATE identity.recovery_code
     SET used_at = now()
   WHERE user_id = v_user
     AND code_hash = ANY(p_code_hashes)
     AND used_at IS NULL;
  IF FOUND THEN
    RETURN 'consumed';
  END IF;
  RETURN 'invalid';
END
$$;

REVOKE ALL ON FUNCTION identity.attempt_recovery_by_subject(text, bytea[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.attempt_recovery_by_subject(text, bytea[]) TO pitbox_app;
COMMENT ON FUNCTION identity.attempt_recovery_by_subject(text, bytea[]) IS
  'Tenta um código de recuperação pelo sub do IdP (recuperação sem sessão). SECURITY DEFINER, só pitbox_app.';

-- Gasta os códigos que sobram: depois de uma recuperação o conjunto pode ter sido visto.
CREATE FUNCTION identity.invalidate_recovery_codes_by_subject(p_subject text)
  RETURNS integer
  LANGUAGE plpgsql
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_count integer;
BEGIN
  IF p_subject IS NULL OR length(p_subject) NOT BETWEEN 1 AND 255 THEN
    RAISE EXCEPTION 'invalidate_recovery_codes_by_subject: invalid subject' USING ERRCODE = '22023';
  END IF;
  UPDATE identity.recovery_code rc
     SET used_at = now()
    FROM identity.user_account u
   WHERE u.id = rc.user_id AND u.idp_subject = p_subject AND rc.used_at IS NULL;
  GET DIAGNOSTICS v_count = ROW_COUNT;
  RETURN v_count;
END
$$;

REVOKE ALL ON FUNCTION identity.invalidate_recovery_codes_by_subject(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.invalidate_recovery_codes_by_subject(text) TO pitbox_app;
COMMENT ON FUNCTION identity.invalidate_recovery_codes_by_subject(text) IS
  'Gasta os códigos de recuperação por usar da conta com este sub. SECURITY DEFINER, só pitbox_app.';
