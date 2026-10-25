-- F1-08b: links de partilha revogáveis e leitura pública estreita.
-- Migração aditiva: a tabela setup.share_link já existe desde o esquema inicial.

ALTER TABLE setup.share_link
  ADD CONSTRAINT share_link_team_id_uq UNIQUE (team_id, id),
  ADD CONSTRAINT share_link_pinned_version_fk
    FOREIGN KEY (team_id, setup_id, pinned_version)
    REFERENCES setup.setup_version (team_id, setup_id, version) ON DELETE CASCADE,
  ADD CONSTRAINT share_link_expiry_ck
    CHECK (expires_at IS NULL OR expires_at > created_at);

-- lint:allow tabela criada no esquema inicial, vazia e ainda sem produção; o índice é pequeno
CREATE INDEX share_link_setup_idx ON setup.share_link (team_id, setup_id);

-- Hashes de tokens e metadados de partilha não são dados de análise.
REVOKE SELECT ON setup.share_link FROM pitbox_readonly;

-- Fica depois do GRANT EXECUTE ON ALL FUNCTIONS no schema.sql; aqui a função é
-- criada depois das permissões, por isso o REVOKE explícito basta.
CREATE FUNCTION setup.open_share_link(p_token_hash bytea)
  RETURNS TABLE (
    name text,
    version integer,
    schema_version integer,
    data jsonb,
    change_summary text,
    version_created_at timestamptz
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
  SELECT s.name, v.version, v.schema_version, v.data, v.change_summary, v.created_at
    FROM opened o
    JOIN setup.setup s ON s.team_id = o.team_id AND s.id = o.setup_id
    JOIN setup.setup_version v
      ON v.team_id = s.team_id
     AND v.setup_id = s.id
     AND v.version = COALESCE(o.pinned_version, s.current_version);
$$;

REVOKE ALL ON FUNCTION setup.open_share_link(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION setup.open_share_link(bytea) TO pitbox_app;
COMMENT ON FUNCTION setup.open_share_link(bytea) IS
  'Abre um link de partilha pelo hash do token, sem contexto de equipa. SECURITY DEFINER, só pitbox_app.';
