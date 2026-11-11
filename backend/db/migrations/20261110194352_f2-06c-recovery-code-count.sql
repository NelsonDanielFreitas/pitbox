-- F2-06b (#158, ADR 0034): quantos códigos de recuperação por usar tem o utilizador.
-- O `recovery_ready` de /auth/session precisa só deste número (nunca dos hashes).
CREATE FUNCTION identity.count_unused_recovery_codes(p_user_id uuid)
  RETURNS integer
  LANGUAGE sql
  STABLE
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
  SELECT count(*)::integer FROM identity.recovery_code WHERE user_id = p_user_id AND used_at IS NULL
$$;

REVOKE ALL ON FUNCTION identity.count_unused_recovery_codes(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.count_unused_recovery_codes(uuid) TO pitbox_app;
COMMENT ON FUNCTION identity.count_unused_recovery_codes(uuid) IS
  'Número de códigos de recuperação por usar. SECURITY DEFINER, só pitbox_app.';
