-- Testes estruturais: RLS em todas as tabelas, donos, papéis e privilégios.
-- Correm como pitbox_api (o papel real da aplicação) numa base já migrada.
--   psql -v ON_ERROR_STOP=1 -U pitbox_api -d pitbox -f tests/structure_test.sql
\set QUIET on
-- Tabelas sem RLS por decisão (o acesso é limitado por GRANT): platform.outbox e
-- telemetry.firmware_release. Qualquer outra tabela sem RLS falha. (Sem tabelas temporárias:
-- a base revoga TEMP a PUBLIC.)

-- 1. Toda a tabela da aplicação tem RLS, exceto as listadas.
DO $$ DECLARE bad text; BEGIN
  SELECT string_agg(c.oid::regclass::text, ', ') INTO bad
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE c.relkind IN ('r', 'p') AND n.nspname IN ('app','identity','garage','setup','maintenance','events','timing','telemetry','sync','platform','audit')
    AND NOT c.relrowsecurity
    AND c.oid NOT IN ('platform.outbox'::regclass, 'telemetry.firmware_release'::regclass);
  ASSERT bad IS NULL, 'tabelas sem RLS: ' || bad;
  ASSERT (SELECT count(*) FROM pg_class WHERE oid IN ('platform.outbox'::regclass, 'telemetry.firmware_release'::regclass)
          AND NOT relrowsecurity) = 2, 'a lista de tabelas sem RLS ficou desatualizada';
END $$;

-- 2. Toda a tabela com coluna team_id (e RLS exigido) está coberta, inclusive as sem política padrão.
DO $$ DECLARE bad text; BEGIN
  SELECT string_agg(c.oid::regclass::text, ', ') INTO bad
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
  JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'team_id' AND NOT a.attisdropped
  WHERE c.relkind IN ('r', 'p') AND n.nspname IN ('app','identity','garage','setup','maintenance','events','timing','telemetry','sync','platform','audit')
    AND NOT c.relrowsecurity AND c.oid <> 'platform.outbox'::regclass;
  ASSERT bad IS NULL, 'tabelas com team_id sem RLS: ' || bad;
END $$;

-- 3. RLS sem política esconderia tudo (falha só em funcionalidade): exige pelo menos uma.
DO $$ DECLARE bad text; BEGIN
  SELECT string_agg(c.oid::regclass::text, ', ') INTO bad
  FROM pg_class c
  WHERE c.relrowsecurity AND NOT EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid);
  ASSERT bad IS NULL, 'RLS sem política: ' || bad;
END $$;

-- 4. Dono: tudo o que as migrações criam pertence a pitbox_owner (nunca ao migrador nem à aplicação).
DO $$ DECLARE bad text; BEGIN
  SELECT string_agg(x, ', ') INTO bad FROM (
    SELECT c.oid::regclass::text AS x FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
      WHERE n.nspname IN ('app','identity','garage','setup','maintenance','events','timing','telemetry','sync','platform','audit') AND c.relkind IN ('r','p','v','m','S','i') AND pg_get_userbyid(c.relowner) <> 'pitbox_owner'
    UNION ALL SELECT p.oid::regprocedure::text FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
      WHERE n.nspname IN ('app','identity','garage','setup','maintenance','events','timing','telemetry','sync','platform','audit') AND pg_get_userbyid(p.proowner) <> 'pitbox_owner'
    UNION ALL SELECT t.oid::regtype::text FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
      WHERE n.nspname IN ('app','identity','garage','setup','maintenance','events','timing','telemetry','sync','platform','audit') AND t.typtype = 'e' AND pg_get_userbyid(t.typowner) <> 'pitbox_owner'
    UNION ALL SELECT n.nspname FROM pg_namespace n
      WHERE n.nspname IN ('app','identity','garage','setup','maintenance','events','timing','telemetry','sync','platform','audit') AND pg_get_userbyid(n.nspowner) <> 'pitbox_owner'
  ) q;
  ASSERT bad IS NULL, 'objetos com dono errado: ' || bad;
  ASSERT (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
          WHERE n.nspname IN ('app','identity','garage','setup','maintenance','events','timing','telemetry','sync','platform','audit') AND c.relkind = 'r') = 55,
    'esperadas 55 tabelas (hypertable incluída)';
END $$;

-- 5. Papéis: nenhum com privilégios de administração; os de grupo não fazem login.
DO $$ DECLARE bad text; BEGIN
  SELECT string_agg(rolname, ', ') INTO bad FROM pg_roles
  WHERE rolname IN ('pitbox_owner','pitbox_app','pitbox_readonly','pitbox_migrator','pitbox_api')
    AND (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb OR rolreplication);
  ASSERT bad IS NULL, 'papéis com privilégios a mais: ' || bad;
  ASSERT (SELECT count(*) FROM pg_roles WHERE rolname IN ('pitbox_app','pitbox_readonly') AND rolcanlogin) = 0,
    'papéis de grupo não devem fazer login';
  -- pitbox_owner tem LOGIN só porque o TimescaleDB o exige para as políticas; a ausência de
  -- palavra-passe é verificada em default_privileges_test.sql (precisa de pg_authid).
  ASSERT (SELECT count(*) FROM pg_roles WHERE rolname IN ('pitbox_owner','pitbox_migrator','pitbox_api') AND rolcanlogin) = 3,
    'papéis de login em falta';
  ASSERT 'role=pitbox_owner' = ANY (SELECT unnest(rolconfig) FROM pg_roles WHERE rolname = 'pitbox_migrator'),
    'pitbox_migrator tem de assumir pitbox_owner';
  ASSERT NOT pg_has_role('pitbox_api', 'pitbox_owner', 'USAGE') AND NOT pg_has_role('pitbox_api', 'pitbox_owner', 'MEMBER'),
    'a API não pode ser membro de pitbox_owner';
END $$;

-- 6. pitbox_app: só SELECT/INSERT/UPDATE/DELETE, e nunca nas tabelas de acrescento / catálogos.
DO $$ DECLARE bad text; BEGIN
  SELECT string_agg(c.oid::regclass::text || ':' || p, ', ') INTO bad
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
  CROSS JOIN unnest(ARRAY['TRUNCATE','REFERENCES','TRIGGER']) p
  WHERE n.nspname IN ('app','identity','garage','setup','maintenance','events','timing','telemetry','sync','platform','audit') AND c.relkind IN ('r','p') AND has_table_privilege('pitbox_app', c.oid, p);
  ASSERT bad IS NULL, 'pitbox_app com privilégios a mais: ' || bad;
  ASSERT NOT has_table_privilege('pitbox_app', 'audit.audit_log', 'UPDATE')
     AND NOT has_table_privilege('pitbox_app', 'audit.audit_log', 'DELETE'), 'audit_log é só de acrescento';
  ASSERT NOT has_table_privilege('pitbox_app', 'setup.setup_version', 'UPDATE')
     AND NOT has_table_privilege('pitbox_app', 'setup.setup_version', 'DELETE'), 'setup_version é imutável';
  ASSERT NOT has_table_privilege('pitbox_app', 'garage.stock_movement', 'UPDATE')
     AND NOT has_table_privilege('pitbox_app', 'garage.stock_movement', 'DELETE'), 'stock_movement é só de acrescento';
  ASSERT NOT has_table_privilege('pitbox_app', 'telemetry.firmware_release', 'INSERT')
     AND NOT has_table_privilege('pitbox_app', 'telemetry.firmware_release', 'UPDATE')
     AND NOT has_table_privilege('pitbox_app', 'telemetry.firmware_release', 'DELETE'), 'firmware_release é só de leitura';
  ASSERT NOT has_schema_privilege('pitbox_app', 'public', 'CREATE'), 'pitbox_app não cria objetos em public';
  ASSERT NOT has_database_privilege('pitbox_app', current_database(), 'CREATE'), 'pitbox_app não cria esquemas';
END $$;

-- 7. pitbox_readonly: lista exata de tabelas que NÃO pode ler (sensíveis ou sem RLS por equipa).
--    Uma tabela nova passa a ser legível por omissão (default privileges): se for sensível, a
--    migração faz REVOKE e acrescenta-a aqui; se não, este teste obriga a decidir.
DO $$ DECLARE got text; want text := 'platform.idempotency_key, platform.outbox, platform.push_subscription'; BEGIN
  SELECT string_agg(c.oid::regclass::text, ', ' ORDER BY c.oid::regclass::text) INTO got
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE n.nspname IN ('app','identity','garage','setup','maintenance','events','timing','telemetry','sync','platform','audit') AND c.relkind IN ('r','p')
    AND NOT has_table_privilege('pitbox_readonly', c.oid, 'SELECT');
  ASSERT got = want, format('pitbox_readonly não lê: %s (esperado: %s)', got, want);
  ASSERT (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
          WHERE n.nspname IN ('app','identity','garage','setup','maintenance','events','timing','telemetry','sync','platform','audit') AND c.relkind IN ('r','p')
            AND (has_table_privilege('pitbox_readonly', c.oid, 'INSERT') OR has_table_privilege('pitbox_readonly', c.oid, 'UPDATE')
                 OR has_table_privilege('pitbox_readonly', c.oid, 'DELETE'))) = 0, 'pitbox_readonly só lê';
END $$;

-- 8. Telemetria: hypertable com retenção e SEM compressão (ADR 0011; incompatível com RLS).
DO $$ BEGIN
  ASSERT EXISTS (SELECT 1 FROM timescaledb_information.hypertables
                 WHERE hypertable_schema = 'telemetry' AND hypertable_name = 'sample'), 'telemetry.sample não é hypertable';
  ASSERT NOT (SELECT compression_enabled FROM timescaledb_information.hypertables
              WHERE hypertable_schema = 'telemetry' AND hypertable_name = 'sample'), 'compressão ativa em tabela com RLS';
  ASSERT EXISTS (SELECT 1 FROM timescaledb_information.jobs
                 WHERE proc_name = 'policy_retention' AND hypertable_schema = 'telemetry' AND hypertable_name = 'sample'),
    'falta a política de retenção';
END $$;

-- 9. SECURITY DEFINER: lista exata (cada uma é uma exceção à RLS e tem de ser revista), dona
--    pitbox_owner, search_path fixo, e ninguém além de pitbox_app a executa (nem PUBLIC).
DO $$ DECLARE got text; bad text; BEGIN
  SELECT string_agg(p.oid::regprocedure::text, ', ' ORDER BY p.oid::regprocedure::text) INTO got
  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
  WHERE p.prosecdef AND n.nspname IN ('app','identity','garage','setup','maintenance','events','timing','telemetry','sync','platform','audit');
  ASSERT got = 'identity.is_owner(uuid), identity.resolve_login(text,citext,text)', format('funções SECURITY DEFINER: %s', got);
  SELECT string_agg(p.oid::regprocedure::text, ', ') INTO bad
  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
  WHERE p.prosecdef AND n.nspname IN ('app','identity','garage','setup','maintenance','events','timing','telemetry','sync','platform','audit')
    AND (pg_get_userbyid(p.proowner) <> 'pitbox_owner'
         OR NOT ('search_path=pg_catalog, pg_temp' = ANY (COALESCE(p.proconfig, '{}')))
         OR has_function_privilege('pitbox_readonly', p.oid, 'EXECUTE')
         OR EXISTS (SELECT 1 FROM aclexplode(COALESCE(p.proacl, acldefault('f', p.proowner))) a WHERE a.grantee = 0)
         OR NOT has_function_privilege('pitbox_app', p.oid, 'EXECUTE'));
  ASSERT bad IS NULL, 'SECURITY DEFINER mal configurada: ' || bad;
END $$;

\echo 'Testes estruturais passaram.'
