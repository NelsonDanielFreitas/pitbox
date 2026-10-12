-- Estado de papéis e jobs, para o teste de desvio (o pg_dump --schema-only não os inclui).
-- Sem palavras-passe (são aleatórias por execução).
\pset format unaligned
\pset tuples_only on
SELECT 'role', rolname, rolsuper, rolinherit, rolcreaterole, rolcreatedb, rolcanlogin, rolreplication, rolbypassrls, rolconfig
FROM pg_roles WHERE rolname !~ '^pg_' ORDER BY rolname;
SELECT 'member', r.rolname, m.rolname, am.inherit_option, am.set_option
FROM pg_auth_members am JOIN pg_roles r ON r.oid = am.roleid JOIN pg_roles m ON m.oid = am.member
WHERE r.rolname !~ '^pg_' ORDER BY 2, 3;
SELECT 'ext', extname, extversion FROM pg_extension ORDER BY extname;
SELECT 'job', proc_name, hypertable_schema, hypertable_name, schedule_interval, config
FROM timescaledb_information.jobs WHERE proc_name LIKE 'policy%' ORDER BY proc_name, hypertable_name;
SELECT 'dim', hypertable_schema, hypertable_name, column_name, time_interval
FROM timescaledb_information.dimensions ORDER BY 2, 3;
SELECT 'defacl', pg_get_userbyid(defaclrole), defaclnamespace::regnamespace, defaclobjtype, defaclacl
FROM pg_default_acl ORDER BY 2, 3, 4;
