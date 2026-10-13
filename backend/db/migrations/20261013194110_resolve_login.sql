-- =============================================================================
-- identity.resolve_login — primeiro login (F1-03b, ADR 0013)
-- =============================================================================
-- No primeiro login ainda não existe app.user_id, por isso a RLS de user_account esconde
-- a linha por idp_subject e recusa o INSERT. Esta função é o único caminho: corre como
-- pitbox_owner (dono da tabela, isento da RLS enquanto ela não for FORCE) e por isso:
--   * procura SÓ por idp_subject (o "sub" do IdP) e nunca liga contas por email;
--   * devolve apenas o id do utilizador (nada de linhas nem de dados de outros);
--   * só pitbox_app pode executá-la (PUBLIC e pitbox_readonly não);
--   * search_path fixo e nomes qualificados (SECURITY DEFINER sem isto é uma escalada).
-- Erros (SQLSTATE próprios, mensagem fixa sem dados pessoais; a API mapeia-os e mostra ao
-- utilizador uma mensagem genérica):
--   22023  argumento inválido (sub ou email vazios ou demasiado longos)
--   PB001  o email já pertence a outra conta
--   PB002  a conta foi apagada
CREATE OR REPLACE FUNCTION identity.resolve_login(p_sub text, p_email citext, p_display_name text)
RETURNS uuid
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_id      uuid;
  v_email   public.citext;
  v_name    text;
  v_deleted timestamptz;
  v_new     text;
BEGIN
  IF p_sub IS NULL OR btrim(p_sub) = '' OR length(p_sub) > 255 THEN
    RAISE EXCEPTION 'resolve_login: invalid subject' USING ERRCODE = '22023';
  END IF;
  IF p_email IS NULL OR btrim(p_email::text) = '' OR length(p_email::text) > 320
     OR position('@' IN p_email::text) < 2 THEN
    RAISE EXCEPTION 'resolve_login: invalid email' USING ERRCODE = '22023';
  END IF;
  -- Nome 1..80 caracteres (CHECK da tabela); sem nome, usa a parte local do email.
  v_new := left(btrim(COALESCE(p_display_name, '')), 80);
  IF v_new = '' THEN
    v_new := left(split_part(p_email::text, '@', 1), 80);
  END IF;

  SELECT u.id, u.email, u.display_name, u.deleted_at
    INTO v_id, v_email, v_name, v_deleted
    FROM identity.user_account u
   WHERE u.idp_subject = p_sub
   FOR UPDATE;

  IF FOUND THEN
    IF v_deleted IS NOT NULL THEN
      RAISE EXCEPTION 'resolve_login: account deleted' USING ERRCODE = 'PB002';
    END IF;
    -- lower(): com este search_path os operadores do citext (em public) não são visíveis e a
    -- comparação direta seria sensível a maiúsculas.
    IF lower(v_email::text) <> lower(p_email::text) OR v_name <> v_new THEN
      BEGIN
        UPDATE identity.user_account
           SET email = p_email, display_name = v_new
         WHERE id = v_id;
      EXCEPTION WHEN unique_violation THEN
        RAISE EXCEPTION 'resolve_login: email in use' USING ERRCODE = 'PB001';
      END;
    END IF;
    RETURN v_id;
  END IF;

  BEGIN
    INSERT INTO identity.user_account (idp_subject, email, display_name)
    VALUES (p_sub, p_email, v_new)
    RETURNING id INTO v_id;
  EXCEPTION WHEN unique_violation THEN
    -- Ou o mesmo sub entrou entretanto (primeiros logins em paralelo: devolve o existente),
    -- ou o email é de outra conta.
    SELECT u.id INTO v_id FROM identity.user_account u
     WHERE u.idp_subject = p_sub AND u.deleted_at IS NULL;
    IF NOT FOUND THEN
      RAISE EXCEPTION 'resolve_login: email in use' USING ERRCODE = 'PB001';
    END IF;
  END;
  RETURN v_id;
END
$$;

REVOKE ALL ON FUNCTION identity.resolve_login(text, citext, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.resolve_login(text, citext, text) TO pitbox_app;
COMMENT ON FUNCTION identity.resolve_login(text, citext, text) IS
  'Primeiro login: devolve o id do utilizador do "sub" do IdP, criando-o se não existir. SECURITY DEFINER, só pitbox_app.';
