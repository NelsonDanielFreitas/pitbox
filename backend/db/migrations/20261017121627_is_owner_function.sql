-- Add identity.is_owner for F1-03c MFA enforcement for team owners.
--
-- lint:allow create_function  new SECURITY DEFINER function, reviewed.

CREATE OR REPLACE FUNCTION identity.is_owner(p_user_id uuid)
  RETURNS boolean
  LANGUAGE plpgsql
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  RETURN EXISTS (
    SELECT 1 FROM identity.membership
    WHERE user_id = p_user_id AND role = 'owner'
  );
END
$$;

REVOKE ALL ON FUNCTION identity.is_owner(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.is_owner(uuid) TO pitbox_app;

COMMENT ON FUNCTION identity.is_owner(uuid) IS
  'Verifica se o utilizador é dono de alguma equipa. SECURITY DEFINER, só pitbox_app.';
