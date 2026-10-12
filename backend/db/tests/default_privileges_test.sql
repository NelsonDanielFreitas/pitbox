-- Uma tabela criada por uma migração futura (pitbox_owner) herda os privilégios certos.
-- Corre como superutilizador (só para SET ROLE) e termina em ROLLBACK: não deixa nada na base.
\set QUIET on
BEGIN;
-- pitbox_owner tem LOGIN (exigido pelo TimescaleDB) mas nunca pode ter palavra-passe.
DO $$ BEGIN
  ASSERT (SELECT rolpassword IS NULL FROM pg_authid WHERE rolname = 'pitbox_owner'), 'pitbox_owner não pode ter palavra-passe';
  ASSERT (SELECT count(*) FROM pg_authid WHERE rolname IN ('pitbox_app','pitbox_readonly') AND rolpassword IS NOT NULL) = 0;
END $$;
SET ROLE pitbox_owner;
CREATE TABLE platform.zz_default_privileges_probe (id int PRIMARY KEY);
RESET ROLE;
DO $$ BEGIN
  ASSERT has_table_privilege('pitbox_app', 'platform.zz_default_privileges_probe', 'SELECT')
     AND has_table_privilege('pitbox_app', 'platform.zz_default_privileges_probe', 'INSERT')
     AND has_table_privilege('pitbox_app', 'platform.zz_default_privileges_probe', 'UPDATE')
     AND has_table_privilege('pitbox_app', 'platform.zz_default_privileges_probe', 'DELETE'), 'pitbox_app sem CRUD por omissão';
  ASSERT NOT has_table_privilege('pitbox_app', 'platform.zz_default_privileges_probe', 'TRUNCATE'), 'pitbox_app não trunca';
  ASSERT has_table_privilege('pitbox_readonly', 'platform.zz_default_privileges_probe', 'SELECT'), 'pitbox_readonly sem SELECT por omissão';
  ASSERT NOT has_table_privilege('pitbox_readonly', 'platform.zz_default_privileges_probe', 'INSERT'), 'pitbox_readonly não escreve';
  ASSERT NOT has_table_privilege('public', 'platform.zz_default_privileges_probe', 'SELECT'), 'PUBLIC não lê';
END $$;
\echo 'Privilégios por omissão OK.'
ROLLBACK;
