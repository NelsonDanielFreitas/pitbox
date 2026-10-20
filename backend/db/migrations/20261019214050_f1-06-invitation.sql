-- F1-06: restrições do convite e aceitação atómica (uso único, validade, email).
ALTER TABLE identity.invitation
  ADD CONSTRAINT invitation_token_hash_len CHECK (length(token_hash) = 32) NOT VALID;
ALTER TABLE identity.invitation VALIDATE CONSTRAINT invitation_token_hash_len;

ALTER TABLE identity.invitation
  ADD CONSTRAINT invitation_expires_after_created CHECK (expires_at > created_at) NOT VALID;
ALTER TABLE identity.invitation VALIDATE CONSTRAINT invitation_expires_after_created;

ALTER TABLE identity.invitation
  ADD CONSTRAINT invitation_team_id_uq UNIQUE (team_id, id);

-- Aceita um convite num só passo, sem contexto de equipa (quem aceita ainda não
-- é membro). A RLS esconderia o convite e recusaria a membership; esta é a única
-- porta estreita. Devolve a equipa, o papel e se o convite foi consumido.
-- Usa app.current_user_id(), nunca um parâmetro de utilizador.
-- Qualquer recusa usa o mesmo SQLSTATE PB010, para não distinguir inexistente,
-- expirado, revogado, usado ou de outra conta.
CREATE OR REPLACE FUNCTION identity.accept_invitation(p_token_hash bytea)
  RETURNS TABLE (out_team_id uuid, out_role identity.team_role, out_consumed boolean)
  LANGUAGE plpgsql
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_user     uuid := app.current_user_id();
  v_inv      identity.invitation%ROWTYPE;
  v_email    public.citext;
  v_role     identity.team_role;
  v_consumed boolean := false;
BEGIN
  IF v_user IS NULL THEN
    RAISE EXCEPTION 'accept_invitation: no user context' USING ERRCODE = 'PB010';
  END IF;
  IF p_token_hash IS NULL OR length(p_token_hash) <> 32 THEN
    RAISE EXCEPTION 'accept_invitation: invalid invitation' USING ERRCODE = 'PB010';
  END IF;

  SELECT * INTO v_inv
    FROM identity.invitation i
   WHERE i.token_hash = p_token_hash
   FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'accept_invitation: invalid invitation' USING ERRCODE = 'PB010';
  END IF;

  -- Já aceite por este utilizador: devolve a pertença existente sem mudar nada.
  IF v_inv.accepted_at IS NOT NULL THEN
    IF v_inv.accepted_by = v_user THEN
      SELECT m.role INTO v_role
        FROM identity.membership m
       WHERE m.team_id = v_inv.team_id AND m.user_id = v_user;
      IF FOUND THEN
        RETURN QUERY SELECT v_inv.team_id, v_role, false;
        RETURN;
      END IF;
    END IF;
    RAISE EXCEPTION 'accept_invitation: invalid invitation' USING ERRCODE = 'PB010';
  END IF;

  IF v_inv.revoked_at IS NOT NULL OR v_inv.expires_at <= now() THEN
    RAISE EXCEPTION 'accept_invitation: invalid invitation' USING ERRCODE = 'PB010';
  END IF;

  -- Convite por email: tem de coincidir com o email da conta. Com este
  -- search_path os operadores de citext não são visíveis: comparar com lower().
  IF v_inv.email IS NOT NULL THEN
    SELECT u.email INTO v_email
      FROM identity.user_account u
     WHERE u.id = v_user AND u.deleted_at IS NULL;
    IF NOT FOUND OR v_email IS NULL OR lower(v_email::text) <> lower(v_inv.email::text) THEN
      RAISE EXCEPTION 'accept_invitation: invalid invitation' USING ERRCODE = 'PB010';
    END IF;
  END IF;

  -- Já é membro por outra via: não consome o convite nem muda o papel.
  SELECT m.role INTO v_role
    FROM identity.membership m
   WHERE m.team_id = v_inv.team_id AND m.user_id = v_user;
  IF FOUND THEN
    RETURN QUERY SELECT v_inv.team_id, v_role, false;
    RETURN;
  END IF;

  UPDATE identity.invitation
     SET accepted_at = now(), accepted_by = v_user
   WHERE id = v_inv.id;

  INSERT INTO identity.membership (team_id, user_id, role, invited_by)
  VALUES (v_inv.team_id, v_user, v_inv.role, v_inv.created_by);

  v_role := v_inv.role;
  v_consumed := true;
  RETURN QUERY SELECT v_inv.team_id, v_role, v_consumed;
END
$$;

REVOKE ALL ON FUNCTION identity.accept_invitation(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.accept_invitation(bytea) TO pitbox_app;
COMMENT ON FUNCTION identity.accept_invitation(bytea) IS
  'Aceita um convite pelo hash do token, sem contexto de equipa. SECURITY DEFINER, só pitbox_app.';
