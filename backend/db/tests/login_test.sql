-- Testes de identity.resolve_login (primeiro login, F1-03b, ADR 0013).
-- Correm como pitbox_api (o papel real da aplicação) numa base já migrada; tudo é desfeito no fim.
--   psql -v ON_ERROR_STOP=1 -U pitbox_api -d pitbox -f tests/login_test.sql
-- Os ids passam de um bloco para o outro por set_config (sem tabelas temporárias: TEMP está
-- revogado a PUBLIC).
\set QUIET on
BEGIN;

-- Sem contexto não se vê nenhuma conta, nem depois de existirem.
DO $$ BEGIN
  ASSERT (SELECT count(*) FROM identity.user_account) = 0, 'sem contexto, sem utilizadores';
END $$;

-- 1. Cria no primeiro login; o email chega em qualquer capitalização.
DO $$ DECLARE a uuid; BEGIN
  a := identity.resolve_login('sub-ana', 'Ana@Exemplo.pt', 'Ana');
  ASSERT a IS NOT NULL, 'devolve o id';
  PERFORM set_config('t.ana', a::text, true);
  ASSERT (SELECT count(*) FROM identity.user_account) = 0,
    'pitbox_app sem contexto continua sem ver linhas, mesmo depois de criar';
  PERFORM set_config('app.user_id', a::text, true);
  ASSERT (SELECT count(*) FROM identity.user_account) = 1, 'o próprio utilizador vê-se';
  ASSERT (SELECT idp_subject FROM identity.user_account WHERE id = a) = 'sub-ana', 'sub guardado';
END $$;

-- 2. Idempotente: segundo login = mesmo id, sem nova versão nem nova linha.
DO $$ DECLARE a uuid := current_setting('t.ana')::uuid; again uuid; BEGIN
  again := identity.resolve_login('sub-ana', 'ana@exemplo.pt', 'Ana');
  ASSERT again = a, 'mesmo utilizador no segundo login';
  PERFORM set_config('app.user_id', a::text, true);
  ASSERT (SELECT count(*) FROM identity.user_account) = 1, 'sem duplicados';
  ASSERT (SELECT version FROM identity.user_account WHERE id = a) = 1, 'sem alterações, a versão não sobe';
END $$;

-- 3. Email e nome atualizam-se quando mudam no IdP (e só então).
DO $$ DECLARE a uuid := current_setting('t.ana')::uuid; BEGIN
  ASSERT identity.resolve_login('sub-ana', 'ana.nova@exemplo.pt', 'Ana Nova') = a, 'mesmo id com dados novos';
  PERFORM set_config('app.user_id', a::text, true);
  ASSERT (SELECT email::text FROM identity.user_account WHERE id = a) = 'ana.nova@exemplo.pt', 'email atualizado';
  ASSERT (SELECT display_name FROM identity.user_account WHERE id = a) = 'Ana Nova', 'nome atualizado';
  ASSERT (SELECT version FROM identity.user_account WHERE id = a) = 2, 'uma só subida de versão';
END $$;

-- 4. Nunca liga contas por email: outro sub com o mesmo email (qualquer capitalização) falha
--    com PB001 e não cria nem toca em nada; a conta da Ana fica como estava.
DO $$ DECLARE a uuid := current_setting('t.ana')::uuid; b uuid; BEGIN
  BEGIN
    PERFORM identity.resolve_login('sub-bruno', 'ANA.NOVA@exemplo.pt', 'Bruno');
    ASSERT false, 'devia ter falhado: email de outra conta';
  EXCEPTION WHEN SQLSTATE 'PB001' THEN NULL;
  END;
  b := identity.resolve_login('sub-bruno', 'bruno@exemplo.pt', 'Bruno');
  ASSERT b <> a, 'o sub do Bruno não ficou ligado à conta da Ana';
  PERFORM set_config('t.bruno', b::text, true);
  -- Mudar para o email de outra conta também falha, e o Bruno mantém o dele.
  BEGIN
    PERFORM identity.resolve_login('sub-bruno', 'ana.nova@exemplo.pt', 'Bruno');
    ASSERT false, 'devia ter falhado: mudança para o email de outra conta';
  EXCEPTION WHEN SQLSTATE 'PB001' THEN NULL;
  END;
  PERFORM set_config('app.user_id', b::text, true);
  ASSERT (SELECT email::text FROM identity.user_account WHERE id = b) = 'bruno@exemplo.pt', 'email do Bruno intacto';
END $$;

-- 5. Conta apagada: o login falha (PB002) e o sub não se reativa; o email fica livre.
DO $$ DECLARE b uuid := current_setting('t.bruno')::uuid; c uuid; BEGIN
  PERFORM set_config('app.user_id', b::text, true);
  UPDATE identity.user_account SET deleted_at = now() WHERE id = b;
  BEGIN
    PERFORM identity.resolve_login('sub-bruno', 'bruno@exemplo.pt', 'Bruno');
    ASSERT false, 'devia ter falhado: conta apagada';
  EXCEPTION WHEN SQLSTATE 'PB002' THEN NULL;
  END;
  c := identity.resolve_login('sub-carla', 'bruno@exemplo.pt', 'Carla');
  ASSERT c IS NOT NULL AND c <> b, 'o email de uma conta apagada pode ser usado por outra pessoa';
  PERFORM set_config('t.carla', c::text, true);
END $$;

-- 6. Entradas inválidas: 22023.
DO $$ DECLARE bad text; i int := 0; args text[][] := ARRAY[
  ARRAY['', 'x@y.pt'], ARRAY['   ', 'x@y.pt'], ARRAY[repeat('s', 256), 'x@y.pt'],
  ARRAY['sub-v', ''], ARRAY['sub-v', 'sem-arroba'], ARRAY['sub-v', '@sem-local.pt'],
  ARRAY['sub-v', repeat('a', 320) || '@y.pt']]; BEGIN
  FOR i IN 1 .. array_length(args, 1) LOOP
    BEGIN
      PERFORM identity.resolve_login(args[i][1], args[i][2]::citext, 'X');
      ASSERT false, format('devia ter falhado com %L', args[i]);
    EXCEPTION WHEN SQLSTATE '22023' THEN NULL;
    END;
  END LOOP;
  BEGIN PERFORM identity.resolve_login(NULL, 'x@y.pt', 'X'); ASSERT false, 'sub NULL';
  EXCEPTION WHEN SQLSTATE '22023' THEN NULL; END;
  BEGIN PERFORM identity.resolve_login('sub-v', NULL, 'X'); ASSERT false, 'email NULL';
  EXCEPTION WHEN SQLSTATE '22023' THEN NULL; END;
END $$;

-- 7. Nome vazio usa a parte local do email; nome comprido é cortado a 80 (CHECK da tabela).
DO $$ DECLARE d uuid; e uuid; BEGIN
  d := identity.resolve_login('sub-dina', 'dina@exemplo.pt', '   ');
  e := identity.resolve_login('sub-eva', 'eva@exemplo.pt', repeat('é', 200));
  PERFORM set_config('app.user_id', d::text, true);
  ASSERT (SELECT display_name FROM identity.user_account WHERE id = d) = 'dina', 'nome em falta';
  PERFORM set_config('app.user_id', e::text, true);
  ASSERT (SELECT length(display_name) FROM identity.user_account WHERE id = e) = 80, 'nome cortado a 80 caracteres';
END $$;

-- 8. Isolamento: cada utilizador, noutra equipa, só vê a sua própria conta.
DO $$ DECLARE a uuid := current_setting('t.ana')::uuid; c uuid := current_setting('t.carla')::uuid;
  ta uuid := '01900000-0000-7000-8000-0000000000e1'; tc uuid := '01900000-0000-7000-8000-0000000000e2'; BEGIN
  PERFORM set_config('app.user_id', a::text, true); PERFORM set_config('app.team_id', ta::text, true);
  INSERT INTO identity.team (id, name, created_by) VALUES (ta, 'Equipa Ana', a);
  INSERT INTO identity.membership (team_id, user_id, role) VALUES (ta, a, 'owner');
  PERFORM set_config('app.user_id', c::text, true); PERFORM set_config('app.team_id', tc::text, true);
  INSERT INTO identity.team (id, name, created_by) VALUES (tc, 'Equipa Carla', c);
  INSERT INTO identity.membership (team_id, user_id, role) VALUES (tc, c, 'owner');

  PERFORM set_config('app.user_id', a::text, true); PERFORM set_config('app.team_id', ta::text, true);
  ASSERT (SELECT count(*) FROM identity.user_account) = 1, 'a Ana só vê a Ana';
  ASSERT NOT EXISTS (SELECT 1 FROM identity.user_account WHERE id = c), 'a Ana não vê a Carla';
  PERFORM set_config('app.user_id', c::text, true); PERFORM set_config('app.team_id', tc::text, true);
  ASSERT (SELECT count(*) FROM identity.user_account) = 1, 'a Carla só vê a Carla';
  -- resolve_login não serve de oráculo: devolve o id do próprio sub, nunca o de outro.
  ASSERT identity.resolve_login('sub-ana', 'ana.nova@exemplo.pt', 'Ana Nova') = a, 'devolve o id do sub pedido';
END $$;

\echo 'Testes de login passaram.'
ROLLBACK;
